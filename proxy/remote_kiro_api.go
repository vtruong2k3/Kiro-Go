package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"kiro-go/config"
	"kiro-go/logger"
	"net/http"
	"runtime"
	"strings"
)

const remoteKiroUserAgent = "kiro-go-remote/1.0"

// remoteChatURL builds the OpenAI chat completions URL for a normalized base.
func remoteChatURL(base string) string {
	return strings.TrimRight(base, "/") + "/v1/chat/completions"
}

// remoteModelsURL builds the OpenAI models list URL for a normalized base.
func remoteModelsURL(base string) string {
	return strings.TrimRight(base, "/") + "/v1/models"
}

// CallRemoteKiroAPI proxies generation to another Kiro-Go (or OpenAI-compatible)
// peer via POST {base}/v1/chat/completions. It rebuilds the request from
// SourceClaude / SourceOpenAI (same as Grok) and drives KiroStreamCallback via
// the shared OpenAI SSE/JSON parsers.
func CallRemoteKiroAPI(account *config.Account, payload *KiroPayload, callback *KiroStreamCallback) error {
	if callback == nil {
		callback = &KiroStreamCallback{}
	}
	if account == nil {
		return fmt.Errorf("remotekiro: account is nil")
	}
	if payload == nil {
		return fmt.Errorf("remotekiro: payload is nil")
	}

	base, err := validateRemoteBaseURL(account.RemoteBaseURL)
	if err != nil {
		return fmt.Errorf("remotekiro: %w", err)
	}
	bearer := strings.TrimSpace(account.AccessToken)
	if bearer == "" {
		return fmt.Errorf("remotekiro: no API key configured (AccessToken)")
	}

	model := resolvePayloadModelForGrok(payload)
	stream := isStreamRequested(payload)

	var reqBody map[string]interface{}
	switch {
	case payload.SourceClaude != nil:
		reqBody, err = ClaudeToOpenAI(payload.SourceClaude, payload.SourceThinking)
	case payload.SourceOpenAI != nil:
		reqBody, err = OpenAIToOpenAI(payload.SourceOpenAI)
	default:
		return fmt.Errorf("remotekiro: no source request on payload (need SourceClaude or SourceOpenAI)")
	}
	if err != nil {
		return fmt.Errorf("remotekiro: build request: %w", err)
	}

	// Pass model through as-is — do NOT apply Grok model aliases.
	if model != "" {
		reqBody["model"] = model
	}
	reqBody["stream"] = stream

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("remotekiro: marshal request: %w", err)
	}

	url := remoteChatURL(base)
	if logger.GetLevel() == logger.LevelDebug {
		logger.Debugf("[RemoteKiro] Request to %s (model=%v, stream=%v)", url, reqBody["model"], stream)
	}

	client := GetClientForProxy(ResolveAccountProxyURL(account))
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("remotekiro: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("User-Agent", fmt.Sprintf("%s (%s/%s)", remoteKiroUserAgent, runtime.GOOS, runtime.GOARCH))
	req.Header.Set("Accept", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("remotekiro: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("remotekiro: upstream error %d: %s", resp.StatusCode, string(errBody))
	}

	if stream {
		return parseGrokOpenAISSE(resp.Body, callback, model)
	}
	return parseGrokOpenAIResponse(resp.Body, callback, model)
}

// FetchRemoteKiroModels lists model IDs from a remote peer's GET /v1/models.
func FetchRemoteKiroModels(account *config.Account) ([]string, error) {
	if account == nil {
		return nil, fmt.Errorf("remotekiro: account is nil")
	}
	base, err := validateRemoteBaseURL(account.RemoteBaseURL)
	if err != nil {
		return nil, err
	}
	bearer := strings.TrimSpace(account.AccessToken)
	if bearer == "" {
		return nil, fmt.Errorf("remotekiro: no API key configured")
	}

	req, err := http.NewRequest(http.MethodGet, remoteModelsURL(base), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", remoteKiroUserAgent)

	resp, err := GetRestClientForProxy(ResolveAccountProxyURL(account)).Do(req)
	if err != nil {
		return nil, fmt.Errorf("remotekiro: models request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remotekiro: models HTTP %d: %s", resp.StatusCode, string(body))
	}

	ids, err := parseOpenAIModelIDs(body)
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// ValidateRemoteKiro validates base URL + sk by probing GET /v1/models.
// Returns the canonical base URL and non-empty model id list.
func ValidateRemoteKiro(baseURL, apiKey, proxyURL string) (canonical string, modelIDs []string, err error) {
	canonical, err = validateRemoteBaseURL(baseURL)
	if err != nil {
		return "", nil, err
	}
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return "", nil, fmt.Errorf("API key is required")
	}

	probe := &config.Account{
		RemoteBaseURL: canonical,
		AccessToken:   key,
		AuthMethod:    "remotekiro",
		Provider:      "remotekiro",
		ProxyURL:      strings.TrimSpace(proxyURL),
	}
	modelIDs, err = FetchRemoteKiroModels(probe)
	if err != nil {
		return "", nil, err
	}
	if len(modelIDs) == 0 {
		return "", nil, fmt.Errorf("remote /v1/models returned no models")
	}
	return canonical, modelIDs, nil
}

// parseOpenAIModelIDs extracts data[].id from an OpenAI-compatible models list body.
func parseOpenAIModelIDs(body []byte) ([]string, error) {
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("remotekiro: parse models: %w", err)
	}
	ids := make([]string, 0, len(parsed.Data))
	seen := make(map[string]bool, len(parsed.Data))
	for _, m := range parsed.Data {
		id := strings.TrimSpace(m.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, nil
}

// remoteCheckKeyResponse is the subset of a Kiro-Go-style check-key payload we
// consume. Both /check/api/lookup (stock) and /checkkey/info (forks) return
// these fields. creditLimit <= 0 means the remote key is unlimited.
type remoteCheckKeyResponse struct {
	Name             string  `json:"name"`
	Enabled          bool    `json:"enabled"`
	CreditLimit      float64 `json:"creditLimit"`
	CreditsUsed      float64 `json:"creditsUsed"`
	CreditsRemaining float64 `json:"creditsRemaining"`
	ExpiresAt        int64   `json:"expiresAt"`
}

// FetchRemoteKiroKeyCredit calls the peer's check-key endpoint with the account's
// sk and returns the parsed credit view. Requires account.RemoteCheckKeyURL to be
// set; the URL is SSRF-validated (host reuse of the base URL rules) before use.
func FetchRemoteKiroKeyCredit(account *config.Account) (*remoteCheckKeyResponse, error) {
	if account == nil {
		return nil, fmt.Errorf("remotekiro: account is nil")
	}
	checkURL := strings.TrimSpace(account.RemoteCheckKeyURL)
	if checkURL == "" {
		return nil, fmt.Errorf("remotekiro: no check-key URL configured")
	}
	if err := validateRemoteCheckKeyURL(checkURL); err != nil {
		return nil, err
	}
	bearer := strings.TrimSpace(account.AccessToken)
	if bearer == "" {
		return nil, fmt.Errorf("remotekiro: no API key configured")
	}

	reqBody, _ := json.Marshal(map[string]string{"key": bearer})
	req, err := http.NewRequest(http.MethodPost, checkURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", remoteKiroUserAgent)

	resp, err := GetRestClientForProxy(ResolveAccountProxyURL(account)).Do(req)
	if err != nil {
		return nil, fmt.Errorf("remotekiro: check-key request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remotekiro: check-key HTTP %d: %s", resp.StatusCode, string(body))
	}

	var parsed remoteCheckKeyResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("remotekiro: parse check-key: %w", err)
	}
	return &parsed, nil
}

// refreshRemoteKiroInfo mirrors the remote key's credit balance into AccountInfo.
// When RemoteCheckKeyURL is unset, it returns an empty info (no credit sync) so
// the account still refreshes without error. Credits map onto Usage* fields so
// the pool's over-quota skip (UsageCurrent >= UsageLimit) applies automatically
// once the remote key runs out.
func refreshRemoteKiroInfo(account *config.Account, info *config.AccountInfo) (*config.AccountInfo, error) {
	if strings.TrimSpace(account.RemoteCheckKeyURL) == "" {
		return info, nil
	}
	cred, err := FetchRemoteKiroKeyCredit(account)
	if err != nil {
		return nil, err
	}
	if cred.CreditLimit > 0 {
		info.UsageLimit = cred.CreditLimit
		info.UsageCurrent = cred.CreditsUsed
		info.UsagePercent = cred.CreditsUsed / cred.CreditLimit
	} else {
		// Unlimited remote key: clear any prior limit so the account is never
		// treated as over-quota.
		info.UsageLimit = 0
		info.UsageCurrent = cred.CreditsUsed
		info.UsagePercent = 0
	}
	return info, nil
}

// remoteModelInfos builds ModelInfo entries from bare model ids for admin UI / cache merge.
func remoteModelInfos(ids []string) []ModelInfo {
	out := make([]ModelInfo, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		out = append(out, ModelInfo{
			ModelId:   id,
			ModelName: id,
		})
	}
	return out
}
