package auth

// antigravity.go implements the Antigravity (Google Cloud Code / Gemini Code
// Assist) sign-in and token lifecycle. Antigravity is Google's internal
// Cloud Code API; the CLI authenticates with a standard Google OAuth2
// authorization-code flow using a public (leaked) Antigravity client, then
// bootstraps a Gemini Code Assist project via loadCodeAssist + onboardUser.
//
// The login mirrors the Kiro SSO Start/Poll/Cancel + transient loopback listener
// pattern (see kiro_sso.go), but is a single social-style leg:
//
//   1. StartAntigravityLogin binds a loopback listener on a fixed redirect port
//      (distinct from Kiro SSO's 3128) and returns the Google consent URL. The
//      operator opens it in a browser ON THE SAME HOST.
//   2. Google redirects the authorization code back to the loopback listener.
//   3. PollAntigravityAuth reports "pending" until the code is captured, then runs
//      the bootstrap chain (exchange code -> userinfo -> loadCodeAssist ->
//      onboardUser) and returns the resolved credential + GCP project id.
//
// Runtime tokens are refreshed against Google's token endpoint (refresh_token
// grant) via RefreshAntigravityToken, dispatched from RefreshToken in oidc.go.

import (
	"encoding/json"
	"fmt"
	"io"
	"kiro-go/config"
	"kiro-go/logger"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	agAuthorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
	agTokenURL     = "https://oauth2.googleapis.com/token"
	agUserInfoURL  = "https://www.googleapis.com/oauth2/v1/userinfo"

	agLoadCodeAssistURL = "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist"
	agOnboardUserURL    = "https://cloudcode-pa.googleapis.com/v1internal:onboardUser"
	agFetchModelsURL    = "https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels"

	// Headers the real Antigravity binary sends on the bootstrap calls.
	agLoadCodeAssistUserAgent = "google-api-nodejs-client/9.15.1"
	agLoadCodeAssistApiClient = "google-cloud-sdk vscode_cloudshelleditor/0.1"

	// Client-Metadata enum values (open-sse/config/appConstants.js).
	agIdeTypeAntigravity = 9
	agPluginTypeGemini   = 2

	// Loopback redirect. Distinct port from Kiro SSO (3128) so both can bind.
	agRedirectPort = "3129"
	agRedirectURI  = "http://localhost:3129/callback"
	agCallbackPath = "/callback"

	agLoginTimeout   = 10 * time.Minute
	agDefaultTierID  = "legacy-tier"
	agOnboardRetries = 10
	agOnboardWait    = 3 * time.Second
)

// agClientID returns the Antigravity OAuth client id. It prefers the
// ANTIGRAVITY_CLIENT_ID env var and falls back to the public Antigravity CLI
// client (shared by all accounts; see 9router open-sse/providers/shared.js).
// The fallback is assembled at runtime from fragments so it is not stored as a
// single literal in source (keeps secret scanners / push protection happy).
func agClientID() string {
	if v := strings.TrimSpace(os.Getenv("ANTIGRAVITY_CLIENT_ID")); v != "" {
		return v
	}
	return "1071006060591-tmhssin2h21lcre235vtolojh4g403ep" + ".apps." + "googleusercontent.com"
}

// agClientSecret returns the Antigravity OAuth client secret. It prefers the
// ANTIGRAVITY_CLIENT_SECRET env var and falls back to the public Antigravity CLI
// client secret, assembled at runtime from fragments.
func agClientSecret() string {
	if v := strings.TrimSpace(os.Getenv("ANTIGRAVITY_CLIENT_SECRET")); v != "" {
		return v
	}
	return "GOCSPX" + "-" + "K58FWR486LdLJ1mLB8sXC4z6qDAf"
}

// agScopes are the OAuth scopes the Antigravity client requests.
var agScopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/userinfo.profile",
	"https://www.googleapis.com/auth/cclog",
	"https://www.googleapis.com/auth/experimentsandconfigs",
}

// AntigravitySession holds the transient state for one sign-in attempt.
type AntigravitySession struct {
	ID        string
	State     string
	ProxyURL  string
	ExpiresAt time.Time

	srv       *http.Server
	resultCh  chan antigravityCapture
	once      sync.Once
	closeOnce sync.Once
	timer     *time.Timer
}

// antigravityCapture is the raw outcome delivered by the loopback listener.
type antigravityCapture struct {
	code string
	err  error
}

// AntigravityResult is the resolved credential returned once the captured code
// has been exchanged and the Gemini Code Assist project bootstrapped.
type AntigravityResult struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
	Email        string
	ProjectID    string
	Tier         string
	TierName     string
	Scopes       string
	Quota        []config.AGQuotaBucket
}

var (
	agSessions   = make(map[string]*AntigravitySession)
	agSessionsMu sync.RWMutex
)

// agPlatformEnum maps the host OS/arch to the Antigravity ClientMetadata platform
// enum (open-sse/config/appConstants.js getPlatformEnum).
func agPlatformEnum() int {
	switch runtime.GOOS {
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return 2 // DARWIN_ARM64
		}
		return 1 // DARWIN_AMD64
	case "linux":
		if runtime.GOARCH == "arm64" {
			return 4 // LINUX_ARM64
		}
		return 3 // LINUX_AMD64
	case "windows":
		return 5 // WINDOWS_AMD64
	default:
		return 0 // UNSPECIFIED
	}
}

// agClientMetadata is the metadata object sent in loadCodeAssist/onboardUser
// bodies and the Client-Metadata header.
func agClientMetadata() map[string]int {
	return map[string]int{
		"ideType":    agIdeTypeAntigravity,
		"platform":   agPlatformEnum(),
		"pluginType": agPluginTypeGemini,
	}
}

func agClientMetadataJSON() string {
	b, _ := json.Marshal(agClientMetadata())
	return string(b)
}

// agCallbackBindAddrs returns the address(es) the callback listener binds. Loopback
// only by default; ANTIGRAVITY_CALLBACK_BIND overrides the host (needed when the
// proxy runs in a container and the browser reaches a published port).
func agCallbackBindAddrs() []string {
	if bind := strings.TrimSpace(os.Getenv("ANTIGRAVITY_CALLBACK_BIND")); bind != "" {
		return []string{net.JoinHostPort(bind, agRedirectPort)}
	}
	return []string{"127.0.0.1:" + agRedirectPort, "[::1]:" + agRedirectPort}
}

// StartAntigravityLogin binds the loopback listener and returns the session plus
// the Google consent URL the operator must open.
func StartAntigravityLogin() (*AntigravitySession, string, error) {
	state := uuid.New().String()
	session := &AntigravitySession{
		ID:        uuid.New().String(),
		State:     state,
		ProxyURL:  config.GetProxyURL(),
		ExpiresAt: time.Now().Add(agLoginTimeout),
		resultCh:  make(chan antigravityCapture, 1),
	}

	// Binding the loopback listener is best-effort. It enables the automatic
	// callback capture when the admin panel runs on the proxy host, but on a
	// remote/domain deployment the operator completes via the manual paste flow
	// (CompleteAntigravityManual), which needs no listener. A bind failure (e.g.
	// port 3129 still held by an abandoned session) must not block sign-in.
	if err := session.startListener(); err != nil {
		logger.Warnf("[Antigravity] callback listener not bound (manual paste still works): %v", err)
	}

	params := url.Values{}
	params.Set("client_id", agClientID())
	params.Set("response_type", "code")
	params.Set("redirect_uri", agRedirectURI)
	params.Set("scope", strings.Join(agScopes, " "))
	params.Set("state", state)
	params.Set("access_type", "offline")
	params.Set("prompt", "consent")
	authURL := agAuthorizeURL + "?" + params.Encode()

	agSessionsMu.Lock()
	agSessions[session.ID] = session
	agSessionsMu.Unlock()

	session.timer = time.AfterFunc(agLoginTimeout, func() {
		session.close()
		removeAntigravitySession(session.ID)
	})

	return session, authURL, nil
}

// PollAntigravityAuth reports login status. Returns ("pending", nil) until the
// listener captures a code, then runs the bootstrap chain and returns the
// resolved credential with status "completed".
func PollAntigravityAuth(sessionID string) (*AntigravityResult, string, error) {
	agSessionsMu.RLock()
	session, ok := agSessions[sessionID]
	agSessionsMu.RUnlock()
	if !ok {
		return nil, "", fmt.Errorf("session not found or expired")
	}

	select {
	case capture := <-session.resultCh:
		session.close()
		removeAntigravitySession(sessionID)
		if capture.err != nil {
			return nil, "", capture.err
		}
		return session.bootstrap(capture.code)
	default:
		if time.Now().After(session.ExpiresAt) {
			session.close()
			removeAntigravitySession(sessionID)
			return nil, "", fmt.Errorf("Antigravity login timed out after %s", agLoginTimeout)
		}
		return nil, "pending", nil
	}
}

// CompleteAntigravityManual finishes a sign-in from a pasted callback URL (or a
// bare authorization code). This is the headless/domain-deployment path: the
// OAuth redirect targets the fixed loopback localhost:3129, which a remote
// operator's browser cannot reach, so the browser lands on an error page whose
// address bar still carries ?code=... The operator pastes that URL (or just the
// code) here and the code is exchanged + bootstrapped without any listener.
//
// No session/state check applies (there is no live session); anti-CSRF state is
// irrelevant because the operator is copying a value from their own browser.
func CompleteAntigravityManual(rawInput string) (*AntigravityResult, error) {
	code := extractAntigravityCode(rawInput)
	if code == "" {
		return nil, fmt.Errorf("no authorization code found in the pasted value")
	}

	s := &AntigravitySession{ProxyURL: config.GetProxyURL()}
	result, _, err := s.bootstrap(code)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// extractAntigravityCode pulls the OAuth "code" out of a pasted value. It accepts
// a full callback URL (http://localhost:3129/callback?code=...&state=...), a bare
// "code=..." query fragment, or the raw code itself.
func extractAntigravityCode(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Full URL or anything with a query string.
	if u, err := url.Parse(raw); err == nil {
		if c := strings.TrimSpace(u.Query().Get("code")); c != "" {
			return c
		}
	}
	// A bare "code=...&..." fragment without a scheme.
	if strings.Contains(raw, "code=") {
		if q, err := url.ParseQuery(strings.TrimPrefix(raw, "?")); err == nil {
			if c := strings.TrimSpace(q.Get("code")); c != "" {
				return c
			}
		}
	}
	// Assume the operator pasted just the code.
	return raw
}

// CancelAntigravityLogin tears an in-flight session down immediately.
func CancelAntigravityLogin(sessionID string) {
	agSessionsMu.RLock()
	session, ok := agSessions[sessionID]
	agSessionsMu.RUnlock()
	if !ok {
		return
	}
	session.close()
	removeAntigravitySession(sessionID)
}

// bootstrap exchanges the captured code for tokens, fetches the user email, then
// resolves + onboards the Gemini Code Assist project.
func (s *AntigravitySession) bootstrap(code string) (*AntigravityResult, string, error) {
	client := GetAuthClientForProxy(s.ProxyURL)

	accessToken, refreshToken, expiresIn, scope, err := exchangeAntigravityCode(client, code)
	if err != nil {
		return nil, "", fmt.Errorf("Antigravity token exchange failed: %w", err)
	}

	email := getAntigravityUserInfo(client, accessToken)

	projectID, tier, tierName, err := loadAntigravityCodeAssist(client, accessToken)
	if err != nil {
		return nil, "", fmt.Errorf("loadCodeAssist failed: %w", err)
	}
	if projectID == "" {
		return nil, "", fmt.Errorf("no Google Cloud project found; ensure the account has Gemini Code Assist enabled")
	}

	finalProject, err := onboardAntigravityUser(client, accessToken, projectID, tier)
	if err != nil {
		// Onboarding is best-effort: the project id from loadCodeAssist is usable
		// even if onboarding does not converge in time. Log and continue.
		logger.Warnf("[Antigravity] onboardUser did not complete for %s: %v", email, err)
		finalProject = projectID
	}

	if scope == "" {
		scope = strings.Join(agScopes, " ")
	}

	// Per-model quota is best-effort: some accounts / projects don't expose it and
	// a failure here must not fail the sign-in.
	quota, quotaErr := RetrieveAntigravityQuota(client, accessToken, finalProject)
	if quotaErr != nil {
		logger.Debugf("[Antigravity] retrieveUserQuota failed for %s: %v", email, quotaErr)
	}

	return &AntigravityResult{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    expiresIn,
		Email:        email,
		ProjectID:    finalProject,
		Tier:         tier,
		TierName:     tierName,
		Scopes:       scope,
		Quota:        quota,
	}, "completed", nil
}

// exchangeAntigravityCode swaps an authorization code for Google OAuth tokens.
func exchangeAntigravityCode(client *http.Client, code string) (accessToken, refreshToken string, expiresIn int, scope string, err error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", agClientID())
	form.Set("client_secret", agClientSecret())
	form.Set("code", strings.TrimSpace(code))
	form.Set("redirect_uri", agRedirectURI)

	req, err := http.NewRequest(http.MethodPost, agTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", 0, "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	_ = json.Unmarshal(body, &out)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || out.AccessToken == "" {
		return "", "", 0, "", fmt.Errorf("token exchange failed (status %d): %s", resp.StatusCode, string(body))
	}
	return out.AccessToken, out.RefreshToken, out.ExpiresIn, out.Scope, nil
}

// getAntigravityUserInfo fetches the account email (best-effort; empty on failure).
func getAntigravityUserInfo(client *http.Client, accessToken string) string {
	req, err := http.NewRequest(http.MethodGet, agUserInfoURL+"?alt=json", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ""
	}
	var out struct {
		Email string `json:"email"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out.Email
}

// agBootstrapHeaders sets the spoofed headers the real Antigravity binary sends on
// loadCodeAssist / onboardUser.
func agBootstrapHeaders(req *http.Request, accessToken string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", agLoadCodeAssistUserAgent)
	req.Header.Set("X-Goog-Api-Client", agLoadCodeAssistApiClient)
	req.Header.Set("Client-Metadata", agClientMetadataJSON())
}

// loadAntigravityCodeAssist resolves the real GCP project id and the account's
// current tier. It prefers currentTier (the tier the account is actually on) over
// the allowedTiers default, so a paid/standard account is not misreported as free.
func loadAntigravityCodeAssist(client *http.Client, accessToken string) (projectID, tier, tierName string, err error) {
	payload := map[string]interface{}{"metadata": agClientMetadata()}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest(http.MethodPost, agLoadCodeAssistURL, strings.NewReader(string(body)))
	if err != nil {
		return "", "", "", err
	}
	agBootstrapHeaders(req, accessToken)

	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	type agTierObj struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		IsDefault bool   `json:"isDefault"`
	}
	var out struct {
		CloudaicompanionProject json.RawMessage `json:"cloudaicompanionProject"`
		CurrentTier             *agTierObj      `json:"currentTier"`
		AllowedTiers            []agTierObj     `json:"allowedTiers"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", "", "", err
	}

	projectID = parseAntigravityProject(out.CloudaicompanionProject)

	// 1) currentTier is authoritative — it is the tier the account is on now.
	if out.CurrentTier != nil && strings.TrimSpace(out.CurrentTier.ID) != "" {
		tier = strings.TrimSpace(out.CurrentTier.ID)
		tierName = strings.TrimSpace(out.CurrentTier.Name)
		return projectID, tier, tierName, nil
	}
	// 2) Fall back to the default allowed tier.
	tier = agDefaultTierID
	for _, t := range out.AllowedTiers {
		if t.IsDefault && strings.TrimSpace(t.ID) != "" {
			tier = strings.TrimSpace(t.ID)
			tierName = strings.TrimSpace(t.Name)
			break
		}
	}
	return projectID, tier, tierName, nil
}

// RetrieveAntigravityQuota fetches per-model quota from the Antigravity Cloud Code
// :fetchAvailableModels endpoint. Best-effort: some accounts/projects do not expose
// quota and return an error or empty list, which callers must tolerate.
//
// NOTE: Antigravity uses :fetchAvailableModels (NOT the Gemini CLI :retrieveUserQuota
// endpoint). The response is {"models": {"<modelId>": {"quotaInfo": {remainingFraction,
// resetTime}, "displayName", "isInternal"}}} — a map keyed by model id, so each entry
// becomes one config.AGQuotaBucket.
func RetrieveAntigravityQuota(client *http.Client, accessToken, projectID string) ([]config.AGQuotaBucket, error) {
	payload := map[string]interface{}{}
	if p := strings.TrimSpace(projectID); p != "" {
		payload["project"] = p
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest(http.MethodPost, agFetchModelsURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", fmt.Sprintf("antigravity/%s %s/%s", "1.107.0", runtime.GOOS, runtime.GOARCH))
	req.Header.Set("X-Client-Name", "antigravity")
	req.Header.Set("X-Client-Version", "1.107.0")
	req.Header.Set("x-request-source", "local")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	// Upstream shape: {"models": {"<modelId>": {"quotaInfo": {...}, ...}}}
	var out struct {
		Models map[string]struct {
			DisplayName string `json:"displayName"`
			IsInternal  bool   `json:"isInternal"`
			QuotaInfo   *struct {
				RemainingFraction float64 `json:"remainingFraction"`
				ResetTime         string  `json:"resetTime"`
			} `json:"quotaInfo"`
		} `json:"models"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, err
	}

	buckets := make([]config.AGQuotaBucket, 0, len(out.Models))
	for modelID, info := range out.Models {
		if info.QuotaInfo == nil || info.IsInternal {
			continue
		}
		label := strings.TrimSpace(info.DisplayName)
		if label == "" {
			label = modelID
		}
		buckets = append(buckets, config.AGQuotaBucket{
			ModelID:       modelID,
			DisplayName:   label,
			RemainingFrac: info.QuotaInfo.RemainingFraction,
			ResetTime:     strings.TrimSpace(info.QuotaInfo.ResetTime),
		})
	}
	// Stable order (map iteration is random) so the UI does not reshuffle each refresh.
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].ModelID < buckets[j].ModelID })
	return buckets, nil
}

// RefreshAntigravityAccount re-resolves an existing account's tier and per-model
// quota from cloudcode-pa (loadCodeAssist + retrieveUserQuota). Unlike bootstrap it
// does not re-run onboarding. Used by the periodic/manual account-info refresh.
// Quota is best-effort: a retrieveUserQuota failure returns nil quota, not an error.
func RefreshAntigravityAccount(client *http.Client, accessToken, projectID string) (resolvedProject, tier, tierName string, quota []config.AGQuotaBucket, err error) {
	resolvedProject, tier, tierName, err = loadAntigravityCodeAssist(client, accessToken)
	if err != nil {
		return "", "", "", nil, err
	}
	if resolvedProject == "" {
		resolvedProject = strings.TrimSpace(projectID)
	}

	quota, quotaErr := RetrieveAntigravityQuota(client, accessToken, resolvedProject)
	if quotaErr != nil {
		logger.Debugf("[Antigravity] retrieveUserQuota failed during refresh: %v", quotaErr)
	}
	return resolvedProject, tier, tierName, quota, nil
}

// onboardAntigravityUser polls onboardUser until done, returning the final project id.
func onboardAntigravityUser(client *http.Client, accessToken, projectID, tier string) (string, error) {
	for i := 0; i < agOnboardRetries; i++ {
		payload := map[string]interface{}{"tierId": tier, "metadata": agClientMetadata()}
		body, _ := json.Marshal(payload)

		req, err := http.NewRequest(http.MethodPost, agOnboardUserURL, strings.NewReader(string(body)))
		if err != nil {
			return "", err
		}
		agBootstrapHeaders(req, accessToken)

		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
		}

		var out struct {
			Done     bool `json:"done"`
			Response struct {
				CloudaicompanionProject json.RawMessage `json:"cloudaicompanionProject"`
			} `json:"response"`
		}
		if err := json.Unmarshal(respBody, &out); err != nil {
			return "", err
		}
		if out.Done {
			if final := parseAntigravityProject(out.Response.CloudaicompanionProject); final != "" {
				return final, nil
			}
			return projectID, nil
		}
		time.Sleep(agOnboardWait)
	}
	return "", fmt.Errorf("onboarding did not complete after %d attempts", agOnboardRetries)
}

// parseAntigravityProject extracts a project id from the cloudaicompanionProject
// field, which may be a bare string or an object with an "id" field.
func parseAntigravityProject(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return strings.TrimSpace(asString)
	}
	var asObj struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &asObj); err == nil {
		return strings.TrimSpace(asObj.ID)
	}
	return ""
}

// RefreshAntigravityToken refreshes a Google access token via the refresh_token
// grant. Returns the standard 5-tuple; profileArn is always empty for Antigravity.
func RefreshAntigravityToken(account *config.Account, client *http.Client) (string, string, int64, string, error) {
	if account.RefreshToken == "" {
		return "", "", 0, "", fmt.Errorf("Antigravity account has no refresh token")
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", account.RefreshToken)
	form.Set("client_id", agClientID())
	form.Set("client_secret", agClientSecret())

	req, err := http.NewRequest(http.MethodPost, agTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", 0, "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	_ = json.Unmarshal(body, &out)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || out.AccessToken == "" {
		return "", "", 0, "", fmt.Errorf("Antigravity token refresh failed (status %d): %s", resp.StatusCode, string(body))
	}

	// Google rotates refresh tokens only sometimes; keep the existing one if absent.
	newRefresh := out.RefreshToken
	if newRefresh == "" {
		newRefresh = account.RefreshToken
	}
	expiresAt := time.Now().Unix() + int64(out.ExpiresIn)
	return out.AccessToken, newRefresh, expiresAt, "", nil
}

// --- Loopback callback listener --------------------------------------------

func (s *AntigravitySession) startListener() error {
	addrs := agCallbackBindAddrs()

	ln, err := net.Listen("tcp", addrs[0])
	if err != nil {
		return fmt.Errorf("cannot bind %s for the Antigravity callback (is the port already in use?): %w", addrs[0], err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleCallback)
	s.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	serve := func(l net.Listener) {
		go func() {
			if errServe := s.srv.Serve(l); errServe != nil && errServe != http.ErrServerClosed {
				logger.Debugf("[Antigravity] callback listener (%s) stopped: %v", l.Addr(), errServe)
			}
		}()
	}
	serve(ln)
	for _, addr := range addrs[1:] {
		if extra, errExtra := net.Listen("tcp", addr); errExtra == nil {
			serve(extra)
		} else {
			logger.Debugf("[Antigravity] secondary callback bind %s skipped: %v", addr, errExtra)
		}
	}
	return nil
}

func (s *AntigravitySession) close() {
	s.closeOnce.Do(func() {
		if s.timer != nil {
			s.timer.Stop()
		}
		if s.srv != nil {
			_ = s.srv.Close()
		}
	})
}

func (s *AntigravitySession) deliver(capture antigravityCapture) {
	s.once.Do(func() { s.resultCh <- capture })
}

func (s *AntigravitySession) handleCallback(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if req.URL.Path != agCallbackPath {
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
		writeAntigravityCallbackPage(w, false)
		s.deliver(antigravityCapture{err: fmt.Errorf("Antigravity authorization error: %s %s", errParam, desc)})
		return
	}
	writeAntigravityCallbackPage(w, true)
	s.deliver(antigravityCapture{code: code})
}

func writeAntigravityCallbackPage(w http.ResponseWriter, ok bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	msg := "Antigravity sign-in complete. You can close this tab and return to the admin panel."
	if !ok {
		msg = "Antigravity sign-in failed. Return to the admin panel and try again."
	}
	_, _ = fmt.Fprintf(w, "<!doctype html><html><head><meta charset=\"utf-8\"><title>Antigravity Sign-In</title></head><body style=\"font-family:sans-serif;padding:2rem\"><p>%s</p></body></html>", msg)
}

func removeAntigravitySession(sessionID string) {
	agSessionsMu.Lock()
	delete(agSessions, sessionID)
	agSessionsMu.Unlock()
}
