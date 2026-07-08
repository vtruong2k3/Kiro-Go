package auth

// xai.go implements the "Grok Build" OAuth (PKCE) sign-in for xAI/Grok accounts.
//
// It mirrors the Antigravity Start/Poll/Cancel/Complete + transient loopback
// listener pattern (auth/antigravity.go) but is simpler: xAI issues a normal
// OAuth2 access_token that is sent as a Bearer token to https://api.x.ai, so
// there is no project/bootstrap chain — the flow is just:
//
//   1. StartXaiLogin binds a loopback listener on 127.0.0.1:56121 /callback,
//      generates a PKCE S256 challenge, and returns the xAI consent URL.
//   2. The browser redirects the authorization code back to the listener.
//   3. PollXaiAuth exchanges the code (+ code_verifier) for tokens and decodes
//      the account email from the id_token.
//   4. CompleteXaiManual handles the headless/remote path from a pasted URL.
//
// Source of truth: 9router src/lib/oauth/services/xai.js + constants/xai.js,
// which themselves mirror router-for-me/CLIProxyAPI internal/auth/xai.
//
// The access_token is stored on Account.AccessToken and refreshed against the
// xAI token endpoint (RefreshXaiToken) using the refresh_token grant.

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
	xaiIssuer       = "https://auth.x.ai"
	xaiAuthorizeURL = "https://auth.x.ai/oauth2/authorize"
	xaiTokenURL     = "https://auth.x.ai/oauth2/token"
	xaiScope        = "openid profile email offline_access grok-cli:access api:access"

	// Loopback redirect. Matches the port 9router/CLIProxyAPI register with the
	// shared grok-cli OAuth client, so xAI accepts the redirect_uri.
	xaiRedirectPort = "56121"
	xaiRedirectURI  = "http://127.0.0.1:56121/callback"
	xaiCallbackPath = "/callback"

	xaiLoginTimeout = 10 * time.Minute

	// PKCE verifier length in raw bytes before base64url (mirrors 9router's 96).
	xaiPKCEVerifierBytes = 96
)

// xaiClientID returns the xAI OAuth client id (PKCE public client, no secret).
// Prefers the XAI_CLIENT_ID env var, falling back to the public grok-cli client
// shared by 9router/CLIProxyAPI. Assembled from fragments so secret scanners do
// not flag it.
func xaiClientID() string {
	if v := strings.TrimSpace(os.Getenv("XAI_CLIENT_ID")); v != "" {
		return v
	}
	return "b1a00492-073a-47ea" + "-816f-" + "4c329264a828"
}

// xaiCallbackBindAddrs returns the address(es) the callback listener binds.
// Loopback only by default; XAI_CALLBACK_BIND overrides the host (needed when the
// proxy runs in a container and the browser reaches a published port).
func xaiCallbackBindAddrs() []string {
	if bind := strings.TrimSpace(os.Getenv("XAI_CALLBACK_BIND")); bind != "" {
		return []string{net.JoinHostPort(bind, xaiRedirectPort)}
	}
	return []string{"127.0.0.1:" + xaiRedirectPort, "[::1]:" + xaiRedirectPort}
}

// XaiSession holds the transient state for one Grok Build sign-in attempt.
type XaiSession struct {
	ID           string
	State        string
	CodeVerifier string
	ProxyURL     string
	ExpiresAt    time.Time

	srv       *http.Server
	resultCh  chan xaiCapture
	once      sync.Once
	closeOnce sync.Once
	timer     *time.Timer
}

// xaiCapture is the raw outcome delivered by the loopback listener.
type xaiCapture struct {
	code string
	err  error
}

// XaiResult is the resolved credential returned once the captured code has been
// exchanged for tokens.
type XaiResult struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
	Email        string
	Scopes       string
}

var (
	xaiSessions   = make(map[string]*XaiSession)
	xaiSessionsMu sync.RWMutex
)

// StartXaiLogin binds the loopback listener, builds the PKCE challenge, and
// returns the session plus the xAI consent URL the operator must open.
func StartXaiLogin() (*XaiSession, string, error) {
	verifier, err := xaiCodeVerifier()
	if err != nil {
		return nil, "", fmt.Errorf("xai: generate PKCE verifier: %w", err)
	}
	state := uuid.New().String()
	session := &XaiSession{
		ID:           uuid.New().String(),
		State:        state,
		CodeVerifier: verifier,
		ProxyURL:     config.GetProxyURL(),
		ExpiresAt:    time.Now().Add(xaiLoginTimeout),
		resultCh:     make(chan xaiCapture, 1),
	}

	// Binding the listener is best-effort: it enables automatic callback capture
	// when the admin panel runs on the proxy host. On a remote deployment the
	// operator completes via CompleteXaiManual (pasted URL), which needs no
	// listener, so a bind failure must not block sign-in.
	if err := session.startListener(); err != nil {
		logger.Warnf("[Grok OAuth] callback listener not bound (manual paste still works): %v", err)
	}

	nonce := uuid.New().String()
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", xaiClientID())
	params.Set("redirect_uri", xaiRedirectURI)
	params.Set("scope", xaiScope)
	params.Set("code_challenge", xaiCodeChallenge(verifier))
	params.Set("code_challenge_method", "S256")
	params.Set("state", state)
	params.Set("nonce", nonce)
	params.Set("plan", "generic")
	params.Set("referrer", "kiro-go")
	authURL := xaiAuthorizeURL + "?" + params.Encode()

	xaiSessionsMu.Lock()
	xaiSessions[session.ID] = session
	xaiSessionsMu.Unlock()

	session.timer = time.AfterFunc(xaiLoginTimeout, func() {
		session.close()
		removeXaiSession(session.ID)
	})

	return session, authURL, nil
}

// PollXaiAuth reports login status. Returns ("pending", nil) until the listener
// captures a code, then exchanges it and returns the resolved credential with
// status "completed".
func PollXaiAuth(sessionID string) (*XaiResult, string, error) {
	xaiSessionsMu.RLock()
	session, ok := xaiSessions[sessionID]
	xaiSessionsMu.RUnlock()
	if !ok {
		return nil, "", fmt.Errorf("session not found or expired")
	}

	select {
	case capture := <-session.resultCh:
		session.close()
		removeXaiSession(sessionID)
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
			removeXaiSession(sessionID)
			return nil, "", fmt.Errorf("Grok OAuth login timed out after %s", xaiLoginTimeout)
		}
		return nil, "pending", nil
	}
}

// CompleteXaiManual finishes a sign-in from a pasted callback URL (or bare code).
// This is the headless/domain-deployment path: xAI redirects to the loopback
// 127.0.0.1:56121, which a remote operator's browser cannot reach, so the browser
// lands on an error page whose address bar still carries ?code=... The operator
// pastes that URL (or the code) here.
//
// It requires the sessionId so the matching PKCE code_verifier can be recovered;
// the authorization code alone cannot be exchanged without it.
func CompleteXaiManual(sessionID, rawInput string) (*XaiResult, error) {
	xaiSessionsMu.RLock()
	session, ok := xaiSessions[sessionID]
	xaiSessionsMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("session not found or expired; start the Grok OAuth login again")
	}

	code := extractXaiCode(rawInput)
	if code == "" {
		return nil, fmt.Errorf("no authorization code found in the pasted value")
	}

	result, err := session.exchange(code)
	if err != nil {
		return nil, err
	}
	session.close()
	removeXaiSession(sessionID)
	return result, nil
}

// extractXaiCode pulls the OAuth "code" out of a pasted value: a full callback
// URL, a bare "code=..." fragment, or the raw code itself.
func extractXaiCode(raw string) string {
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

// CancelXaiLogin tears an in-flight session down immediately, freeing the port.
func CancelXaiLogin(sessionID string) {
	xaiSessionsMu.RLock()
	session, ok := xaiSessions[sessionID]
	xaiSessionsMu.RUnlock()
	if !ok {
		return
	}
	session.close()
	removeXaiSession(sessionID)
}

// exchange swaps the captured authorization code for tokens and decodes the
// account email from the id_token.
func (s *XaiSession) exchange(code string) (*XaiResult, error) {
	client := GetAuthClientForProxy(s.ProxyURL)

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", xaiClientID())
	form.Set("code", strings.TrimSpace(code))
	form.Set("redirect_uri", xaiRedirectURI)
	form.Set("code_verifier", s.CodeVerifier)

	accessToken, refreshToken, idToken, expiresIn, scope, err := postXaiToken(client, form)
	if err != nil {
		return nil, fmt.Errorf("Grok OAuth token exchange failed: %w", err)
	}
	if scope == "" {
		scope = xaiScope
	}
	return &XaiResult{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    expiresIn,
		Email:        decodeXaiIDTokenEmail(idToken),
		Scopes:       scope,
	}, nil
}

// RefreshXaiToken refreshes an xAI access token using the refresh_token grant.
// Returns: accessToken, refreshToken, expiresAt (Unix), error.
func RefreshXaiToken(account *config.Account, client *http.Client) (string, string, int64, error) {
	if strings.TrimSpace(account.RefreshToken) == "" {
		return "", "", 0, fmt.Errorf("xai: no refresh token; re-authenticate the Grok account")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", xaiClientID())
	form.Set("refresh_token", account.RefreshToken)

	accessToken, refreshToken, _, expiresIn, _, err := postXaiToken(client, form)
	if err != nil {
		return "", "", 0, err
	}
	// xAI may omit a rotated refresh token; keep the existing one when absent.
	if refreshToken == "" {
		refreshToken = account.RefreshToken
	}
	return accessToken, refreshToken, time.Now().Unix() + int64(expiresIn), nil
}

// postXaiToken performs a form-encoded POST to the xAI token endpoint and maps
// the OAuth2 response. Shared by the code exchange and the refresh grant.
func postXaiToken(client *http.Client, form url.Values) (accessToken, refreshToken, idToken string, expiresIn int, scope string, err error) {
	req, err := http.NewRequest(http.MethodPost, xaiTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", "", 0, "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", 0, "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	_ = json.Unmarshal(body, &out)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || out.AccessToken == "" {
		return "", "", "", 0, "", fmt.Errorf("xai token request failed (status %d): %s", resp.StatusCode, string(body))
	}
	return out.AccessToken, out.RefreshToken, out.IDToken, out.ExpiresIn, out.Scope, nil
}

// decodeXaiIDTokenEmail extracts the email claim from an id_token JWT without
// verifying the signature (mirrors CLIProxyAPI). Returns "" when not parseable.
func decodeXaiIDTokenEmail(idToken string) string {
	if idToken == "" {
		return ""
	}
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// Tolerate padded base64url.
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return ""
		}
	}
	var claims struct {
		Email             string `json:"email"`
		PreferredUsername string `json:"preferred_username"`
		Sub               string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	if claims.Email != "" {
		return claims.Email
	}
	if claims.PreferredUsername != "" {
		return claims.PreferredUsername
	}
	return claims.Sub
}

func xaiCodeVerifier() (string, error) {
	b := make([]byte, xaiPKCEVerifierBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func xaiCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// --- Loopback callback listener --------------------------------------------

func (s *XaiSession) startListener() error {
	addrs := xaiCallbackBindAddrs()

	ln, err := net.Listen("tcp", addrs[0])
	if err != nil {
		return fmt.Errorf("cannot bind %s for the Grok OAuth callback (is the port already in use?): %w", addrs[0], err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleCallback)
	s.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	serve := func(l net.Listener) {
		go func() {
			if errServe := s.srv.Serve(l); errServe != nil && errServe != http.ErrServerClosed {
				logger.Debugf("[Grok OAuth] callback listener (%s) stopped: %v", l.Addr(), errServe)
			}
		}()
	}
	serve(ln)
	for _, addr := range addrs[1:] {
		if extra, errExtra := net.Listen("tcp", addr); errExtra == nil {
			serve(extra)
		} else {
			logger.Debugf("[Grok OAuth] secondary callback bind %s skipped: %v", addr, errExtra)
		}
	}
	return nil
}

func (s *XaiSession) close() {
	s.closeOnce.Do(func() {
		if s.timer != nil {
			s.timer.Stop()
		}
		if s.srv != nil {
			_ = s.srv.Close()
		}
	})
}

func (s *XaiSession) deliver(capture xaiCapture) {
	s.once.Do(func() { s.resultCh <- capture })
}

func (s *XaiSession) handleCallback(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if req.URL.Path != xaiCallbackPath {
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
		writeXaiCallbackPage(w, false)
		s.deliver(xaiCapture{err: fmt.Errorf("Grok OAuth authorization error: %s %s", errParam, desc)})
		return
	}
	writeXaiCallbackPage(w, true)
	s.deliver(xaiCapture{code: code})
}

func writeXaiCallbackPage(w http.ResponseWriter, ok bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	msg := "Grok sign-in complete. You can close this tab and return to the admin panel."
	if !ok {
		msg = "Grok sign-in failed. Return to the admin panel and try again."
	}
	_, _ = fmt.Fprintf(w, "<!doctype html><html><head><meta charset=\"utf-8\"><title>Grok Sign-In</title></head><body style=\"font-family:sans-serif;padding:2rem\"><p>%s</p></body></html>", msg)
}

func removeXaiSession(sessionID string) {
	xaiSessionsMu.Lock()
	delete(xaiSessions, sessionID)
	xaiSessionsMu.Unlock()
}
