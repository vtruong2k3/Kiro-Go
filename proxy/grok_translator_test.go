package proxy

import (
	"encoding/json"
	"testing"
)

// helper: marshal to map for easy field assertions.
func toMsgs(t *testing.T, body map[string]interface{}) []map[string]interface{} {
	t.Helper()
	raw, ok := body["messages"]
	if !ok {
		t.Fatal("body has no messages")
	}
	arr, ok := raw.([]map[string]interface{})
	if !ok {
		t.Fatalf("messages is %T, want []map[string]interface{}", raw)
	}
	return arr
}

func TestClaudeToOpenAI_SystemAndText(t *testing.T) {
	req := &ClaudeRequest{
		Model:     "grok-4",
		MaxTokens: 100,
		System:    "you are helpful",
		Messages: []ClaudeMessage{
			{Role: "user", Content: "hello"},
		},
	}
	body, err := ClaudeToOpenAI(req, false)
	if err != nil {
		t.Fatalf("ClaudeToOpenAI: %v", err)
	}
	msgs := toMsgs(t, body)
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if msgs[0]["role"] != "system" || msgs[0]["content"] != "you are helpful" {
		t.Errorf("system msg = %v", msgs[0])
	}
	if msgs[1]["role"] != "user" || msgs[1]["content"] != "hello" {
		t.Errorf("user msg = %v", msgs[1])
	}
	if body["max_tokens"] != 100 {
		t.Errorf("max_tokens = %v, want 100", body["max_tokens"])
	}
}

func TestClaudeToOpenAI_ToolUseAndResult(t *testing.T) {
	// assistant turn with a tool_use block, then a user turn with a tool_result.
	req := &ClaudeRequest{
		Model: "grok-4",
		Messages: []ClaudeMessage{
			{Role: "user", Content: "what's the weather?"},
			{Role: "assistant", Content: []interface{}{
				map[string]interface{}{
					"type":  "tool_use",
					"id":    "toolu_1",
					"name":  "get_weather",
					"input": map[string]interface{}{"city": "Hanoi"},
				},
			}},
			{Role: "user", Content: []interface{}{
				map[string]interface{}{
					"type":        "tool_result",
					"tool_use_id": "toolu_1",
					"content":     "sunny, 30C",
				},
			}},
		},
	}
	body, err := ClaudeToOpenAI(req, false)
	if err != nil {
		t.Fatalf("ClaudeToOpenAI: %v", err)
	}
	msgs := toMsgs(t, body)
	// user, assistant(tool_calls), tool
	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3: %+v", len(msgs), msgs)
	}

	assistant := msgs[1]
	if assistant["role"] != "assistant" {
		t.Fatalf("msgs[1] role = %v, want assistant", assistant["role"])
	}
	tcs, ok := assistant["tool_calls"].([]map[string]interface{})
	if !ok || len(tcs) != 1 {
		t.Fatalf("tool_calls = %v", assistant["tool_calls"])
	}
	if tcs[0]["id"] != "toolu_1" {
		t.Errorf("tool_call id = %v", tcs[0]["id"])
	}
	fn := tcs[0]["function"].(map[string]interface{})
	if fn["name"] != "get_weather" {
		t.Errorf("fn name = %v", fn["name"])
	}
	// arguments must be a JSON string
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(fn["arguments"].(string)), &args); err != nil {
		t.Fatalf("arguments not valid JSON: %v", err)
	}
	if args["city"] != "Hanoi" {
		t.Errorf("args city = %v", args["city"])
	}

	tool := msgs[2]
	if tool["role"] != "tool" {
		t.Errorf("msgs[2] role = %v, want tool", tool["role"])
	}
	if tool["tool_call_id"] != "toolu_1" {
		t.Errorf("tool_call_id = %v", tool["tool_call_id"])
	}
	if tool["content"] != "sunny, 30C" {
		t.Errorf("tool content = %v", tool["content"])
	}
}

func TestClaudeToOpenAI_ImageBlock(t *testing.T) {
	req := &ClaudeRequest{
		Model: "grok-4",
		Messages: []ClaudeMessage{
			{Role: "user", Content: []interface{}{
				map[string]interface{}{"type": "text", "text": "describe this"},
				map[string]interface{}{
					"type": "image",
					"source": map[string]interface{}{
						"media_type": "image/png",
						"data":       "AAAA",
					},
				},
			}},
		},
	}
	body, err := ClaudeToOpenAI(req, false)
	if err != nil {
		t.Fatalf("ClaudeToOpenAI: %v", err)
	}
	msgs := toMsgs(t, body)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	parts, ok := msgs[0]["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("content is %T, want multipart array", msgs[0]["content"])
	}
	if len(parts) != 2 {
		t.Fatalf("got %d parts, want 2", len(parts))
	}
	if parts[0]["type"] != "text" {
		t.Errorf("part0 type = %v", parts[0]["type"])
	}
	if parts[1]["type"] != "image_url" {
		t.Errorf("part1 type = %v", parts[1]["type"])
	}
	iu := parts[1]["image_url"].(map[string]string)
	if iu["url"] != "data:image/png;base64,AAAA" {
		t.Errorf("image url = %v", iu["url"])
	}
}

func TestResolveGrokModel(t *testing.T) {
	if got := resolveGrokModel(""); got != "grok-4" {
		t.Errorf("empty -> %q, want grok-4", got)
	}
	if got := resolveGrokModel("grok-3"); got != "grok-3" {
		t.Errorf("grok-3 -> %q", got)
	}
}
