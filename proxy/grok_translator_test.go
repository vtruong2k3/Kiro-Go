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

// TestClaudeToOpenAI_SanitizesNullSchemaFields covers the xAI 400:
//
//	Schema validation failed: (root): null is not of types "boolean", "object"
//
// which clients trigger by emitting additionalProperties:null (and similar
// null schema fields) inside tool input_schema.
func TestClaudeToOpenAI_SanitizesNullSchemaFields(t *testing.T) {
	req := &ClaudeRequest{
		Model: "grok-4.5",
		Messages: []ClaudeMessage{
			{Role: "user", Content: "use tools"},
		},
		Tools: []ClaudeTool{
			{
				Name:        "read_file",
				Description: "Read a file",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":                 "string",
							"description":          "file path",
							"additionalProperties": nil, // ← the bad value
						},
						"options": map[string]interface{}{
							"type":                 []interface{}{"object", "null"},
							"additionalProperties": nil,
							"properties": map[string]interface{}{
								"offset": map[string]interface{}{
									"type":    []interface{}{"integer", "null"},
									"minimum": nil,
								},
							},
							"required": []interface{}{"offset", nil, ""},
						},
					},
					"additionalProperties": nil,
					"required":             []interface{}{"path"},
					"items":                nil,
				},
			},
			{
				Name:        "noop",
				Description: "no params",
				InputSchema: nil,
			},
		},
	}

	body, err := ClaudeToOpenAI(req, false)
	if err != nil {
		t.Fatalf("ClaudeToOpenAI: %v", err)
	}

	tools, ok := body["tools"].([]map[string]interface{})
	if !ok || len(tools) != 2 {
		t.Fatalf("tools = %#v", body["tools"])
	}

	// Walk the marshaled JSON and assert no null values remain anywhere under tools.
	raw, err := json.Marshal(tools)
	if err != nil {
		t.Fatalf("marshal tools: %v", err)
	}
	var decoded interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal tools: %v", err)
	}
	if n := countNulls(decoded); n != 0 {
		t.Fatalf("expected 0 nulls in sanitized tools, found %d\n%s", n, string(raw))
	}

	fn0 := tools[0]["function"].(map[string]interface{})
	params0 := fn0["parameters"].(map[string]interface{})
	if _, still := params0["additionalProperties"]; still {
		t.Fatalf("root additionalProperties should be dropped when null, got %#v", params0["additionalProperties"])
	}
	if _, still := params0["items"]; still {
		t.Fatalf("null items should be dropped, got %#v", params0["items"])
	}

	props := params0["properties"].(map[string]interface{})
	pathSchema := props["path"].(map[string]interface{})
	if _, still := pathSchema["additionalProperties"]; still {
		t.Fatalf("nested additionalProperties:null should be dropped, got %#v", pathSchema)
	}

	options := props["options"].(map[string]interface{})
	// type: ["object","null"] → "object"
	if options["type"] != "object" {
		t.Fatalf("options.type = %#v, want \"object\"", options["type"])
	}
	reqList, _ := options["required"].([]interface{})
	if len(reqList) != 1 || reqList[0] != "offset" {
		t.Fatalf("options.required = %#v, want [offset]", reqList)
	}

	// nil InputSchema → empty object schema
	fn1 := tools[1]["function"].(map[string]interface{})
	params1 := fn1["parameters"].(map[string]interface{})
	if params1["type"] != "object" {
		t.Fatalf("noop parameters type = %#v", params1["type"])
	}
	if _, ok := params1["properties"].(map[string]interface{}); !ok {
		t.Fatalf("noop parameters missing properties: %#v", params1)
	}
}

func TestOpenAIToOpenAI_SanitizesNullSchemaFields(t *testing.T) {
	req := &OpenAIRequest{
		Model: "grok-4.5",
		Messages: []OpenAIMessage{
			{Role: "user", Content: "hi"},
		},
		Tools: []OpenAITool{
			{
				Type: "function",
			},
		},
	}
	// Set nested function fields via the UnmarshalJSON-compatible path: build
	// the tool through JSON so the embedded struct is populated cleanly.
	rawTool := []byte(`{
		"type":"function",
		"function":{
			"name":"exec_command",
			"description":"Run a shell command",
			"parameters":{
				"type":"object",
				"properties":{
					"cmd":{"type":"string","additionalProperties":null}
				},
				"additionalProperties":null
			}
		}
	}`)
	if err := json.Unmarshal(rawTool, &req.Tools[0]); err != nil {
		t.Fatalf("unmarshal tool: %v", err)
	}

	body, err := OpenAIToOpenAI(req)
	if err != nil {
		t.Fatalf("OpenAIToOpenAI: %v", err)
	}

	raw, err := json.Marshal(body["tools"])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if n := countNulls(decoded); n != 0 {
		t.Fatalf("expected 0 nulls in sanitized OpenAI tools, found %d\n%s", n, string(raw))
	}
}

func TestSanitizeGrokToolParameters_PreservesBoolAdditionalProperties(t *testing.T) {
	in := map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"x": map[string]interface{}{"type": "string"},
		},
	}
	out, ok := sanitizeGrokToolParameters(in).(map[string]interface{})
	if !ok {
		t.Fatalf("got %T", sanitizeGrokToolParameters(in))
	}
	if out["additionalProperties"] != false {
		t.Fatalf("additionalProperties = %#v, want false", out["additionalProperties"])
	}
	// Caller schema must not be mutated.
	if in["additionalProperties"] != false {
		t.Fatalf("sanitizer mutated input")
	}
}

func countNulls(v interface{}) int {
	switch val := v.(type) {
	case nil:
		return 1
	case map[string]interface{}:
		n := 0
		for _, child := range val {
			n += countNulls(child)
		}
		return n
	case []interface{}:
		n := 0
		for _, child := range val {
			n += countNulls(child)
		}
		return n
	default:
		return 0
	}
}
