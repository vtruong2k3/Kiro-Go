package proxy

import (
	"bytes"
	"context"
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
const remoteAnthropicVersion = "2023-06-01"

// remoteChatURL builds the OpenAI chat completions URL for a normalized base.
func remoteChatURL(base string) string {
	return strings.TrimRight(base, "/") + "/v1/chat/completions"
}

// remoteMessagesURL builds the Anthropic messages URL for a normalized base.
func remoteMessagesURL(base string) string {
	return strings.TrimRight(base, "/") + "/v1/messages"
}

// remoteModelsURL builds the OpenAI models list URL for a normalized base.
func remoteModelsURL(base string) string {
	return strings.TrimRight(base, "/") + "/v1/models"
}

// CallRemoteKiroAPI proxies generation to an OpenAI-compatible remote.
// Claude-origin requests are translated to the standard OpenAI chat/completions
// schema, so every remote call uses the same endpoint and response parser.
//
// ctx is the caller's request context; the upstream request is derived from it so
// a client disconnect cancels the generation on the peer too.
func CallRemoteKiroAPI(ctx context.Context, account *config.Account, payload *KiroPayload, callback *KiroStreamCallback) error {
	if ctx == nil {
		ctx = context.Background()
	}
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

	var bodyBytes []byte
	switch {
	case payload.SourceClaude != nil:
		converted, convErr := ClaudeToOpenAI(payload.SourceClaude, payload.SourceThinking)
		if convErr != nil {
			return fmt.Errorf("remotekiro: convert claude request: %w", convErr)
		}
		if model != "" {
			converted["model"] = model
		}
		converted["stream"] = stream
		bodyBytes, err = json.Marshal(converted)
		if err != nil {
			return fmt.Errorf("remotekiro: marshal openai request: %w", err)
		}
	case payload.SourceOpenAI != nil:
		// Marshal through a map so stream:false is retained despite omitempty.
		reqCopy := *payload.SourceOpenAI
		if model != "" {
			reqCopy.Model = model
		}
		reqCopy.Stream = stream
		raw, mErr := json.Marshal(&reqCopy)
		if mErr != nil {
			return fmt.Errorf("remotekiro: marshal openai request: %w", mErr)
		}
		var asMap map[string]interface{}
		if err := json.Unmarshal(raw, &asMap); err != nil {
			return fmt.Errorf("remotekiro: remap openai request: %w", err)
		}
		asMap["model"] = model
		asMap["stream"] = stream
		bodyBytes, err = json.Marshal(asMap)
		if err != nil {
			return fmt.Errorf("remotekiro: marshal openai request: %w", err)
		}
	default:
		return fmt.Errorf("remotekiro: no source request on payload (need SourceClaude or SourceOpenAI)")
	}
	url := remoteChatURL(base)

	if logger.GetLevel() == logger.LevelDebug {
		logger.Debugf("[RemoteKiro] Request to %s (model=%s, stream=%v)", url, model, stream)
	}

	client := GetClientForProxy(ResolveAccountProxyURL(account))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	resp, err := doRemoteOpenAIRequest(ctx, client, url, bodyBytes, bearer, stream)
	if err != nil {
		return fmt.Errorf("remotekiro: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return newUpstreamError("remotekiro", resp.StatusCode, string(errBody), "")
	}

	// A peer that accepts the request then stalls mid-stream would otherwise block
	// forever; this client has Timeout: 0 and ResponseHeaderTimeout only covers
	// the wait for the first header.
	idleReader := newIdleTimeoutReader(resp.Body, streamIdleTimeout, cancel)
	defer idleReader.Stop()

	if stream {
		return parseGrokOpenAISSE(idleReader, callback, model)
	}
	return parseGrokOpenAIResponse(idleReader, callback, model)
}

func doRemoteOpenAIRequest(ctx context.Context, client *http.Client, url string, body []byte, bearer string, stream bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("User-Agent", fmt.Sprintf("%s (%s/%s)", remoteKiroUserAgent, runtime.GOOS, runtime.GOARCH))
	req.Header.Set("Accept", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	return client.Do(req)
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
		return nil, newUpstreamError("remotekiro", resp.StatusCode, string(body), fmt.Sprintf("models HTTP %d", resp.StatusCode))
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
		return nil, newUpstreamError("remotekiro", resp.StatusCode, string(body), fmt.Sprintf("check-key HTTP %d", resp.StatusCode))
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
