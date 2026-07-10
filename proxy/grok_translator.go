package proxy

// grok_translator.go contains model lists and request converters for the
// Grok / xAI upstream. Grok official API is OpenAI-compatible, so we primarily
// convert Claude requests into OpenAI chat.completions shape and pass OpenAI
// requests through (with light normalization).
//
// This mirrors the approach used for Antigravity but targets OpenAI wire format
// (https://api.x.ai/v1/chat/completions).
//
// References:
//   - 9router open-sse/providers/registry/xai.js
//   - 9router open-sse/providers/registry/grok-web.js (models only)

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ==================== Static model catalog ====================

type grokModel struct {
	ID   string
	Name string
}

// grokModels lists the models available via the xAI API (https://api.x.ai).
// Both auth modes — official API key and Grok Build OAuth — hit the same
// OpenAI-compatible endpoint, so they share one catalog. Source: 9router
// registry/xai.js.
var grokModels = []grokModel{
	{"grok-4.5", "Grok 4.5"},
	{"grok-4-thinking", "Grok 4 Thinking"},
	{"grok-4", "Grok 4"},
	{"grok-4-fast-reasoning", "Grok 4 Fast Reasoning"},
	{"grok-code-fast-1", "Grok Code Fast"},
	{"grok-3", "Grok 3"},
	{"grok-3-mini", "Grok 3 Mini"},
	// Image-capable model (xAI supports image generation via separate endpoint,
	// but some chat models accept image input).
	{"grok-2-image-1212", "Grok 2 Image"},
}

// grokModelIDs returns the model ids for pool routing.
func grokModelIDs() []string {
	ids := make([]string, len(grokModels))
	for i, m := range grokModels {
		ids[i] = m.ID
	}
	return ids
}

// grokModelInfos returns ModelInfo entries for /v1/models aggregation.
func grokModelInfos() []ModelInfo {
	infos := make([]ModelInfo, len(grokModels))
	for i, m := range grokModels {
		infos[i] = ModelInfo{
			ModelId:     m.ID,
			ModelName:   m.Name,
			Description: "xAI Grok",
		}
	}
	return infos
}

// resolveGrokModel returns the upstream model id to send to xAI.
// For now we do light normalization (strip common suffixes if needed).
func resolveGrokModel(model string) string {
	m := strings.TrimSpace(model)
	if m == "" {
		return "grok-4"
	}
	// Accept both "grok-4-thinking" style and plain ids.
	return m
}

// ==================== Request conversion: Claude → OpenAI (for Grok) ====================

// ClaudeToOpenAI converts a ClaudeRequest into a map ready for
// POST /v1/chat/completions against Grok/xAI.
//
// This is intentionally simpler than full Kiro translation because xAI is
// OpenAI-compatible.
func ClaudeToOpenAI(req *ClaudeRequest, thinking bool) (map[string]interface{}, error) {
	if req == nil {
		return nil, fmt.Errorf("claude request is nil")
	}

	body := map[string]interface{}{
		"model": resolveGrokModel(req.Model),
	}

	// Messages
	msgs := make([]map[string]interface{}, 0, len(req.Messages)+1)

	// System prompt → system message (or first system if present)
	systemPrompt := extractClaudeSystemPrompt(req.System)
	if systemPrompt != "" {
		msgs = append(msgs, map[string]interface{}{
			"role":    "system",
			"content": systemPrompt,
		})
	}

	for _, m := range req.Messages {
		msgs = append(msgs, claudeMessageToOpenAI(m)...)
	}
	body["messages"] = msgs

	// Generation params
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if req.TopP > 0 {
		body["top_p"] = req.TopP
	}

	// Tools
	if len(req.Tools) > 0 {
		body["tools"] = claudeToolsToOpenAITools(req.Tools)
		if req.ToolChoice != nil {
			body["tool_choice"] = req.ToolChoice
		}
	}

	// Stream flag is handled by caller
	return body, nil
}

func extractClaudeSystemPrompt(system interface{}) string {
	if system == nil {
		return ""
	}
	if s, ok := system.(string); ok {
		return strings.TrimSpace(s)
	}
	if arr, ok := system.([]interface{}); ok {
		var parts []string
		for _, item := range arr {
			if m, ok := item.(map[string]interface{}); ok {
				if txt, ok := m["text"].(string); ok && txt != "" {
					parts = append(parts, txt)
				}
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	}
	return ""
}

// claudeMessageToOpenAI converts one Claude message into one or more OpenAI
// chat messages. It expands the two Claude patterns that don't map 1:1 to
// OpenAI:
//   - an assistant turn containing tool_use blocks becomes an assistant
//     message with a `tool_calls` array;
//   - a user turn containing tool_result blocks becomes one `role:"tool"`
//     message per result (OpenAI requires a separate tool message keyed by
//     tool_call_id), plus a normal user message for any accompanying text.
//
// Plain text/image content passes through as before.
func claudeMessageToOpenAI(m ClaudeMessage) []map[string]interface{} {
	role := m.Role
	if role == "" {
		role = "user"
	}

	// String content: emit a single message verbatim.
	if s, ok := m.Content.(string); ok {
		if strings.TrimSpace(s) == "" {
			return nil
		}
		return []map[string]interface{}{{"role": role, "content": s}}
	}

	blocks, ok := m.Content.([]interface{})
	if !ok {
		// Unknown shape: best-effort passthrough.
		if m.Content == nil {
			return nil
		}
		return []map[string]interface{}{{"role": role, "content": m.Content}}
	}

	var out []map[string]interface{}
	var texts []string
	var toolCalls []map[string]interface{}
	var imageParts []map[string]interface{}

	for _, b := range blocks {
		mm, ok := b.(map[string]interface{})
		if !ok {
			continue
		}
		switch typ, _ := mm["type"].(string); typ {
		case "text":
			if t, ok := mm["text"].(string); ok && t != "" {
				texts = append(texts, t)
			}
		case "image":
			if src, ok := mm["source"].(map[string]interface{}); ok {
				media, _ := src["media_type"].(string)
				data, _ := src["data"].(string)
				if data != "" {
					url := fmt.Sprintf("data:%s;base64,%s", media, data)
					imageParts = append(imageParts, map[string]interface{}{
						"type":      "image_url",
						"image_url": map[string]string{"url": url},
					})
				}
			}
		case "tool_use":
			id, _ := mm["id"].(string)
			name, _ := mm["name"].(string)
			args := "{}"
			if raw, err := json.Marshal(mm["input"]); err == nil {
				args = string(raw)
			}
			toolCalls = append(toolCalls, map[string]interface{}{
				"id":   id,
				"type": "function",
				"function": map[string]interface{}{
					"name":      name,
					"arguments": args,
				},
			})
		case "tool_result":
			id, _ := mm["tool_use_id"].(string)
			out = append(out, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": id,
				"content":      claudeToolResultContent(mm["content"]),
			})
		}
	}

	// Assemble the main message for this turn (text + images + tool_calls).
	main := map[string]interface{}{"role": role}
	hasMain := false

	if len(imageParts) > 0 {
		parts := make([]map[string]interface{}, 0, len(imageParts)+1)
		if len(texts) > 0 {
			parts = append(parts, map[string]interface{}{"type": "text", "text": strings.Join(texts, "\n")})
		}
		parts = append(parts, imageParts...)
		main["content"] = parts
		hasMain = true
	} else if len(texts) > 0 {
		main["content"] = strings.Join(texts, "\n")
		hasMain = true
	}

	if len(toolCalls) > 0 {
		main["tool_calls"] = toolCalls
		if !hasMain {
			main["content"] = nil // OpenAI allows null content when tool_calls present
		}
		hasMain = true
	}

	// A tool_result-only user turn produces just the tool messages; the empty
	// user shell would be rejected by OpenAI, so skip it.
	if hasMain {
		// Prepend the assistant/user message before any tool messages so order
		// stays: assistant(tool_calls) → tool results, or user text → (rare).
		out = append([]map[string]interface{}{main}, out...)
	}
	return out
}

// claudeToolResultContent flattens a Claude tool_result content field (which
// may be a string or an array of blocks) into a plain string for OpenAI.
func claudeToolResultContent(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, b := range v {
			if m, ok := b.(map[string]interface{}); ok {
				if t, ok := m["text"].(string); ok && t != "" {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	case nil:
		return ""
	default:
		if raw, err := json.Marshal(v); err == nil {
			return string(raw)
		}
		return ""
	}
}

func claudeToolsToOpenAITools(tools []ClaudeTool) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		if strings.TrimSpace(t.Name) == "" {
			continue
		}
		fn := map[string]interface{}{
			"name":       t.Name,
			"parameters": sanitizeGrokToolParameters(t.InputSchema),
		}
		if desc := strings.TrimSpace(t.Description); desc != "" {
			fn["description"] = desc
		}
		out = append(out, map[string]interface{}{
			"type":     "function",
			"function": fn,
		})
	}
	return out
}

// ==================== OpenAI passthrough (light normalization) ====================

// OpenAIToOpenAI prepares an OpenAIRequest for the Grok/xAI endpoint.
// For pure OpenAI clients we can mostly forward the body as-is.
func OpenAIToOpenAI(req *OpenAIRequest) (map[string]interface{}, error) {
	if req == nil {
		return nil, fmt.Errorf("openai request is nil")
	}

	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	var out map[string]interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}

	// Ensure model is resolved for grok
	if m, ok := out["model"].(string); ok {
		out["model"] = resolveGrokModel(m)
	} else {
		out["model"] = "grok-4"
	}

	// xAI rejects tool schemas that contain nulls where a boolean/object is
	// required (most commonly additionalProperties:null from client SDKs).
	sanitizeGrokOpenAITools(out)

	// xAI is strict about some fields in certain cases; drop empty ones
	cleanEmptyOpenAIFields(out)
	return out, nil
}

// sanitizeGrokOpenAITools rewrites tools[*].function.parameters so the body is
// acceptable to xAI's JSON Schema validator.
func sanitizeGrokOpenAITools(body map[string]interface{}) {
	rawTools, ok := body["tools"]
	if !ok || rawTools == nil {
		return
	}
	tools, ok := rawTools.([]interface{})
	if !ok {
		return
	}
	cleaned := make([]interface{}, 0, len(tools))
	for _, raw := range tools {
		tool, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		// Nested Chat Completions shape: {type, function:{name,parameters,...}}
		if fn, ok := tool["function"].(map[string]interface{}); ok {
			fn["parameters"] = sanitizeGrokToolParameters(fn["parameters"])
			if name, _ := fn["name"].(string); strings.TrimSpace(name) == "" {
				continue
			}
			cleaned = append(cleaned, tool)
			continue
		}
		// Flat Responses shape: {type, name, parameters, ...}
		if name, _ := tool["name"].(string); strings.TrimSpace(name) != "" {
			params := sanitizeGrokToolParameters(tool["parameters"])
			entry := map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":       name,
					"parameters": params,
				},
			}
			if desc, _ := tool["description"].(string); strings.TrimSpace(desc) != "" {
				entry["function"].(map[string]interface{})["description"] = desc
			}
			cleaned = append(cleaned, entry)
			continue
		}
	}
	if len(cleaned) == 0 {
		delete(body, "tools")
		return
	}
	body["tools"] = cleaned
}

// emptyGrokObjectSchema is the fallback parameters object when a tool declares
// no schema (or a non-object schema). xAI requires a valid object schema.
func emptyGrokObjectSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

// sanitizeGrokToolParameters normalizes a tool parameter schema for xAI.
// The common failure mode is:
//
//	Schema validation failed: (root): null is not of types "boolean", "object"
//
// which happens when clients emit e.g. `"additionalProperties": null`. xAI's
// validator is stricter than OpenAI's and rejects null for any schema field
// that is typed as boolean|object (or similar unions).
func sanitizeGrokToolParameters(schema interface{}) interface{} {
	if schema == nil {
		return emptyGrokObjectSchema()
	}
	m, ok := schema.(map[string]interface{})
	if !ok {
		// Non-object schemas aren't valid tool parameters; fall back.
		return emptyGrokObjectSchema()
	}
	if len(m) == 0 {
		return emptyGrokObjectSchema()
	}
	cleaned := sanitizeGrokSchemaValue(cloneSchemaMap(m))
	out, ok := cleaned.(map[string]interface{})
	if !ok || len(out) == 0 {
		return emptyGrokObjectSchema()
	}
	// Tool parameters must be an object schema.
	if _, hasType := out["type"]; !hasType {
		if _, hasProps := out["properties"]; hasProps {
			out["type"] = "object"
		} else if _, hasItems := out["items"]; hasItems {
			// Array root is uncommon for tools; wrap as object for safety.
			return emptyGrokObjectSchema()
		} else {
			out["type"] = "object"
		}
	}
	if t, _ := out["type"].(string); t == "object" {
		if _, hasProps := out["properties"]; !hasProps {
			out["properties"] = map[string]interface{}{}
		}
	}
	return out
}

// sanitizeGrokSchemaValue recursively strips nulls and normalizes fields that
// xAI's JSON Schema validator is known to reject.
func sanitizeGrokSchemaValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		return sanitizeGrokSchemaMap(val)
	case []interface{}:
		out := make([]interface{}, 0, len(val))
		for _, item := range val {
			if item == nil {
				continue
			}
			cleaned := sanitizeGrokSchemaValue(item)
			if cleaned != nil {
				out = append(out, cleaned)
			}
		}
		return out
	default:
		return v
	}
}

func sanitizeGrokSchemaMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}

	// Drop nulls first so downstream checks see only real values.
	for k, v := range m {
		if v == nil {
			delete(m, k)
		}
	}

	// additionalProperties must be boolean or object — never null (already
	// deleted above). Keep true/false and object schemas; drop anything else.
	if ap, exists := m["additionalProperties"]; exists {
		switch ap.(type) {
		case bool:
			// ok
		case map[string]interface{}:
			m["additionalProperties"] = sanitizeGrokSchemaValue(ap)
		default:
			// e.g. number/string leftovers from broken clients
			delete(m, "additionalProperties")
		}
	}

	// required must be an array of strings; empty/null already handled.
	if req, exists := m["required"]; exists {
		switch arr := req.(type) {
		case []interface{}:
			valid := make([]interface{}, 0, len(arr))
			for _, item := range arr {
				if s, ok := item.(string); ok && s != "" {
					valid = append(valid, s)
				}
			}
			if len(valid) == 0 {
				delete(m, "required")
			} else {
				m["required"] = valid
			}
		case []string:
			if len(arr) == 0 {
				delete(m, "required")
			}
		default:
			delete(m, "required")
		}
	}

	// type: ["string","null"] → "string" (drop nullability union). xAI is fine
	// with single string types; multi-type arrays with "null" are unnecessary
	// noise that some validators reject.
	if t, ok := m["type"].([]interface{}); ok {
		var nonNull []interface{}
		for _, item := range t {
			if s, _ := item.(string); s != "" && s != "null" {
				nonNull = append(nonNull, s)
			}
		}
		switch len(nonNull) {
		case 0:
			delete(m, "type")
		case 1:
			m["type"] = nonNull[0]
		default:
			m["type"] = nonNull
		}
	}

	// Recurse into nested schema nodes.
	for k, v := range m {
		switch k {
		case "properties", "patternProperties", "dependentSchemas", "definitions", "$defs":
			if child, ok := v.(map[string]interface{}); ok {
				cleaned := make(map[string]interface{}, len(child))
				for pk, pv := range child {
					if pv == nil {
						continue
					}
					if cm := sanitizeGrokSchemaValue(pv); cm != nil {
						cleaned[pk] = cm
					}
				}
				m[k] = cleaned
			}
		case "items", "contains", "not", "if", "then", "else", "additionalProperties", "propertyNames", "unevaluatedProperties", "unevaluatedItems":
			if v == nil {
				delete(m, k)
				continue
			}
			if child, ok := v.(map[string]interface{}); ok {
				m[k] = sanitizeGrokSchemaMap(child)
			} else if _, isBool := v.(bool); !isBool {
				// items can also be an array of schemas
				m[k] = sanitizeGrokSchemaValue(v)
			}
		case "allOf", "anyOf", "oneOf", "prefixItems":
			m[k] = sanitizeGrokSchemaValue(v)
		default:
			// Generic recursion for unknown nested objects/arrays.
			switch v.(type) {
			case map[string]interface{}, []interface{}:
				m[k] = sanitizeGrokSchemaValue(v)
			}
		}
	}

	// Drop empty required/enum arrays that may remain after filtering.
	for _, key := range []string{"required", "enum", "allOf", "anyOf", "oneOf"} {
		if arr, ok := m[key].([]interface{}); ok && len(arr) == 0 {
			delete(m, key)
		}
	}

	return m
}

func cleanEmptyOpenAIFields(m map[string]interface{}) {
	for k, v := range m {
		switch val := v.(type) {
		case string:
			if val == "" {
				delete(m, k)
			}
		case []interface{}:
			if len(val) == 0 {
				delete(m, k)
			}
		case map[string]interface{}:
			cleanEmptyOpenAIFields(val)
			if len(val) == 0 {
				delete(m, k)
			}
		}
	}
}

// ==================== Response helpers (used by grok_api.go) ====================

// openAIStreamChunk represents a single SSE chunk from OpenAI-compatible endpoint.
type openAIStreamChunk struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role    string `json:"role,omitempty"`
			Content string `json:"content,omitempty"`
			// Many providers (incl. Grok) use reasoning_content for thinking.
			ReasoningContent string                `json:"reasoning_content,omitempty"`
			ToolCalls        []streamToolCallDelta `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *OpenAIUsage `json:"usage,omitempty"`
}

// streamToolCallDelta is a partial tool call as streamed in OpenAI SSE deltas.
// The first delta for a given index carries id + function.name; later deltas
// for the same index append fragments of function.arguments.
type streamToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

// openAIResponse is the non-streaming response shape.
type openAIResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int           `json:"index"`
		Message      OpenAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage OpenAIUsage `json:"usage"`
}

// extractTextFromOpenAIMessage pulls the best text + optional reasoning out of a message.
func extractTextFromOpenAIMessage(msg OpenAIMessage) (content string, reasoning string) {
	if s, ok := msg.Content.(string); ok {
		content = s
	}
	// Some responses put reasoning in a separate field (future proof)
	if r, ok := msg.Content.(map[string]interface{}); ok {
		if c, ok := r["content"].(string); ok {
			content = c
		}
		if rc, ok := r["reasoning_content"].(string); ok {
			reasoning = rc
		}
	}
	return
}
