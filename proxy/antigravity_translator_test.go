package proxy

import (
	"strings"
	"testing"
)

func TestParseGeminiSSE(t *testing.T) {
	// Antigravity wraps the Gemini response in a "response" envelope. Cover a
	// thought part, a plain text part, a functionCall, and usage metadata.
	stream := strings.Join([]string{
		`data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"thinking...","thought":true}]}}]}}`,
		`data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"hello world"}]}}]}}`,
		`data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"get_weather_ide","args":{"city":"Hanoi"}}}]}}]}}`,
		`data: {"response":{"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":42,"candidatesTokenCount":7}}}`,
		`data: [DONE]`,
		"",
	}, "\n")

	var text, thinking strings.Builder
	var toolNames []string
	var inTok, outTok int
	completed := false

	cb := &KiroStreamCallback{
		OnText: func(s string, isThinking bool) {
			if isThinking {
				thinking.WriteString(s)
			} else {
				text.WriteString(s)
			}
		},
		OnToolUse: func(tu KiroToolUse) { toolNames = append(toolNames, tu.Name) },
		OnComplete: func(in, out int) {
			inTok, outTok = in, out
			completed = true
		},
	}

	if err := parseGeminiSSE(strings.NewReader(stream), cb); err != nil {
		t.Fatalf("parseGeminiSSE returned error: %v", err)
	}
	if text.String() != "hello world" {
		t.Errorf("text = %q, want %q", text.String(), "hello world")
	}
	if thinking.String() != "thinking..." {
		t.Errorf("thinking = %q, want %q", thinking.String(), "thinking...")
	}
	if len(toolNames) != 1 || toolNames[0] != "get_weather_ide" {
		t.Errorf("toolNames = %v, want [get_weather_ide]", toolNames)
	}
	if inTok != 42 || outTok != 7 {
		t.Errorf("tokens = (%d,%d), want (42,7)", inTok, outTok)
	}
	if !completed {
		t.Error("OnComplete was not called")
	}
}

func TestSanitizeFunctionName(t *testing.T) {
	cases := map[string]string{
		"":                "_unknown",
		"validName":       "validName",
		"has space":       "has_space",
		"weird!@#chars":   "weird___chars",
		"123leading":      "_123leading",
		"dots.and:colons": "dots.and:colons",
	}
	for in, want := range cases {
		if got := sanitizeFunctionName(in); got != want {
			t.Errorf("sanitizeFunctionName(%q) = %q, want %q", in, got, want)
		}
	}
	// Truncation to 64.
	long := strings.Repeat("a", 100)
	if got := sanitizeFunctionName(long); len(got) != 64 {
		t.Errorf("expected truncation to 64, got len %d", len(got))
	}
}

func TestCleanGeminiSchemaConstToEnum(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"mode": map[string]interface{}{"const": "fast"},
		},
	}
	cleaned := cleanGeminiSchema(schema).(map[string]interface{})
	props := cleaned["properties"].(map[string]interface{})
	mode := props["mode"].(map[string]interface{})
	if _, hasConst := mode["const"]; hasConst {
		t.Error("const should have been removed")
	}
	enum, ok := mode["enum"].([]interface{})
	if !ok || len(enum) != 1 || enum[0] != "fast" {
		t.Errorf("expected enum [fast], got %v", mode["enum"])
	}
	if mode["type"] != "string" {
		t.Errorf("expected type string for enum, got %v", mode["type"])
	}
}

func TestCleanGeminiSchemaStripsUnsupported(t *testing.T) {
	schema := map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"minLength":            float64(3),
		"x-custom":             "vendor",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string", "format": "email"},
		},
	}
	cleaned := cleanGeminiSchema(schema).(map[string]interface{})
	for _, k := range []string{"additionalProperties", "minLength", "x-custom"} {
		if _, ok := cleaned[k]; ok {
			t.Errorf("key %q should have been stripped", k)
		}
	}
	name := cleaned["properties"].(map[string]interface{})["name"].(map[string]interface{})
	if _, ok := name["format"]; ok {
		t.Error("nested format should have been stripped")
	}
}

func TestCleanGeminiSchemaAnyOfFlatten(t *testing.T) {
	schema := map[string]interface{}{
		"anyOf": []interface{}{
			map[string]interface{}{"type": "null"},
			map[string]interface{}{"type": "object", "properties": map[string]interface{}{"x": map[string]interface{}{"type": "string"}}},
		},
	}
	cleaned := cleanGeminiSchema(schema).(map[string]interface{})
	if _, ok := cleaned["anyOf"]; ok {
		t.Error("anyOf should have been flattened away")
	}
	if cleaned["type"] != "object" {
		t.Errorf("expected flattened type object, got %v", cleaned["type"])
	}
}

func TestCleanGeminiSchemaEmptyObjectPlaceholder(t *testing.T) {
	schema := map[string]interface{}{"type": "object"}
	cleaned := cleanGeminiSchema(schema).(map[string]interface{})
	props, ok := cleaned["properties"].(map[string]interface{})
	if !ok || len(props) == 0 {
		t.Fatal("expected placeholder properties on empty object schema")
	}
	if _, ok := props["reason"]; !ok {
		t.Error("expected reason placeholder property")
	}
}

func TestCloakGeminiToolsSuffixAndDecoys(t *testing.T) {
	inner := &GeminiInner{
		Tools: []GeminiToolGroup{{FunctionDeclarations: []GeminiFunctionDecl{
			{Name: "my_custom_tool"},
			{Name: "run_command"}, // native AG name — kept unrenamed
		}}},
		Contents: []GeminiContent{
			{Role: "model", Parts: []GeminiPart{{FunctionCall: &GeminiFunctionCall{Name: "my_custom_tool"}}}},
			{Role: "user", Parts: []GeminiPart{{FunctionResponse: &GeminiFunctionResp{Name: "my_custom_tool"}}}},
		},
	}
	nameMap := cloakGeminiTools(inner)

	if nameMap["my_custom_tool_ide"] != "my_custom_tool" {
		t.Errorf("expected suffixed->original mapping, got %v", nameMap)
	}
	if _, mapped := nameMap["run_command_ide"]; mapped {
		t.Error("native AG tool should not be suffixed/mapped")
	}

	// History names renamed.
	fc := inner.Contents[0].Parts[0].FunctionCall
	if fc.Name != "my_custom_tool_ide" {
		t.Errorf("history functionCall not renamed, got %q", fc.Name)
	}
	fr := inner.Contents[1].Parts[0].FunctionResponse
	if fr.Name != "my_custom_tool_ide" {
		t.Errorf("history functionResponse not renamed, got %q", fr.Name)
	}

	// Decoys injected: exactly one tool group, contains native names.
	if len(inner.Tools) != 1 {
		t.Fatalf("expected single merged tool group, got %d", len(inner.Tools))
	}
	names := map[string]bool{}
	for _, d := range inner.Tools[0].FunctionDeclarations {
		names[d.Name] = true
	}
	if !names["my_custom_tool_ide"] {
		t.Error("expected cloaked client tool in declarations")
	}
	for _, decoy := range []string{"run_command", "view_file", "browser_subagent"} {
		if !names[decoy] {
			t.Errorf("expected decoy %q injected", decoy)
		}
	}
}

func TestClaudeToGeminiToolResultNameMatch(t *testing.T) {
	req := &ClaudeRequest{
		Model:    "gemini-3-flash",
		Messages: []ClaudeMessage{
			{Role: "assistant", Content: []interface{}{
				map[string]interface{}{"type": "tool_use", "id": "tu_1", "name": "search", "input": map[string]interface{}{"q": "hi"}},
			}},
			{Role: "user", Content: []interface{}{
				map[string]interface{}{"type": "tool_result", "tool_use_id": "tu_1", "content": "result text"},
			}},
		},
	}
	inner, err := ClaudeToGemini(req, false)
	if err != nil {
		t.Fatal(err)
	}
	// Find the functionResponse; its name must resolve to "search", not the id.
	var frName string
	for _, c := range inner.Contents {
		for _, p := range c.Parts {
			if p.FunctionResponse != nil {
				frName = p.FunctionResponse.Name
			}
		}
	}
	if frName != "search" {
		t.Errorf("expected functionResponse name resolved to tool name 'search', got %q", frName)
	}
}

func TestClaudeToGeminiDefaultSystem(t *testing.T) {
	req := &ClaudeRequest{Model: "gemini-3-flash", Messages: []ClaudeMessage{{Role: "user", Content: "hi"}}}
	inner, err := ClaudeToGemini(req, false)
	if err != nil {
		t.Fatal(err)
	}
	if inner.SystemInstruction == nil || len(inner.SystemInstruction.Parts) == 0 {
		t.Fatal("expected default system instruction")
	}
	if !strings.Contains(inner.SystemInstruction.Parts[0].Text, "Antigravity") {
		t.Error("expected Antigravity default system spoof when no system prompt")
	}
}

func TestBuildGeminiGenConfigCap(t *testing.T) {
	cfg := buildGeminiGenConfig(999999, 0, 0)
	if cfg == nil || cfg.MaxOutputTokens != maxAntigravityOutputTokens {
		t.Errorf("expected maxOutputTokens capped at %d, got %v", maxAntigravityOutputTokens, cfg)
	}
	if buildGeminiGenConfig(0, 0, 0) != nil {
		t.Error("expected nil gen config when nothing set")
	}
}

func TestParseModelAndThinkingGeminiPassthrough(t *testing.T) {
	for _, m := range []string{"gemini-3-flash", "gemini-pro-agent", "gemini-3.5-flash-low"} {
		got, _ := ParseModelAndThinking(m, "-thinking")
		if got != m {
			t.Errorf("ParseModelAndThinking(%q) = %q, want passthrough", m, got)
		}
	}
	// Thinking suffix still detected on gemini.
	got, thinking := ParseModelAndThinking("gemini-3-flash-thinking", "-thinking")
	if got != "gemini-3-flash" || !thinking {
		t.Errorf("expected (gemini-3-flash,true), got (%q,%v)", got, thinking)
	}
}
