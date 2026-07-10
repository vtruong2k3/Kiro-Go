package proxy

// codex_api.go implements the upstream call for Codex (ChatGPT) accounts.
//
//   POST https://chatgpt.com/backend-api/codex/responses
//   Authorization: Bearer <access token>
//   Accept: text/event-stream   (Codex always streams — forceStream)
//
// It builds an OpenAI Responses API body from the preserved SourceClaude /
// SourceOpenAI (via BuildCodexResponsesRequest) and parses the Responses SSE
// event stream into the shared KiroStreamCallback. This is the reverse of
// proxy/responses_handler.go, which emits these same events to clients.
//
// Source of truth: 9router open-sse/executors/codex.js +
// open-sse/translator/response/openai-responses.js.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"kiro-go/auth"
	"kiro-go/config"
	"kiro-go/logger"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

const (
	codexResponsesURL = "https://chatgpt.com/backend-api/codex/responses"
	codexUserAgent    = "codex_cli_rs/0.136.0"
	codexOriginator   = "codex_cli_rs"
	codexVersion      = "0.136.0"
)

// CallCodexAPI routes a generation request to the Codex (ChatGPT) upstream.
func CallCodexAPI(account *config.Account, payload *KiroPayload, callback *KiroStreamCallback) error {
	if callback == nil {
		callback = &KiroStreamCallback{}
	}
	if account == nil {
		return fmt.Errorf("codex: account is nil")
	}
	if payload == nil {
		return fmt.Errorf("codex: payload is nil")
	}

	bearer := strings.TrimSpace(account.AccessToken)
	if bearer == "" {
		return fmt.Errorf("codex: no access token (sign in with Codex OAuth or import a ChatGPT token)")
	}

	model := resolvePayloadModelForCodex(payload)
	sessionID := codexSessionID(account, payload)

	reqBody, err := BuildCodexResponsesRequest(payload.SourceClaude, payload.SourceOpenAI, model, sessionID, payload.SourceThinking)
	if err != nil {
		return fmt.Errorf("codex: build request: %w", err)
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("codex: marshal request: %w", err)
	}

	if logger.GetLevel() == logger.LevelDebug {
		logger.Debugf("[Codex] Request to %s (model=%v)", codexResponsesURL, reqBody["model"])
	}

	client := GetClientForProxy(ResolveAccountProxyURL(account))

	req, err := http.NewRequest(http.MethodPost, codexResponsesURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("codex: new request: %w", err)
	}

	accountID := strings.TrimSpace(account.CodexAccountID)
	if accountID == "" {
		_, accountID, _ = auth.DecodeCodexIDToken(account.CodexIDToken)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", codexUserAgent)
	req.Header.Set("originator", codexOriginator)
	req.Header.Set("version", codexVersion)
	req.Header.Set("session_id", sessionID)
	if accountID != "" {
		req.Header.Set("chatgpt-account-id", accountID)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("codex: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("codex: upstream error %d: %s", resp.StatusCode, string(errBody))
	}

	return parseCodexResponsesSSE(resp.Body, callback, model)
}

// CallCodexImageAPI drives the hosted image_generation tool on the Codex
// Responses endpoint and returns the final base64-encoded image. Image requests
// require a ChatGPT Plus (or higher) account; a free account yields no image and
// this returns an entitlement error.
func CallCodexImageAPI(account *config.Account, req *CodexImageRequest) (b64 string, mimeType string, err error) {
	if account == nil {
		return "", "", fmt.Errorf("codex: account is nil")
	}
	bearer := strings.TrimSpace(account.AccessToken)
	if bearer == "" {
		return "", "", fmt.Errorf("codex: no access token")
	}

	body := BuildCodexImageRequest(req)
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", "", fmt.Errorf("codex: marshal image request: %w", err)
	}

	client := GetClientForProxy(ResolveAccountProxyURL(account))
	httpReq, err := http.NewRequest(http.MethodPost, codexResponsesURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", "", err
	}

	accountID := strings.TrimSpace(account.CodexAccountID)
	if accountID == "" {
		_, accountID, _ = auth.DecodeCodexIDToken(account.CodexIDToken)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+bearer)
	httpReq.Header.Set("Accept", "text/event-stream, application/json")
	httpReq.Header.Set("User-Agent", codexUserAgent)
	httpReq.Header.Set("originator", codexOriginator)
	httpReq.Header.Set("version", codexVersion)
	httpReq.Header.Set("session_id", uuid.New().String())
	httpReq.Header.Set("x-client-request-id", uuid.New().String())
	if accountID != "" {
		httpReq.Header.Set("chatgpt-account-id", accountID)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return "", "", fmt.Errorf("codex: image request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("codex: image upstream error %d: %s", resp.StatusCode, string(errBody))
	}

	var finalImage string
	cb := &KiroStreamCallback{
		OnImage: func(b64 string, mime string, partial bool) {
			if !partial && b64 != "" {
				finalImage = b64
				mimeType = mime
			}
		},
	}
	if err := parseCodexResponsesSSE(resp.Body, cb, req.Model); err != nil {
		return "", "", err
	}
	if finalImage == "" {
		return "", "", fmt.Errorf("codex: no image returned (account may not be entitled — ChatGPT Plus or higher required)")
	}
	if mimeType == "" {
		mimeType = "image/png"
	}
	return finalImage, mimeType, nil
}

// resolvePayloadModelForCodex extracts the intended model id.
func resolvePayloadModelForCodex(payload *KiroPayload) string {
	if payload == nil {
		return ""
	}
	if m := payload.ConversationState.CurrentMessage.UserInputMessage.ModelID; m != "" {
		return m
	}
	if payload.SourceClaude != nil {
		return payload.SourceClaude.Model
	}
	if payload.SourceOpenAI != nil {
		return payload.SourceOpenAI.Model
	}
	return ""
}

// codexSessionID resolves a stable session id for prompt caching. Prefers a
// non-empty conversation id on the payload; otherwise a random uuid.
func codexSessionID(account *config.Account, payload *KiroPayload) string {
	if payload != nil {
		if id := strings.TrimSpace(payload.ConversationState.ConversationID); id != "" {
			return id
		}
	}
	return uuid.New().String()
}

// ==================== Responses SSE parsing ====================

// codexSSEEvent is one parsed SSE event (event name + JSON data payload).
type codexResponsesData struct {
	Type  string          `json:"type"`
	Delta string          `json:"delta"`
	Item  json.RawMessage `json:"item"`
	// response.completed / response.done carry usage inside a "response" object.
	Response struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		Error *codexErrorPayload `json:"error"`
	} `json:"response"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *codexErrorPayload `json:"error"`
	// image partials
	PartialImageB64 string `json:"partial_image_b64"`
}

type codexErrorPayload struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

// codexResponsesItem is a Responses output item (function_call / image_generation_call).
type codexResponsesItem struct {
	Type      string          `json:"type"`
	CallID    string          `json:"call_id"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments string          `json:"arguments"`
	Result    string          `json:"result"`
	Content   json.RawMessage `json:"content"`
}

// parseCodexResponsesSSE reads the Codex Responses SSE stream and drives the
// shared callback. Text deltas, reasoning summaries, function calls, image
// output, usage, and errors are all mapped.
func parseCodexResponsesSSE(body io.Reader, callback *KiroStreamCallback, model string) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var fullContent strings.Builder
	var inputTokens, outputTokens int

	// Accumulate streamed function-call arguments by call id.
	type toolAccum struct {
		callID string
		name   string
		args   strings.Builder
	}
	toolByID := map[string]*toolAccum{}
	var toolOrder []string

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			if data == "[DONE]" {
				break
			}
			continue
		}

		var ev codexResponsesData
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}

		switch ev.Type {
		case "response.output_text.delta":
			if ev.Delta != "" {
				fullContent.WriteString(ev.Delta)
				if callback.OnText != nil {
					callback.OnText(ev.Delta, false)
				}
			}
		case "response.reasoning_summary_text.delta":
			if ev.Delta != "" && callback.OnText != nil {
				callback.OnText(ev.Delta, true)
			}
		case "response.image_generation_call.partial_image":
			if ev.PartialImageB64 != "" && callback.OnImage != nil {
				callback.OnImage(ev.PartialImageB64, "image/png", true)
			}
		case "response.output_item.added", "response.output_item.done":
			if len(ev.Item) > 0 {
				var item codexResponsesItem
				if err := json.Unmarshal(ev.Item, &item); err == nil {
					switch item.Type {
					case "function_call":
						id := item.CallID
						if id == "" {
							id = item.ID
						}
						acc := toolByID[id]
						if acc == nil {
							acc = &toolAccum{callID: id}
							toolByID[id] = acc
							toolOrder = append(toolOrder, id)
						}
						if item.Name != "" {
							acc.name = item.Name
						}
						if item.Arguments != "" {
							acc.args.Reset()
							acc.args.WriteString(item.Arguments)
						}
					case "image_generation_call":
						if item.Result != "" && callback.OnImage != nil {
							callback.OnImage(item.Result, "image/png", false)
						}
					}
				}
			}
		case "response.function_call_arguments.delta":
			// Arguments delta carries the call id via item_id; fall back to the
			// single in-flight tool when only one exists.
			var d struct {
				ItemID string `json:"item_id"`
				CallID string `json:"call_id"`
				Delta  string `json:"delta"`
			}
			_ = json.Unmarshal([]byte(data), &d)
			id := d.CallID
			if id == "" {
				id = d.ItemID
			}
			acc := toolByID[id]
			if acc == nil && len(toolOrder) == 1 {
				acc = toolByID[toolOrder[0]]
			}
			if acc != nil && d.Delta != "" {
				acc.args.WriteString(d.Delta)
			}
		case "response.completed", "response.done":
			if ev.Response.Usage.InputTokens > 0 || ev.Response.Usage.OutputTokens > 0 {
				inputTokens = ev.Response.Usage.InputTokens
				outputTokens = ev.Response.Usage.OutputTokens
			}
		case "error", "response.failed":
			msg := "codex: upstream stream error"
			if ev.Error != nil && ev.Error.Message != "" {
				msg = "codex: " + ev.Error.Message
			} else if ev.Response.Error != nil && ev.Response.Error.Message != "" {
				msg = "codex: " + ev.Response.Error.Message
			}
			err := fmt.Errorf("%s", msg)
			if callback.OnError != nil {
				callback.OnError(err)
			}
			return err
		}

		if ev.Usage != nil && (ev.Usage.InputTokens > 0 || ev.Usage.OutputTokens > 0) {
			inputTokens = ev.Usage.InputTokens
			outputTokens = ev.Usage.OutputTokens
		}
	}

	if err := scanner.Err(); err != nil {
		if callback.OnError != nil {
			callback.OnError(err)
		}
		return err
	}

	if callback.OnToolUse != nil {
		for _, id := range toolOrder {
			acc := toolByID[id]
			if acc == nil || acc.name == "" {
				continue
			}
			input := map[string]interface{}{}
			if acc.args.Len() > 0 {
				_ = json.Unmarshal([]byte(acc.args.String()), &input)
			}
			callID := acc.callID
			if callID == "" {
				callID = "toolu_" + uuid.New().String()
			}
			callback.OnToolUse(KiroToolUse{ToolUseID: callID, Name: acc.name, Input: input})
		}
	}

	if callback.OnComplete != nil {
		if inputTokens == 0 && outputTokens == 0 {
			inputTokens = estimateTokens(fullContent.String())
			outputTokens = inputTokens
		}
		callback.OnComplete(inputTokens, outputTokens)
	}

	_ = model
	return nil
}
