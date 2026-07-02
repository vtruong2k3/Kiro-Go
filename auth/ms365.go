package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Errors surfaced by the MS365 (Microsoft Entra "Sign in with your organization")
// flow.
var (
	ErrMs365InvalidToken = errors.New("ms365 token is invalid or incomplete")
)

// ms365AuthorizeBaseURL is the real Kiro "Sign in with your organization"
// entry point (confirmed by capturing the KiroIDE flow). Overridable in tests.
var ms365AuthorizeBaseURL = func() string {
	return "https://app.kiro.dev/signin"
}

// ms365LoopbackAddr is the fixed loopback address the Kiro org sign-in flow
// redirects back to. KiroIDE registers http://localhost:3128, so Kiro-Go must
// listen there and use the same redirect_uri.
const ms365LoopbackAddr = "127.0.0.1:3128"
const ms365RedirectURI = "http://localhost:3128"

// ms365MSRedirectURI is the redirect registered for Kiro's Entra application
// for the Microsoft leg. Confirmed from the real flow: Microsoft redirects to
// this custom scheme with ?code=... A web server cannot receive a custom
// scheme, so the operator pastes that redirected URL back and Kiro-Go performs
// the token exchange itself.
const ms365MSRedirectURI = "kiro://kiro.oauth/callback"

// Ms365Session holds the state for one in-flight org sign-in. The loopback
// server fills TokenEndpoint/ClientID/Scopes from the Kiro callback, then the
// Microsoft leg produces the final token stored in Result.
type Ms365Session struct {
	State         string
	CodeVerifier  string
	CodeChallenge string
	ExpiresAt     time.Time

	// Filled from the Kiro /signin/callback (external IdP details).
	TokenEndpoint string
	ClientID      string
	Scopes        string
	LoginHint     string

	// Completion result / error (set by the loopback handler).
	Result *Ms365Token
	Err    error
	Done   bool
}

var (
	ms365Sessions   = make(map[string]*Ms365Session)
	ms365SessionsMu sync.RWMutex

	ms365ServerOnce sync.Once
	ms365ServerErr  error
)

// Ms365Token is the resolved external_idp credential set.
type Ms365Token struct {
	AccessToken   string
	RefreshToken  string
	ExpiresAt     int64
	TokenEndpoint string
	ClientID      string
	Scopes        string
	Provider      string
	Email         string
}

// StartMs365Login begins an automated org sign-in: it generates PKCE + state,
// ensures the loopback capture server is running on :3128, and returns the
// real app.kiro.dev/signin authorization URL for the operator to open. The
// loopback server captures the callback and completes the flow automatically;
// the caller polls PollMs365Login for the result.
func StartMs365Login() (sessionID, authorizeURL string, expiresIn int, err error) {
	if err := ensureMs365Server(); err != nil {
		return "", "", 0, fmt.Errorf("cannot start local login listener on %s (is Kiro IDE running and holding the port?): %w", ms365LoopbackAddr, err)
	}

	codeVerifier := generateCodeVerifier()
	codeChallenge := generateCodeChallenge(codeVerifier)
	state := uuid.New().String()

	params := url.Values{}
	params.Set("state", state)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	params.Set("redirect_uri", ms365RedirectURI)
	params.Set("redirect_from", "KiroIDE")
	authorizeURL = fmt.Sprintf("%s?%s", ms365AuthorizeBaseURL(), params.Encode())

	sessionID = uuid.New().String()
	sess := &Ms365Session{
		State:         state,
		CodeVerifier:  codeVerifier,
		CodeChallenge: codeChallenge,
		ExpiresAt:     time.Now().Add(10 * time.Minute),
	}

	ms365SessionsMu.Lock()
	ms365Sessions[sessionID] = sess
	ms365SessionsMu.Unlock()

	go cleanupExpiredMs365Sessions()
	return sessionID, authorizeURL, 600, nil
}

// PollMs365Login reports whether the sign-in for sessionID has completed. When
// done and successful it returns the resolved token; on failure it returns the
// error. While pending it returns (false, nil, nil).
func PollMs365Login(sessionID string) (done bool, token *Ms365Token, err error) {
	ms365SessionsMu.RLock()
	sess, ok := ms365Sessions[sessionID]
	ms365SessionsMu.RUnlock()
	if !ok {
		return false, nil, ErrMs365InvalidToken
	}
	if !sess.Done {
		if time.Now().After(sess.ExpiresAt) {
			return false, nil, fmt.Errorf("login session expired")
		}
		return false, nil, nil
	}
	return true, sess.Result, sess.Err
}

// findMs365SessionByState returns the pending session matching state.
func findMs365SessionByState(state string) *Ms365Session {
	ms365SessionsMu.RLock()
	defer ms365SessionsMu.RUnlock()
	for _, s := range ms365Sessions {
		if s.State == state {
			return s
		}
	}
	return nil
}

// ensureMs365Server starts the loopback capture server once.
func ensureMs365Server() error {
	ms365ServerOnce.Do(func() {
		ln, err := net.Listen("tcp", ms365LoopbackAddr)
		if err != nil {
			ms365ServerErr = err
			return
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/", ms365LoopbackHandler)
		srv := &http.Server{Handler: mux}
		go srv.Serve(ln)
		// Register the OS handler for the kiro:// scheme so the Microsoft
		// callback is captured automatically (best-effort; Windows).
		registerMs365ProtocolHandler()
	})
	return ms365ServerErr
}

// ForwardMs365OSCallback is invoked by the `kiro-go ms365-callback <url>`
// subcommand that the OS launches when the browser navigates to
// kiro://kiro.oauth/callback?code=... It forwards the URL to the running
// login listener and returns.
func ForwardMs365OSCallback(rawURL string) {
	client := &http.Client{Timeout: 10 * time.Second}
	_, _ = client.Post("http://127.0.0.1:3128/ms365/oscallback", "text/plain", strings.NewReader(rawURL))
}

// registerMs365ProtocolHandler registers this executable as the handler for the
// kiro:// URL scheme (Windows, current user). This lets the Microsoft Entra
// redirect (kiro://kiro.oauth/callback?code=..) be captured automatically.
// Best-effort: failures are ignored (the manual paste path still works).
//
// Note: this overrides any existing kiro:// handler (e.g. the Kiro IDE) for the
// current user while Kiro-Go is the registered handler.
func registerMs365ProtocolHandler() {
	if runtime.GOOS != "windows" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	root := `HKCU\Software\Classes\kiro`
	cmd := fmt.Sprintf(`"%s" ms365-callback "%%1"`, exe)
	runs := [][]string{
		{"add", root, "/ve", "/d", "URL:Kiro Protocol", "/f"},
		{"add", root, "/v", "URL Protocol", "/d", "", "/f"},
		{"add", root + `\shell\open\command`, "/ve", "/d", cmd, "/f"},
	}
	for _, args := range runs {
		_ = exec.Command("reg", args...).Run()
	}
}

// completeMs365FromCallbackURL completes the pending session identified by the
// state in the pasted/forwarded redirect URL by exchanging its code for tokens.
func completeMs365FromCallbackURL(raw string) {
	code, state, oauthErr := parseMs365CodeState(raw)
	sess := findMs365SessionByState(state)
	if sess == nil {
		return
	}
	if oauthErr != "" {
		finishMs365(sess, nil, fmt.Errorf("%w: authorization denied (%s)", ErrMs365InvalidToken, oauthErr))
		return
	}
	if code == "" {
		return
	}
	tok, err := exchangeMs365Code(sess, code)
	finishMs365(sess, tok, err)
}

// ms365LoopbackHandler handles every request to :3128. It implements a
// two-leg OAuth2 flow:
//  1. Kiro's /signin/callback delivers the external-IdP details (issuer_url,
//     client_id, scopes). We store them and 302 the browser to the Microsoft
//     Entra authorize endpoint (authorization-code + PKCE).
//  2. Microsoft redirects back with ?code=..; we exchange it for tokens at the
//     IdP token endpoint and finish the session.
func ms365LoopbackHandler(w http.ResponseWriter, r *http.Request) {
	// OS-forwarded custom-scheme callback (kiro://...): the ms365-callback
	// subcommand POSTs the full redirect URL here so the session completes
	// automatically without any manual paste.
	if r.URL.Path == "/ms365/oscallback" {
		body, _ := io.ReadAll(r.Body)
		completeMs365FromCallbackURL(string(body))
		w.WriteHeader(http.StatusOK)
		return
	}

	q := r.URL.Query()
	state := q.Get("state")

	// Leg 2: Microsoft returned an authorization code.
	if code := q.Get("code"); code != "" {
		sess := findMs365SessionByState(state)
		if sess == nil {
			ms365WritePage(w, "Login session not found or expired. You can close this window.")
			return
		}
		if oauthErr := q.Get("error"); oauthErr != "" {
			finishMs365(sess, nil, fmt.Errorf("authorization error: %s %s", oauthErr, q.Get("error_description")))
			ms365WritePage(w, "Authorization was denied. You can close this window.")
			return
		}
		tok, err := exchangeMs365Code(sess, code)
		finishMs365(sess, tok, err)
		if err != nil {
			ms365WritePage(w, "Sign-in failed: "+err.Error())
			return
		}
		ms365WritePage(w, "Microsoft 365 sign-in complete. You can close this window and return to Kiro-Go.")
		return
	}

	// Leg 1: Kiro delivered the external-IdP details.
	if issuer := q.Get("issuer_url"); issuer != "" {
		sess := findMs365SessionByState(state)
		if sess == nil {
			ms365WritePage(w, "Login session not found or expired. You can close this window.")
			return
		}
		sess.ClientID = q.Get("client_id")
		sess.Scopes = q.Get("scopes")
		sess.LoginHint = q.Get("login_hint")
		sess.TokenEndpoint = deriveMs365TokenEndpoint(issuer)

		authorizeURL := deriveMs365AuthorizeEndpoint(issuer)
		if authorizeURL == "" || sess.ClientID == "" {
			finishMs365(sess, nil, fmt.Errorf("callback missing issuer or client_id"))
			ms365WritePage(w, "Sign-in failed: incomplete callback. You can close this window.")
			return
		}

		p := url.Values{}
		p.Set("client_id", sess.ClientID)
		p.Set("response_type", "code")
		p.Set("redirect_uri", ms365MSRedirectURI)
		p.Set("scope", sess.Scopes)
		p.Set("code_challenge", sess.CodeChallenge)
		p.Set("code_challenge_method", "S256")
		p.Set("state", state)
		p.Set("response_mode", "query")
		if sess.LoginHint != "" {
			p.Set("login_hint", sess.LoginHint)
		}
		http.Redirect(w, r, authorizeURL+"?"+p.Encode(), http.StatusFound)
		return
	}

	ms365WritePage(w, "Waiting for Microsoft 365 sign-in… you can return to Kiro-Go.")
}

// CompleteMs365Login finishes the org sign-in from the redirected URL the
// operator pastes back (e.g. "kiro://kiro.oauth/callback?code=..&state=..").
// It locates the in-flight session (which the loopback populated with the IdP
// token endpoint, client id and scopes during the app.kiro.dev callback) and
// exchanges the authorization code for tokens directly at the IdP.
func CompleteMs365Login(sessionID, callback string) (*Ms365Token, error) {
	code, state, oauthErr := parseMs365CodeState(callback)
	if oauthErr != "" {
		return nil, fmt.Errorf("%w: authorization denied (%s)", ErrMs365InvalidToken, oauthErr)
	}
	if code == "" {
		return nil, fmt.Errorf("%w: no authorization code found in the pasted URL", ErrMs365InvalidToken)
	}

	var sess *Ms365Session
	ms365SessionsMu.RLock()
	if sessionID != "" {
		sess = ms365Sessions[sessionID]
	}
	ms365SessionsMu.RUnlock()
	if sess == nil && state != "" {
		sess = findMs365SessionByState(state)
	}
	if sess == nil {
		return nil, fmt.Errorf("%w: login session not found or expired", ErrMs365InvalidToken)
	}
	if sess.TokenEndpoint == "" || sess.ClientID == "" {
		return nil, fmt.Errorf("%w: sign-in did not reach the organization step; start again and complete the org sign-in", ErrMs365InvalidToken)
	}

	tok, err := exchangeMs365Code(sess, code)
	if err != nil {
		return nil, err
	}
	finishMs365(sess, tok, nil)
	return tok, nil
}

// parseMs365CodeState extracts code/state/error from a pasted redirect URL
// (custom scheme kiro:// or otherwise). If the input is not a URL it is treated
// as a bare authorization code.
func parseMs365CodeState(raw string) (code, state, oauthErr string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", ""
	}
	if u, err := url.Parse(raw); err == nil && u.RawQuery != "" {
		q := u.Query()
		return q.Get("code"), q.Get("state"), q.Get("error")
	}
	// Not a URL with a query: treat the whole string as the code.
	return raw, "", ""
}


func exchangeMs365Code(sess *Ms365Session, code string) (*Ms365Token, error) {
	if sess.TokenEndpoint == "" || sess.ClientID == "" {
		return nil, fmt.Errorf("missing token endpoint or client id")
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", sess.ClientID)
	form.Set("code", code)
	form.Set("redirect_uri", ms365MSRedirectURI)
	form.Set("code_verifier", sess.CodeVerifier)
	if sess.Scopes != "" {
		form.Set("scope", sess.Scopes)
	}

	req, _ := http.NewRequest("POST", sess.TokenEndpoint, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("token exchange failed: %d %s", resp.StatusCode, string(body))
	}

	var res struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}
	if res.AccessToken == "" || res.RefreshToken == "" {
		return nil, fmt.Errorf("token exchange returned no tokens")
	}

	return &Ms365Token{
		AccessToken:   res.AccessToken,
		RefreshToken:  res.RefreshToken,
		ExpiresAt:     time.Now().Unix() + int64(res.ExpiresIn),
		TokenEndpoint: sess.TokenEndpoint,
		ClientID:      sess.ClientID,
		Scopes:        sess.Scopes,
		Provider:      "ExternalIdp",
		Email:         emailFromJWT(res.AccessToken),
	}, nil
}

func finishMs365(sess *Ms365Session, tok *Ms365Token, err error) {
	ms365SessionsMu.Lock()
	sess.Result = tok
	sess.Err = err
	sess.Done = true
	ms365SessionsMu.Unlock()
}

func ms365WritePage(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<!doctype html><html><head><meta charset='utf-8'><title>Kiro-Go</title></head><body style='font-family:system-ui;background:#0f0f14;color:#eee;display:flex;align-items:center;justify-content:center;height:100vh'><div style='text-align:center'><h2>Kiro-Go · Microsoft 365</h2><p>%s</p></div></body></html>", msg)
}

func cleanupExpiredMs365Sessions() {
	ms365SessionsMu.Lock()
	defer ms365SessionsMu.Unlock()
	now := time.Now()
	for id, s := range ms365Sessions {
		if now.After(s.ExpiresAt) && (s.Done || now.After(s.ExpiresAt.Add(5*time.Minute))) {
			delete(ms365Sessions, id)
		}
	}
}

// deriveMs365TokenEndpoint: https://login.microsoftonline.com/{tenant}/v2.0
// -> https://login.microsoftonline.com/{tenant}/oauth2/v2.0/token
func deriveMs365TokenEndpoint(issuer string) string {
	base := ms365IssuerBase(issuer)
	if base == "" {
		return ""
	}
	return base + "/oauth2/v2.0/token"
}

// deriveMs365AuthorizeEndpoint: issuer -> .../oauth2/v2.0/authorize
func deriveMs365AuthorizeEndpoint(issuer string) string {
	base := ms365IssuerBase(issuer)
	if base == "" {
		return ""
	}
	return base + "/oauth2/v2.0/authorize"
}

func ms365IssuerBase(issuer string) string {
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	if issuer == "" {
		return ""
	}
	return strings.TrimSuffix(issuer, "/v2.0")
}

// ParseMs365Token parses a pasted token JSON into an Ms365Token (manual
// fallback path). Kept for callers that supply a ready token bundle.
func ParseMs365Token(raw string) (*Ms365Token, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%w: empty token", ErrMs365InvalidToken)
	}
	var t struct {
		AccessToken   string `json:"accessToken"`
		RefreshToken  string `json:"refreshToken"`
		ExpiresAt     string `json:"expiresAt"`
		Provider      string `json:"provider"`
		TokenEndpoint string `json:"tokenEndpoint"`
		IssuerUrl     string `json:"issuerUrl"`
		ClientID      string `json:"clientId"`
		Scopes        string `json:"scopes"`
	}
	if err := json.Unmarshal([]byte(raw), &t); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMs365InvalidToken, err)
	}
	if t.AccessToken == "" || t.RefreshToken == "" {
		return nil, fmt.Errorf("%w: missing accessToken or refreshToken", ErrMs365InvalidToken)
	}
	if t.TokenEndpoint == "" && t.IssuerUrl != "" {
		t.TokenEndpoint = deriveMs365TokenEndpoint(t.IssuerUrl)
	}
	if t.TokenEndpoint == "" || t.ClientID == "" {
		return nil, fmt.Errorf("%w: missing tokenEndpoint or clientId", ErrMs365InvalidToken)
	}
	provider := t.Provider
	if provider == "" {
		provider = "ExternalIdp"
	}
	var expiresAt int64
	if ts, err := time.Parse(time.RFC3339, t.ExpiresAt); err == nil {
		expiresAt = ts.Unix()
	} else {
		expiresAt = time.Now().Add(time.Hour).Unix()
	}
	return &Ms365Token{
		AccessToken:   t.AccessToken,
		RefreshToken:  t.RefreshToken,
		ExpiresAt:     expiresAt,
		TokenEndpoint: t.TokenEndpoint,
		ClientID:      t.ClientID,
		Scopes:        t.Scopes,
		Provider:      provider,
		Email:         emailFromJWT(t.AccessToken),
	}, nil
}

// emailFromJWT best-effort decodes "preferred_username"/"email"/"upn".
func emailFromJWT(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		PreferredUsername string `json:"preferred_username"`
		Email             string `json:"email"`
		UPN               string `json:"upn"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	if claims.PreferredUsername != "" {
		return claims.PreferredUsername
	}
	if claims.Email != "" {
		return claims.Email
	}
	return claims.UPN
}
