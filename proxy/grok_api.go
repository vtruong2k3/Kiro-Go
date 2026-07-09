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
	grokChatURL     = "https://api.x.ai/v1/chat/completions"
	grokUserAgent   = "kiro-go/1.0"
	grokMaxRetries  = 1 // simple for first implementation; pool handles failover
)

// CallGrokAPI routes the request to xAI (or Grok Web in the future).
func CallGrokAPI(account *config.Account, payload *KiroPayload, callback *KiroStreamCallback) error {
	if callback == nil {
		callback = &KiroStreamCallback{}
	}
	if account == nil {
		return fmt.Errorf("grok: account is nil")
	}
	if payload == nil {
		return fmt.Errorf("grok: payload is nil")
	}

	// Both auth modes send a Bearer token to https://api.x.ai. Grok Build OAuth
	// stores its (refreshable) access token on Account.AccessToken; the official
	// API-key mode stores a static key on Account.GrokAPIKey. Prefer the OAuth
	// access token when present, else fall back to the API key.
	bearer := strings.TrimSpace(account.AccessToken)
	if bearer == "" {
		bearer = strings.TrimSpace(account.GrokAPIKey)
	}
	if bearer == "" {
		return fmt.Errorf("grok: no credentials configured (sign in with Grok Build OAuth or set an xAI API key)")
	}

	model := resolvePayloadModelForGrok(payload)
	stream := isStreamRequested(payload)

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

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("grok: marshal request: %w", err)
	}

	if logger.GetLevel() == logger.LevelDebug {
		logger.Debugf("[Grok] Request to %s (model=%v, stream=%v)", grokChatURL, reqBody["model"], stream)
	}

	client := GetClientForProxy(ResolveAccountProxyURL(account))

	req, err := http.NewRequest(http.MethodPost, grokChatURL, bytes.NewReader(bodyBytes))
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

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("grok: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("grok: upstream error %d: %s", resp.StatusCode, string(errBody))
	}

	if stream {
		return parseGrokOpenAISSE(resp.Body, callback, model)
	}
	return parseGrokOpenAIResponse(resp.Body, callback, model)
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

	// Finalize
	if callback.OnComplete != nil {
		if inputTokens == 0 && outputTokens == 0 {
			// Rough estimate if upstream didn't send usage
			inputTokens = estimateTokens(fullContent.String())
			outputTokens = inputTokens
		}
		callback.OnComplete(inputTokens, outputTokens)
	}

	_ = lastFinish // currently unused but available for future finish reason mapping
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

	if callback.OnComplete != nil {
		inTok := resp.Usage.PromptTokens
		outTok := resp.Usage.CompletionTokens
		if inTok == 0 && outTok == 0 {
			inTok = estimateTokens(content)
			outTok = inTok
		}
		callback.OnComplete(inTok, outTok)
	}

	return nil
}

// estimateTokens is a very rough fallback (4 chars ≈ 1 token).
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
