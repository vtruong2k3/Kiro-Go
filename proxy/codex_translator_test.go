package proxy

import (
	"testing"
)

func TestResolveCodexModel_ReviewAndEffortSuffix(t *testing.T) {
	cases := []struct {
		in           string
		wantUpstream string
		wantEffort   string
	}{
		{"gpt-5.5", "gpt-5.5", ""},
		{"gpt-5.3-codex-high", "gpt-5.3-codex", "high"},
		{"gpt-5.3-codex-xhigh", "gpt-5.3-codex", "xhigh"},
		{"gpt-5.3-codex-none", "gpt-5.3-codex", "none"},
		// A "-review" twin maps to its base upstream model first.
		{"gpt-5.5-review", "gpt-5.5", ""},
		{"gpt-5.3-codex-high-review", "gpt-5.3-codex", "high"},
		{"", "gpt-5.5", ""},
		// GPT 5.6 ships as sol/terra/luna upstream.
		{"gpt-5.6-sol", "gpt-5.6-sol", ""},
		{"gpt-5.6-terra", "gpt-5.6-terra", ""},
		{"gpt-5.6-luna", "gpt-5.6-luna", ""},
		{"gpt-5.6-sol-review", "gpt-5.6-sol", ""},
		{"gpt-5.6-terra-review", "gpt-5.6-terra", ""},
		{"gpt-5.6-luna-review", "gpt-5.6-luna", ""},
		{"gpt-5.6-sol-high", "gpt-5.6-sol", "high"},
		{"gpt-5.6-sol-xhigh", "gpt-5.6-sol", "xhigh"},
		{"gpt-5.6-sol-minimal", "gpt-5.6-sol", "minimal"},
		{"gpt-5.6-sol-max", "gpt-5.6-sol", "max"}, // wire-normalized later to xhigh
		// Legacy bare gpt-5.6 aliases to sol (including effort variants).
		{"gpt-5.6", "gpt-5.6-sol", ""},
		{"gpt-5.6-review", "gpt-5.6-sol", ""},
		{"gpt-5.6-high", "gpt-5.6-sol", "high"},
		{"gpt-5.6-max", "gpt-5.6-sol", "max"},
	}
	for _, c := range cases {
		gotUpstream, gotEffort := resolveCodexModel(c.in)
		if gotUpstream != c.wantUpstream || gotEffort != c.wantEffort {
			t.Errorf("resolveCodexModel(%q) = (%q,%q), want (%q,%q)",
				c.in, gotUpstream, gotEffort, c.wantUpstream, c.wantEffort)
		}
	}
}

func TestBuildCodexResponsesRequest_GPT56SolMaxEffort(t *testing.T) {
	req := &ClaudeRequest{
		Model:    "gpt-5.6-sol-max",
		Messages: []ClaudeMessage{{Role: "user", Content: "hi"}},
	}
	body, err := BuildCodexResponsesRequest(req, nil, "gpt-5.6-sol-max", "s", false)
	if err != nil {
		t.Fatalf("BuildCodexResponsesRequest: %v", err)
	}
	if body["model"] != "gpt-5.6-sol" {
		t.Errorf("model = %v, want gpt-5.6-sol", body["model"])
	}
	reasoning := body["reasoning"].(map[string]interface{})
	if reasoning["effort"] != "xhigh" {
		t.Errorf("reasoning.effort = %v, want xhigh (max→xhigh)", reasoning["effort"])
	}
}

func TestBuildCodexResponsesRequest_LegacyGPT56Alias(t *testing.T) {
	req := &ClaudeRequest{
		Model:    "gpt-5.6",
		Messages: []ClaudeMessage{{Role: "user", Content: "hi"}},
	}
	body, err := BuildCodexResponsesRequest(req, nil, "gpt-5.6", "s", false)
	if err != nil {
		t.Fatalf("BuildCodexResponsesRequest: %v", err)
	}
	if body["model"] != "gpt-5.6-sol" {
		t.Errorf("model = %v, want gpt-5.6-sol (legacy alias)", body["model"])
	}
}

func TestCodexModelIDs_IncludesGPT56Variants(t *testing.T) {
	ids := codexModelIDs()
	want := []string{
		"gpt-5.6-sol", "gpt-5.6-sol-review",
		"gpt-5.6-terra", "gpt-5.6-terra-review",
		"gpt-5.6-luna", "gpt-5.6-luna-review",
		"gpt-5.6", "gpt-5.6-review", // legacy aliases stay routable
	}
	have := make(map[string]bool, len(ids))
	for _, id := range ids {
		have[id] = true
	}
	for _, id := range want {
		if !have[id] {
			t.Errorf("codexModelIDs missing %q", id)
		}
	}
}

func TestBuildCodexResponsesRequest_ClaudeStoreAndReasoning(t *testing.T) {
	req := &ClaudeRequest{
		Model:     "gpt-5.3-codex-high",
		MaxTokens: 100,
		System:    "you are codex",
		Messages: []ClaudeMessage{
			{Role: "user", Content: "hello"},
		},
	}
	body, err := BuildCodexResponsesRequest(req, nil, "gpt-5.3-codex-high", "sess-1", false)
	if err != nil {
		t.Fatalf("BuildCodexResponsesRequest: %v", err)
	}

	if body["store"] != false {
		t.Errorf("store = %v, want false", body["store"])
	}
	if body["stream"] != true {
		t.Errorf("stream = %v, want true", body["stream"])
	}
	if body["model"] != "gpt-5.3-codex" {
		t.Errorf("model = %v, want gpt-5.3-codex (effort suffix stripped)", body["model"])
	}
	if body["instructions"] != "you are codex" {
		t.Errorf("instructions = %v, want the system prompt", body["instructions"])
	}
	if body["prompt_cache_key"] != "sess-1" {
		t.Errorf("prompt_cache_key = %v, want sess-1", body["prompt_cache_key"])
	}

	reasoning, ok := body["reasoning"].(map[string]interface{})
	if !ok {
		t.Fatalf("reasoning is %T, want map", body["reasoning"])
	}
	if reasoning["effort"] != "high" {
		t.Errorf("reasoning.effort = %v, want high", reasoning["effort"])
	}
	if reasoning["summary"] != "auto" {
		t.Errorf("reasoning.summary = %v, want auto", reasoning["summary"])
	}
	// effort != none → encrypted reasoning content is requested.
	if _, ok := body["include"]; !ok {
		t.Errorf("expected include for reasoning.encrypted_content when effort != none")
	}
}

func TestBuildCodexResponsesRequest_DefaultEffortLow(t *testing.T) {
	req := &ClaudeRequest{
		Model:    "gpt-5.5",
		Messages: []ClaudeMessage{{Role: "user", Content: "hi"}},
	}
	body, err := BuildCodexResponsesRequest(req, nil, "gpt-5.5", "s", false)
	if err != nil {
		t.Fatalf("BuildCodexResponsesRequest: %v", err)
	}
	reasoning := body["reasoning"].(map[string]interface{})
	if reasoning["effort"] != "low" {
		t.Errorf("default reasoning.effort = %v, want low", reasoning["effort"])
	}
}

func TestBuildCodexResponsesRequest_InjectsDefaultInstructions(t *testing.T) {
	req := &ClaudeRequest{
		Model:    "gpt-5.5",
		Messages: []ClaudeMessage{{Role: "user", Content: "hi"}},
	}
	body, _ := BuildCodexResponsesRequest(req, nil, "gpt-5.5", "s", false)
	instr, _ := body["instructions"].(string)
	if len(instr) < 100 {
		t.Errorf("expected default Codex instructions injected, got %d bytes", len(instr))
	}
}

func TestBuildCodexResponsesRequest_SystemToDeveloperForOpenAI(t *testing.T) {
	req := &OpenAIRequest{
		Model: "gpt-5.5",
		Messages: []OpenAIMessage{
			{Role: "system", Content: "sys prompt"},
			{Role: "user", Content: "hello"},
		},
	}
	body, err := BuildCodexResponsesRequest(nil, req, "gpt-5.5", "s", false)
	if err != nil {
		t.Fatalf("BuildCodexResponsesRequest: %v", err)
	}
	// OpenAI system messages are folded into instructions (cacheable prefix).
	if body["instructions"] != "sys prompt" {
		t.Errorf("instructions = %v, want sys prompt", body["instructions"])
	}
	input, ok := body["input"].([]map[string]interface{})
	if !ok {
		t.Fatalf("input is %T, want []map", body["input"])
	}
	if len(input) != 1 || input[0]["role"] != "user" {
		t.Errorf("expected single user input item, got %v", input)
	}
}

func TestBuildCodexResponsesRequest_FlatTools(t *testing.T) {
	req := &ClaudeRequest{
		Model:    "gpt-5.5",
		Messages: []ClaudeMessage{{Role: "user", Content: "hi"}},
		Tools: []ClaudeTool{
			{Name: "get_weather", Description: "gets weather", InputSchema: map[string]interface{}{"type": "object"}},
		},
	}
	body, _ := BuildCodexResponsesRequest(req, nil, "gpt-5.5", "s", false)
	tools, ok := body["tools"].([]map[string]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %v, want one flat tool", body["tools"])
	}
	tool := tools[0]
	if tool["type"] != "function" || tool["name"] != "get_weather" {
		t.Errorf("tool = %v, want flat {type:function, name:get_weather}", tool)
	}
	// Must NOT be nested under a "function" key (chat-completions shape).
	if _, nested := tool["function"]; nested {
		t.Errorf("tool should be flat, not nested under 'function'")
	}
}

func TestBuildCodexResponsesRequest_ClaudeSystemRoleInMessages(t *testing.T) {
	// A Claude client may put role:"system" inside the messages array (not the
	// top-level system field). Codex rejects system messages in input[], so they
	// must be rewritten to role:"developer".
	req := &ClaudeRequest{
		Model: "gpt-5.5",
		Messages: []ClaudeMessage{
			{Role: "system", Content: "you are helpful"},
			{Role: "user", Content: "hi"},
		},
	}
	body, err := BuildCodexResponsesRequest(req, nil, "gpt-5.5", "s", false)
	if err != nil {
		t.Fatalf("BuildCodexResponsesRequest: %v", err)
	}
	input := body["input"].([]map[string]interface{})
	for _, item := range input {
		if item["role"] == "system" {
			t.Errorf("input still has a system-role item; want it rewritten to developer: %v", item)
		}
	}
	if len(input) < 1 || input[0]["role"] != "developer" {
		t.Errorf("first item role = %v, want developer", input[0]["role"])
	}
}

func TestStripCodexStoredItems(t *testing.T) {
	input := []map[string]interface{}{
		{"type": "message", "role": "user", "id": "msg_123", "content": []interface{}{}},
		{"type": "item_reference", "id": "rs_abc"},
		{"type": "message", "role": "assistant", "id": "local-1"},
	}
	out := stripCodexStoredItems(input)
	if len(out) != 2 {
		t.Fatalf("got %d items, want 2 (item_reference dropped)", len(out))
	}
	// Server id stripped from the surviving message.
	if _, ok := out[0]["id"]; ok {
		t.Errorf("expected server id msg_ to be stripped, got %v", out[0]["id"])
	}
	// Non-server id retained.
	if out[1]["id"] != "local-1" {
		t.Errorf("expected local-1 id retained, got %v", out[1]["id"])
	}
}

func TestBuildCodexImageRequest_HostedTool(t *testing.T) {
	req := &CodexImageRequest{
		Model:        "gpt-5.5-image",
		Prompt:       "a cat",
		Size:         "1024x1024",
		OutputFormat: "png",
	}
	body := BuildCodexImageRequest(req)
	if body["model"] != "gpt-5.5" {
		t.Errorf("model = %v, want gpt-5.5 (-image stripped)", body["model"])
	}
	tools, ok := body["tools"].([]map[string]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %v, want one image tool", body["tools"])
	}
	if tools[0]["type"] != "image_generation" {
		t.Errorf("tool type = %v, want image_generation", tools[0]["type"])
	}
	if tools[0]["size"] != "1024x1024" {
		t.Errorf("tool size = %v, want 1024x1024", tools[0]["size"])
	}
	if body["store"] != false || body["stream"] != true {
		t.Errorf("expected store=false stream=true, got store=%v stream=%v", body["store"], body["stream"])
	}
}
