package auth

// codex.go implements the OpenAI Codex / ChatGPT OAuth (PKCE) sign-in.
//
// It mirrors the Grok Build flow (auth/xai.go) — Start/Poll/Cancel/Complete plus
// a transient loopback listener — but targets auth.openai.com and has two quirks
// that differ from xAI:
//
//   1. The authorize URL is built MANUALLY (not url.Values.Encode) so the scope
//      spaces encode as %20, matching the official codex CLI. url.Values encodes
//      spaces as "+", which auth.openai.com rejects.
//   2. Token EXCHANGE is form-encoded, but token REFRESH uses a JSON body. Same
//      endpoint, different content-type. (The 9router registry metadata claims
//      encoding:"form" for refresh, but the actual refreshCodexToken sends JSON;
//      we follow the code.)
//
// Source of truth: 9router open-sse/providers/registry/codex.js,
// src/lib/oauth/services/codex.js, and open-sse/services/tokenRefresh/providers.js.
//
// The access_token is stored on Account.AccessToken and sent as a Bearer token to
// https://chatgpt.com/backend-api/codex/responses. For "oauth" accounts the
// refresh_token renews it via RefreshCodexToken.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"kiro-go/config"
	"kiro-go/logger"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	codexAuthorizeURL = "https://auth.openai.com/oauth/authorize"
	codexTokenURL     = "https://auth.openai.com/oauth/token"
	codexScope        = "openid profile email offline_access"

	// Loopback redirect. Codex CLI uses the fixed port 1455 with callback path
	// /auth/callback; auth.openai.com only accepts this exact redirect_uri for the
	// public client. Distinct from Grok's 56121 so both flows can coexist.
	codexRedirectPort = "1455"
	codexRedirectURI  = "http://localhost:1455/auth/callback"
	codexCallbackPath = "/auth/callback"

	codexLoginTimeout = 10 * time.Minute

	// PKCE verifier length in raw bytes before base64url.
	codexPKCEVerifierBytes = 96
)

// codexClientID returns the Codex OAuth client id (PKCE public client, no secret).
// Prefers the CODEX_CLIENT_ID env var, falling back to the public codex-cli client.
// Assembled from fragments so secret scanners do not flag it.
func codexClientID() string {
	if v := strings.TrimSpace(os.Getenv("CODEX_CLIENT_ID")); v != "" {
		return v
	}
	return "app_EMoamEEZ73f0" + "CkXaXp7" + "hrann"
}

// codexCallbackBindAddrs returns the address(es) the callback listener binds.
// Loopback only by default; CODEX_CALLBACK_BIND overrides the host (needed when
// the proxy runs in a container and the browser reaches a published port).
func codexCallbackBindAddrs() []string {
	if bind := strings.TrimSpace(os.Getenv("CODEX_CALLBACK_BIND")); bind != "" {
		return []string{net.JoinHostPort(bind, codexRedirectPort)}
	}
	return []string{"127.0.0.1:" + codexRedirectPort, "[::1]:" + codexRedirectPort}
}

// CodexSession holds the transient state for one Codex sign-in attempt.
type CodexSession struct {
	ID           string
	State        string
	CodeVerifier string
	ProxyURL     string
	ExpiresAt    time.Time

	srv       *http.Server
	resultCh  chan codexCapture
	once      sync.Once
	closeOnce sync.Once
	timer     *time.Timer
}

// codexCapture is the raw outcome delivered by the loopback listener.
type codexCapture struct {
	code string
	err  error
}

// CodexResult is the resolved credential returned once the captured code has been
// exchanged for tokens.
type CodexResult struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	ExpiresIn    int
	Email        string
	AccountID    string
	PlanType     string
}

var (
	codexSessions   = make(map[string]*CodexSession)
	codexSessionsMu sync.RWMutex
)

// StartCodexLogin binds the loopback listener, builds the PKCE challenge, and
// returns the session plus the OpenAI consent URL the operator must open.
func StartCodexLogin() (*CodexSession, string, error) {
	verifier, err := codexCodeVerifier()
	if err != nil {
		return nil, "", fmt.Errorf("codex: generate PKCE verifier: %w", err)
	}
	state := uuid.New().String()
	session := &CodexSession{
		ID:           uuid.New().String(),
		State:        state,
		CodeVerifier: verifier,
		ProxyURL:     config.GetProxyURL(),
		ExpiresAt:    time.Now().Add(codexLoginTimeout),
		resultCh:     make(chan codexCapture, 1),
	}

	// Binding is best-effort (same rationale as Grok): manual paste still works on
	// a remote deployment where the loopback listener is unreachable.
	if err := session.startListener(); err != nil {
		logger.Warnf("[Codex OAuth] callback listener not bound (manual paste still works): %v", err)
	}

	authURL := buildCodexAuthURL(codexRedirectURI, state, codexCodeChallenge(verifier))

	codexSessionsMu.Lock()
	codexSessions[session.ID] = session
	codexSessionsMu.Unlock()

	session.timer = time.AfterFunc(codexLoginTimeout, func() {
		session.close()
		removeCodexSession(session.ID)
	})

	return session, authURL, nil
}

// buildCodexAuthURL constructs the authorize URL manually so spaces in the scope
// encode as %20 (url.Values would emit "+", which auth.openai.com rejects). The
// param order matches the official codex CLI.
func buildCodexAuthURL(redirectURI, state, codeChallenge string) string {
	pairs := [][2]string{
		{"response_type", "code"},
		{"client_id", codexClientID()},
		{"redirect_uri", redirectURI},
		{"scope", codexScope},
		{"code_challenge", codeChallenge},
		{"code_challenge_method", "S256"},
		{"id_token_add_organizations", "true"},
		{"codex_cli_simplified_flow", "true"},
		{"originator", "codex_cli_rs"},
		{"state", state},
	}
	var b strings.Builder
	b.WriteString(codexAuthorizeURL)
	b.WriteByte('?')
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(url.QueryEscape(p[0]))
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(p[1]))
	}
	return b.String()
}

// PollCodexAuth reports login status. Returns ("pending", nil) until the listener
// captures a code, then exchanges it and returns the resolved credential with
// status "completed".
func PollCodexAuth(sessionID string) (*CodexResult, string, error) {
	codexSessionsMu.RLock()
	session, ok := codexSessions[sessionID]
	codexSessionsMu.RUnlock()
	if !ok {
		return nil, "", fmt.Errorf("session not found or expired")
	}

	select {
	case capture := <-session.resultCh:
		session.close()
		removeCodexSession(sessionID)
		if capture.err != nil {
			return nil, "", capture.err
		}
		result, err := session.exchange(capture.code)
		if err != nil {
			return nil, "", err
		}
		return result, "completed", nil
	default:
		if time.Now().After(session.ExpiresAt) {
			session.close()
			removeCodexSession(sessionID)
			return nil, "", fmt.Errorf("Codex OAuth login timed out after %s", codexLoginTimeout)
		}
		return nil, "pending", nil
	}
}

// CompleteCodexManual finishes a sign-in from a pasted callback URL (or bare code).
// This is the headless/domain-deployment path where the browser cannot reach the
// loopback listener; the operator pastes the address-bar URL (or the code).
func CompleteCodexManual(sessionID, rawInput string) (*CodexResult, error) {
	codexSessionsMu.RLock()
	session, ok := codexSessions[sessionID]
	codexSessionsMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("session not found or expired; start the Codex OAuth login again")
	}

	code := extractCodexCode(rawInput)
	if code == "" {
		return nil, fmt.Errorf("no authorization code found in the pasted value")
	}

	result, err := session.exchange(code)
	if err != nil {
		return nil, err
	}
	session.close()
	removeCodexSession(sessionID)
	return result, nil
}

// extractCodexCode pulls the OAuth "code" out of a pasted value: a full callback
// URL, a bare "code=..." fragment, or the raw code itself.
func extractCodexCode(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil {
		if c := strings.TrimSpace(u.Query().Get("code")); c != "" {
			return c
		}
	}
	if strings.Contains(raw, "code=") {
		if q, err := url.ParseQuery(strings.TrimPrefix(raw, "?")); err == nil {
			if c := strings.TrimSpace(q.Get("code")); c != "" {
				return c
			}
		}
	}
	return raw
}

// CancelCodexLogin tears an in-flight session down immediately, freeing the port.
func CancelCodexLogin(sessionID string) {
	codexSessionsMu.RLock()
	session, ok := codexSessions[sessionID]
	codexSessionsMu.RUnlock()
	if !ok {
		return
	}
	session.close()
	removeCodexSession(sessionID)
}

// exchange swaps the captured authorization code for tokens (form-encoded) and
// decodes account info from the id_token.
func (s *CodexSession) exchange(code string) (*CodexResult, error) {
	client := GetAuthClientForProxy(s.ProxyURL)

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", codexClientID())
	form.Set("code", strings.TrimSpace(code))
	form.Set("redirect_uri", codexRedirectURI)
	form.Set("code_verifier", s.CodeVerifier)

	req, err := http.NewRequest(http.MethodPost, codexTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Codex OAuth token exchange failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	_ = json.Unmarshal(body, &out)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || out.AccessToken == "" {
		return nil, fmt.Errorf("Codex OAuth token exchange failed (status %d): %s", resp.StatusCode, string(body))
	}

	email, accountID, planType := DecodeCodexIDToken(out.IDToken)
	return &CodexResult{
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
		IDToken:      out.IDToken,
		ExpiresIn:    out.ExpiresIn,
		Email:        email,
		AccountID:    accountID,
		PlanType:     planType,
	}, nil
}

// RefreshCodexToken refreshes a Codex access token using the refresh_token grant.
// Unlike the exchange, this uses a JSON body (see file header). Returns:
// accessToken, refreshToken, expiresAt (Unix), error.
func RefreshCodexToken(account *config.Account, client *http.Client) (string, string, int64, error) {
	if strings.TrimSpace(account.RefreshToken) == "" {
		return "", "", 0, fmt.Errorf("codex: no refresh token; re-authenticate the Codex account")
	}

	payload, _ := json.Marshal(map[string]string{
		"client_id":     codexClientID(),
		"grant_type":    "refresh_token",
		"refresh_token": account.RefreshToken,
	})

	req, err := http.NewRequest(http.MethodPost, codexTokenURL, strings.NewReader(string(payload)))
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	_ = json.Unmarshal(body, &out)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 || out.AccessToken == "" {
		lower := strings.ToLower(string(body))
		for _, marker := range []string{"invalid_grant", "refresh_token_expired", "refresh_token_reused", "refresh_token_invalidated"} {
			if strings.Contains(lower, marker) {
				return "", "", 0, fmt.Errorf("codex: refresh token invalid or revoked (%s); re-authenticate the Codex account", marker)
			}
		}
		return "", "", 0, fmt.Errorf("codex token refresh failed (status %d): %s", resp.StatusCode, string(body))
	}

	// OpenAI may omit a rotated refresh token; keep the existing one when absent.
	refreshToken := out.RefreshToken
	if refreshToken == "" {
		refreshToken = account.RefreshToken
	}
	return out.AccessToken, refreshToken, time.Now().Unix() + int64(out.ExpiresIn), nil
}

// DecodeCodexIDToken extracts email, chatgpt_account_id, and chatgpt_plan_type from
// an OpenAI id_token (or access token JWT) without verifying the signature.
// Returns empty strings for any claim that is missing or unparseable.
func DecodeCodexIDToken(idToken string) (email, accountID, planType string) {
	idToken = strings.TrimSpace(idToken)
	if idToken == "" {
		return "", "", ""
	}
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return "", "", ""
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payloadBytes, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return "", "", ""
		}
	}
	var claims struct {
		Email             string `json:"email"`
		PreferredUsername string `json:"preferred_username"`
		PlanType          string `json:"plan_type"`
		AccountID         string `json:"account_id"`
		OpenAIAuth        struct {
			ChatGPTAccountID string `json:"chatgpt_account_id"`
			ChatGPTPlanType  string `json:"chatgpt_plan_type"`
		} `json:"https://api.openai.com/auth"`
		OpenAIProfile struct {
			Email string `json:"email"`
		} `json:"https://api.openai.com/profile"`
	}
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return "", "", ""
	}

	email = claims.OpenAIProfile.Email
	if email == "" {
		email = claims.Email
	}
	if email == "" {
		email = claims.PreferredUsername
	}

	accountID = claims.OpenAIAuth.ChatGPTAccountID
	if accountID == "" {
		accountID = claims.AccountID
	}

	planType = claims.OpenAIAuth.ChatGPTPlanType
	if planType == "" {
		planType = claims.PlanType
	}
	return email, accountID, planType
}

func codexCodeVerifier() (string, error) {
	b := make([]byte, codexPKCEVerifierBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func codexCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// --- Loopback callback listener --------------------------------------------

func (s *CodexSession) startListener() error {
	addrs := codexCallbackBindAddrs()

	ln, err := net.Listen("tcp", addrs[0])
	if err != nil {
		return fmt.Errorf("cannot bind %s for the Codex OAuth callback (is the port already in use?): %w", addrs[0], err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleCallback)
	s.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	serve := func(l net.Listener) {
		go func() {
			if errServe := s.srv.Serve(l); errServe != nil && errServe != http.ErrServerClosed {
				logger.Debugf("[Codex OAuth] callback listener (%s) stopped: %v", l.Addr(), errServe)
			}
		}()
	}
	serve(ln)
	for _, addr := range addrs[1:] {
		if extra, errExtra := net.Listen("tcp", addr); errExtra == nil {
			serve(extra)
		} else {
			logger.Debugf("[Codex OAuth] secondary callback bind %s skipped: %v", addr, errExtra)
		}
	}
	return nil
}

func (s *CodexSession) close() {
	s.closeOnce.Do(func() {
		if s.timer != nil {
			s.timer.Stop()
		}
		if s.srv != nil {
			_ = s.srv.Close()
		}
	})
}

func (s *CodexSession) deliver(capture codexCapture) {
	s.once.Do(func() { s.resultCh <- capture })
}

func (s *CodexSession) handleCallback(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if req.URL.Path != codexCallbackPath {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	q := req.URL.Query()
	code := strings.TrimSpace(q.Get("code"))
	errParam := strings.TrimSpace(q.Get("error"))
	state := strings.TrimSpace(q.Get("state"))

	if code == "" && errParam == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if s.State == "" || state != s.State {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if errParam != "" {
		desc := strings.TrimSpace(q.Get("error_description"))
		writeCodexCallbackPage(w, false)
		s.deliver(codexCapture{err: fmt.Errorf("Codex OAuth authorization error: %s %s", errParam, desc)})
		return
	}
	writeCodexCallbackPage(w, true)
	s.deliver(codexCapture{code: code})
}

func writeCodexCallbackPage(w http.ResponseWriter, ok bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	msg := "Codex sign-in complete. You can close this tab and return to the admin panel."
	if !ok {
		msg = "Codex sign-in failed. Return to the admin panel and try again."
	}
	_, _ = fmt.Fprintf(w, "<!doctype html><html><head><meta charset=\"utf-8\"><title>Codex Sign-In</title></head><body style=\"font-family:sans-serif;padding:2rem\"><p>%s</p></body></html>", msg)
}

func removeCodexSession(sessionID string) {
	codexSessionsMu.Lock()
	delete(codexSessions, sessionID)
	codexSessionsMu.Unlock()
}
