package proxy

// grok_api.go implements the upstream call for Grok/xAI accounts.
//
// Both auth modes hit the same OpenAI-compatible endpoint and differ only in
// the Bearer token source:
//   - "oauth"  (Grok Build): refreshable access token on Account.AccessToken
//   - "apikey" (official xAI): static key on Account.GrokAPIKey
//   POST https://api.x.ai/v1/chat/completions
//   Authorization: Bearer <token>
//
// The implementation converts the preserved SourceClaude / SourceOpenAI
// into OpenAI format and drives the shared KiroStreamCallback.
//
// Streaming uses standard OpenAI SSE ("data: {...}" lines).
// Non-stream collects the full response then emits via callback.
//
// References from 9router:
//   - providers/registry/xai.js (baseUrl, responsesUrl)
//   - src/lib/oauth/services/xai.js (OAuth PKCE flow)

import (
	"bufio"
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
	"time"

	"github.com/google/uuid"
)

const (
	grokChatURL    = "https://api.x.ai/v1/chat/completions"
	grokModelsURL  = "https://api.x.ai/v1/models"
	grokUserAgent  = "kiro-go/1.0"
	grokMaxRetries = 3 // backoff for 429/502/503; pool handles failover
	grokModelsTO   = 20 * time.Second
)

// grokBearer returns the Bearer credential for an account (OAuth access token
// preferred, else static GrokAPIKey). Empty when neither is set.
func grokBearer(account *config.Account) string {
	if account == nil {
		return ""
	}
	if t := strings.TrimSpace(account.AccessToken); t != "" {
		return t
	}
	return strings.TrimSpace(account.GrokAPIKey)
}

// FetchGrokModels lists model ids from live GET https://api.x.ai/v1/models.
// On any failure the caller should fall back to grokModelInfos().
func FetchGrokModels(account *config.Account) ([]ModelInfo, error) {
	if account == nil {
		return nil, fmt.Errorf("grok: account is nil")
	}
	bearer := grokBearer(account)
	if bearer == "" {
		return nil, fmt.Errorf("grok: no credentials configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), grokModelsTO)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, grokModelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("grok: models request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", fmt.Sprintf("%s (%s/%s)", grokUserAgent, runtime.GOOS, runtime.GOARCH))

	// Use the REST client (bounded timeout) — model list is small and must not hang
	// the refresh loop the way a streaming chat client might.
	client := GetRestClientForProxy(ResolveAccountProxyURL(account))
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("grok: models request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, newUpstreamError("grok", resp.StatusCode, string(body), fmt.Sprintf("models HTTP %d", resp.StatusCode))
	}

	ids, err := parseOpenAIModelIDs(body)
	if err != nil {
		return nil, fmt.Errorf("grok: %w", err)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("grok: /v1/models returned no models")
	}

	infos := make([]ModelInfo, 0, len(ids))
	for _, id := range ids {
		infos = append(infos, ModelInfo{ModelId: id, ModelName: id})
	}
	out := enhanceGrokModelInfos(infos)
	if len(out) == 0 {
		return nil, fmt.Errorf("grok: /v1/models returned no grok-* models")
	}
	return out, nil
}

// resolveGrokModels prefers a live xAI catalog and falls back to the static list.
// always returns a non-empty slice so pool routing never goes blank.
func resolveGrokModels(account *config.Account) []ModelInfo {
	if account != nil {
		live, err := FetchGrokModels(account)
		if err == nil && len(live) > 0 {
			return live
		}
		if err != nil {
			logger.Warnf("[Grok] live /v1/models failed for %s, using static catalog: %v", account.Email, err)
		}
	}
	return grokModelInfos()
}

// CallGrokAPI routes the request to xAI (or Grok Web in the future).
// ctx is the caller's request context; the upstream request is derived from it so
// a client disconnect cancels the generation.
func CallGrokAPI(ctx context.Context, account *config.Account, payload *KiroPayload, callback *KiroStreamCallback) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if callback == nil {
		callback = &KiroStreamCallback{}
	}
	if account == nil {
		return fmt.Errorf("grok: account is nil")
	}
	if payload == nil {
		return fmt.Errorf("grok: payload is nil")
	}

	bearer := grokBearer(account)
	if bearer == "" {
		return fmt.Errorf("grok: no credentials configured (sign in with Grok Build OAuth or set an xAI API key)")
	}

	model := resolvePayloadModelForGrok(payload)
	stream := isStreamRequested(payload)

	// Image models are served by the dedicated xAI images endpoint, not
	// chat.completions. Extract the prompt, generate one image, and surface it as
	// an inline markdown data-URI through the shared callback (mirrors the handler
	// image path but keeps chat clients working).
	if isGrokImageModel(model) {
		prompt := extractGrokImagePrompt(payload)
		if strings.TrimSpace(prompt) == "" {
			return fmt.Errorf("grok image: no prompt found in request")
		}
		b64, mime, imgErr := CallGrokImageAPI(ctx, account, &CodexImageRequest{Model: model, Prompt: prompt, N: 1})
		if imgErr != nil {
			return imgErr
		}
		if callback.OnImage != nil {
			callback.OnImage(b64, mime, false)
		}
		if callback.OnComplete != nil {
			callback.OnComplete(0, 0)
		}
		return nil
	}

	// Build OpenAI-compatible request body
	var reqBody map[string]interface{}
	var err error

	switch {
	case payload.SourceClaude != nil:
		reqBody, err = ClaudeToOpenAI(payload.SourceClaude, payload.SourceThinking)
	case payload.SourceOpenAI != nil:
		reqBody, err = OpenAIToOpenAI(payload.SourceOpenAI)
	default:
		return fmt.Errorf("grok: no source request on payload (need SourceClaude or SourceOpenAI)")
	}
	if err != nil {
		return fmt.Errorf("grok: build request: %w", err)
	}

	// Override model from payload if the source didn't have a good one
	if model != "" {
		reqBody["model"] = resolveGrokModel(model)
	}
	reqBody["stream"] = stream

	// xAI only reports token usage in a streamed response when the request opts in
	// via stream_options.include_usage. Without it the final chunk carries no
	// `usage` object at all, which used to send this code down a fabricated
	// estimate path (see parseGrokOpenAISSE) and made every Grok account's token
	// accounting meaningless.
	if stream {
		reqBody["stream_options"] = map[string]interface{}{"include_usage": true}
	} else {
		delete(reqBody, "stream_options")
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("grok: marshal request: %w", err)
	}

	if logger.GetLevel() == logger.LevelDebug {
		logger.Debugf("[Grok] Request to %s (model=%v, stream=%v)", grokChatURL, reqBody["model"], stream)
	}

	client := GetClientForProxy(ResolveAccountProxyURL(account))

	// Derived from the caller's request context: a client disconnect cancels the
	// upstream call, and the idle watchdog below can cancel it independently.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, grokChatURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("grok: new request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("User-Agent", fmt.Sprintf("%s (%s/%s)", grokUserAgent, runtime.GOOS, runtime.GOARCH))
	req.Header.Set("Accept", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}

	// Retry transient 429/502/503 with exponential backoff (grokMaxRetries=3).
	lastErr := doGrokCallWithRetry(ctx, client, req, cancel, stream, callback, model)
	return lastErr
}

// resolvePayloadModelForGrok tries to extract the intended model.
func resolvePayloadModelForGrok(payload *KiroPayload) string {
	if payload == nil {
		return ""
	}
	// Prefer the model stored in the Kiro shape
	m := payload.ConversationState.CurrentMessage.UserInputMessage.ModelID
	if m != "" {
		return m
	}
	// Fallbacks
	if payload.SourceClaude != nil {
		return payload.SourceClaude.Model
	}
	if payload.SourceOpenAI != nil {
		return payload.SourceOpenAI.Model
	}
	return ""
}

// extractGrokImagePrompt pulls the text prompt for image generation from the
// preserved source request (last user message wins).
func extractGrokImagePrompt(payload *KiroPayload) string {
	if payload == nil {
		return ""
	}
	if payload.SourceOpenAI != nil {
		for i := len(payload.SourceOpenAI.Messages) - 1; i >= 0; i-- {
			m := payload.SourceOpenAI.Messages[i]
			if m.Role != "user" {
				continue
			}
			if s := extractOpenAIMessageText(m.Content); strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	if payload.SourceClaude != nil {
		for i := len(payload.SourceClaude.Messages) - 1; i >= 0; i-- {
			m := payload.SourceClaude.Messages[i]
			if m.Role != "user" {
				continue
			}
			if s, _, _ := extractClaudeUserContent(m.Content); strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return ""
}

func isStreamRequested(payload *KiroPayload) bool {
	if payload.SourceClaude != nil {
		return payload.SourceClaude.Stream
	}
	if payload.SourceOpenAI != nil {
		return payload.SourceOpenAI.Stream
	}
	// Default to streaming (most clients want it)
	return true
}

// ==================== SSE parsing (OpenAI format) ====================

// parseGrokOpenAISSE reads standard OpenAI SSE from Grok and drives the callback.
func parseGrokOpenAISSE(body io.Reader, callback *KiroStreamCallback, model string) error {
	scanner := bufio.NewScanner(body)
	// Grok can emit very large SSE lines (tool arguments, long reasoning). The
	// default 64KB scanner buffer would trigger bufio.ErrTooLong and truncate
	// the stream; raise the max token size to 1MB.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var fullContent strings.Builder
	var fullReasoning strings.Builder
	var inputTokens, outputTokens int
	var lastFinish string

	// Accumulate streamed tool calls by their delta index. OpenAI-style SSE
	// sends the id/name in the first delta and appends argument fragments in
	// later deltas for the same index.
	type toolAccum struct {
		id   string
		name string
		args strings.Builder
	}
	toolByIndex := map[int]*toolAccum{}
	var toolOrder []int

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		data = strings.TrimSpace(data)

		if data == "[DONE]" {
			break
		}

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			// Skip unparseable lines (common with some providers)
			continue
		}

		for _, ch := range chunk.Choices {
			if ch.Delta.Content != "" {
				fullContent.WriteString(ch.Delta.Content)
				if callback.OnText != nil {
					callback.OnText(ch.Delta.Content, false)
				}
			}
			if ch.Delta.ReasoningContent != "" {
				fullReasoning.WriteString(ch.Delta.ReasoningContent)
				if callback.OnText != nil {
					// Emit thinking as isThinking=true so UI can show it separately
					callback.OnText(ch.Delta.ReasoningContent, true)
				}
			}
			for _, tcd := range ch.Delta.ToolCalls {
				acc := toolByIndex[tcd.Index]
				if acc == nil {
					acc = &toolAccum{}
					toolByIndex[tcd.Index] = acc
					toolOrder = append(toolOrder, tcd.Index)
				}
				if tcd.ID != "" {
					acc.id = tcd.ID
				}
				if tcd.Function.Name != "" {
					acc.name = tcd.Function.Name
				}
				if tcd.Function.Arguments != "" {
					acc.args.WriteString(tcd.Function.Arguments)
				}
			}
			if ch.FinishReason != nil && *ch.FinishReason != "" {
				lastFinish = *ch.FinishReason
			}
		}

		if chunk.Usage != nil {
			inputTokens = chunk.Usage.PromptTokens
			outputTokens = chunk.Usage.CompletionTokens
		}
	}

	if err := scanner.Err(); err != nil {
		if callback.OnError != nil {
			callback.OnError(err)
		}
		return err
	}

	// Emit any accumulated tool calls before completing. Without this the
	// stream would end silently whenever the model chose to call a tool.
	if callback.OnToolUse != nil {
		for _, idx := range toolOrder {
			acc := toolByIndex[idx]
			if acc == nil || acc.name == "" {
				continue
			}
			input := map[string]interface{}{}
			if acc.args.Len() > 0 {
				_ = json.Unmarshal([]byte(acc.args.String()), &input)
			}
			id := acc.id
			if id == "" {
				id = "toolu_" + uuid.New().String()
			}
			callback.OnToolUse(KiroToolUse{
				ToolUseID: id,
				Name:      acc.name,
				Input:     input,
			})
		}
	}

	// Finalize. With stream_options.include_usage the final chunk always carries
	// usage; never fall back to estimateTokens.
	if callback.OnComplete != nil {
		callback.OnComplete(inputTokens, outputTokens)
	}

	if lastFinish != "" && callback.OnFinishReason != nil {
		callback.OnFinishReason(lastFinish)
	}

	_ = model
	return nil
}

// parseGrokOpenAIResponse handles the non-streaming case.
func parseGrokOpenAIResponse(body io.Reader, callback *KiroStreamCallback, model string) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("grok: read response: %w", err)
	}

	var resp openAIResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("grok: decode response: %w", err)
	}

	var content, reasoning string
	if len(resp.Choices) > 0 {
		c, r := extractTextFromOpenAIMessage(resp.Choices[0].Message)
		content = c
		reasoning = r

		// Emit tool calls if present
		if callback.OnToolUse != nil {
			for _, tc := range resp.Choices[0].Message.ToolCalls {
				if tc.ID != "" && tc.Function.Name != "" {
					input := map[string]interface{}{}
					_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)

					callback.OnToolUse(KiroToolUse{
						ToolUseID: tc.ID,
						Name:      tc.Function.Name,
						Input:     input,
					})
				}
			}
		}
	}

	if content != "" && callback.OnText != nil {
		callback.OnText(content, false)
	}
	if reasoning != "" && callback.OnText != nil {
		callback.OnText(reasoning, true)
	}

	if len(resp.Choices) > 0 && callback.OnFinishReason != nil {
		callback.OnFinishReason(resp.Choices[0].FinishReason)
	}

	if content == "" && reasoning == "" && len(resp.Choices) == 0 {
		err := fmt.Errorf("grok: empty response (model=%s)", model)
		if callback.OnError != nil {
			callback.OnError(err)
		}
		return err
	}
	if content == "" && reasoning == "" && len(resp.Choices) > 0 && len(resp.Choices[0].Message.ToolCalls) == 0 {
		err := fmt.Errorf("grok: empty response (model=%s)", model)
		if callback.OnError != nil {
			callback.OnError(err)
		}
		return err
	}

	if callback.OnComplete != nil {
		// Report upstream usage verbatim. Non-stream responses always carry a usage
		// block, so a zero here means xAI really reported zero — do not substitute
		// a guess (see parseGrokOpenAISSE for why).
		callback.OnComplete(resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	}

	return nil
}

// estimateTokens is a very rough fallback (4 chars ≈ 1 token). Used only where no
// upstream usage exists at all (the images endpoint returns no token counts).
func estimateTokens(s string) int {
	n := len(strings.TrimSpace(s))
	if n == 0 {
		return 0
	}
	return (n + 3) / 4
}

// GetGrokClientForTesting returns a client for unit tests (exported for tests only).
func GetGrokClientForTesting(proxyURL string) *http.Client {
	return GetClientForProxy(proxyURL)
}

// NewGrokRequestID generates a request id for logging / tracing.
func NewGrokRequestID() string {
	return "grok-" + uuid.New().String()[:8]
}

// GrokDefaultTimeout is the client timeout used for non-stream Grok calls.
var GrokDefaultTimeout = 5 * time.Minute

// isRetryableGrokStatus reports the explicit transient upstream statuses.
func isRetryableGrokStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusBadGateway || status == http.StatusServiceUnavailable
}

func cloneGrokRequest(ctx context.Context, template *http.Request) (*http.Request, error) {
	req := template.Clone(ctx)
	if template.Body == nil {
		return req, nil
	}
	if template.GetBody == nil {
		return nil, fmt.Errorf("grok: request body cannot be replayed")
	}
	body, err := template.GetBody()
	if err != nil {
		return nil, fmt.Errorf("grok: recreate request body: %w", err)
	}
	req.Body = body
	return req, nil
}

func waitForGrokRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// doGrokCallWithRetry makes at most grokMaxRetries total attempts. Parser
// failures after a 200 response are never retried, avoiding duplicate output.
func doGrokCallWithRetry(ctx context.Context, client *http.Client, template *http.Request, cancel context.CancelFunc, stream bool, callback *KiroStreamCallback, model string) error {
	var lastErr error
	backoff := 500 * time.Millisecond
	for attempt := 0; attempt < grokMaxRetries; attempt++ {
		req, err := cloneGrokRequest(ctx, template)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("grok: request failed: %w", err)
		} else if resp.StatusCode == http.StatusOK {
			idleReader := newIdleTimeoutReader(resp.Body, streamIdleTimeout, cancel)
			var parseErr error
			if stream {
				parseErr = parseGrokOpenAISSE(idleReader, callback, model)
			} else {
				parseErr = parseGrokOpenAIResponse(idleReader, callback, model)
			}
			idleReader.Stop()
			resp.Body.Close()
			return parseErr
		} else {
			status := resp.StatusCode
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = newUpstreamError("grok", status, string(errBody), "")
			if !isRetryableGrokStatus(status) {
				return lastErr
			}
		}
		if attempt == grokMaxRetries-1 {
			break
		}
		if err := waitForGrokRetry(ctx, backoff); err != nil {
			return fmt.Errorf("grok: retry canceled: %w", err)
		}
		backoff *= 2
		if backoff > 10*time.Second {
			backoff = 10 * time.Second
		}
	}
	return lastErr
}
