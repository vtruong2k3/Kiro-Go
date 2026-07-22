package proxy

// codex_translator.go builds OpenAI Responses API requests for the Codex
// (ChatGPT) upstream (https://chatgpt.com/backend-api/codex/responses) from the
// preserved SourceClaude / SourceOpenAI request on a KiroPayload.
//
// Unlike Grok (which is OpenAI chat.completions-compatible), Codex speaks the
// OpenAI Responses API: input[] items, flat tools, reasoning{effort,summary},
// store:false, and an allowlist of top-level fields. This file is the reverse of
// proxy/responses_handler.go, which EMITS Responses SSE to clients.
//
// Source of truth: 9router open-sse/executors/codex.js + registry/codex.js.

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

//go:embed codex_instructions.txt
var codexDefaultInstructions string

// ==================== Static model catalog ====================

type codexModel struct {
	ID   string
	Name string
	// UpstreamModelID is the real Codex model to send when ID is a virtual
	// variant (e.g. a "-review" quota twin). Empty means ID is sent as-is
	// (after reasoning-effort suffix stripping).
	UpstreamModelID string
	// Kind is "image" for image-generation models, "" for text.
	Kind string
}

// codexModels mirrors 9router registry/codex.js: text models (each with a
// "-review" quota-family twin), reasoning-effort variants, and image models.
// GPT 5.6 ships as sol/terra/luna variants upstream — bare "gpt-5.6" is kept as
// a legacy alias that maps to gpt-5.6-sol so older clients keep working.
var codexModels = []codexModel{
	{ID: "gpt-5.6-sol", Name: "GPT 5.6 Sol"},
	{ID: "gpt-5.6-sol-review", Name: "GPT 5.6 Sol Review", UpstreamModelID: "gpt-5.6-sol"},
	{ID: "gpt-5.6-terra", Name: "GPT 5.6 Terra"},
	{ID: "gpt-5.6-terra-review", Name: "GPT 5.6 Terra Review", UpstreamModelID: "gpt-5.6-terra"},
	{ID: "gpt-5.6-luna", Name: "GPT 5.6 Luna"},
	{ID: "gpt-5.6-luna-review", Name: "GPT 5.6 Luna Review", UpstreamModelID: "gpt-5.6-luna"},
	// Legacy aliases (not real upstream ids).
	{ID: "gpt-5.6", Name: "GPT 5.6", UpstreamModelID: "gpt-5.6-sol"},
	{ID: "gpt-5.6-review", Name: "GPT 5.6 Review", UpstreamModelID: "gpt-5.6-sol"},
	{ID: "gpt-5.5", Name: "GPT 5.5", UpstreamModelID: "gpt-5.5"},
	{ID: "gpt-5.5-review", Name: "GPT 5.5 Review", UpstreamModelID: "gpt-5.5"},
	{ID: "gpt-5.4", Name: "GPT 5.4"},
	{ID: "gpt-5.4-review", Name: "GPT 5.4 Review", UpstreamModelID: "gpt-5.4"},
	{ID: "gpt-5.4-mini", Name: "GPT 5.4 Mini"},
	{ID: "gpt-5.4-mini-review", Name: "GPT 5.4 Mini Review", UpstreamModelID: "gpt-5.4-mini"},
	{ID: "gpt-5.3-codex", Name: "GPT 5.3 Codex"},
	{ID: "gpt-5.3-codex-review", Name: "GPT 5.3 Codex Review", UpstreamModelID: "gpt-5.3-codex"},
	{ID: "gpt-5.3-codex-xhigh", Name: "GPT 5.3 Codex (xHigh)"},
	{ID: "gpt-5.3-codex-xhigh-review", Name: "GPT 5.3 Codex (xHigh) Review", UpstreamModelID: "gpt-5.3-codex-xhigh"},
	{ID: "gpt-5.3-codex-high", Name: "GPT 5.3 Codex (High)"},
	{ID: "gpt-5.3-codex-high-review", Name: "GPT 5.3 Codex (High) Review", UpstreamModelID: "gpt-5.3-codex-high"},
	{ID: "gpt-5.3-codex-low", Name: "GPT 5.3 Codex (Low)"},
	{ID: "gpt-5.3-codex-low-review", Name: "GPT 5.3 Codex (Low) Review", UpstreamModelID: "gpt-5.3-codex-low"},
	{ID: "gpt-5.3-codex-none", Name: "GPT 5.3 Codex (None)"},
	{ID: "gpt-5.3-codex-none-review", Name: "GPT 5.3 Codex (None) Review", UpstreamModelID: "gpt-5.3-codex-none"},
	{ID: "gpt-5.3-codex-spark", Name: "GPT 5.3 Codex Spark"},
	{ID: "gpt-5.3-codex-spark-review", Name: "GPT 5.3 Codex Spark Review", UpstreamModelID: "gpt-5.3-codex-spark"},
	{ID: "gpt-5.5-image", Name: "GPT 5.5 Image", Kind: "image"},
	{ID: "gpt-5.4-image", Name: "GPT 5.4 Image", Kind: "image"},
	{ID: "gpt-5.3-image", Name: "GPT 5.3 Image", Kind: "image"},
}

// codexModelIDs returns the model ids for pool routing.
func codexModelIDs() []string {
	ids := make([]string, len(codexModels))
	for i, m := range codexModels {
		ids[i] = m.ID
	}
	return ids
}

// codexModelInfos returns ModelInfo entries for /v1/models aggregation.
func codexModelInfos() []ModelInfo {
	infos := make([]ModelInfo, len(codexModels))
	for i, m := range codexModels {
		info := ModelInfo{
			ModelId:     m.ID,
			ModelName:   m.Name,
			Description: "OpenAI Codex",
		}
		if m.Kind == "image" {
			info.InputTypes = []string{"text", "image"}
		} else {
			info.InputTypes = []string{"text"}
		}
		infos[i] = info
	}
	return infos
}

// isCodexImageModel reports whether a model id targets Codex image generation.
func isCodexImageModel(model string) bool {
	return strings.HasSuffix(strings.TrimSpace(model), "-image")
}

// codexReviewUpstream maps a "-review" (or any virtual) model id to its real
// upstream Codex model. Unknown ids pass through unchanged.
func codexReviewUpstream(model string) string {
	m := strings.TrimSpace(model)
	for _, cm := range codexModels {
		if cm.ID == m && cm.UpstreamModelID != "" {
			return cm.UpstreamModelID
		}
	}
	return m
}

// codexEffortSuffixes are trailing "-<effort>" tokens stripped from model ids.
// Longer tokens come first so "-xhigh" wins over "-high".
// "max" is accepted as a client alias and normalized to wire "xhigh" later
// (mirrors 9router normalizeReasoningEffort for gpt-5.6-sol).
var codexEffortSuffixes = []string{"xhigh", "minimal", "medium", "none", "high", "low", "max"}

// normalizeCodexReasoningEffort maps client effort aliases to Codex wire values.
func normalizeCodexReasoningEffort(effort string) string {
	if effort == "max" {
		return "xhigh"
	}
	return effort
}

// resolveCodexModel maps a requested model id to the real upstream model and the
// reasoning effort implied by any suffix. Order:
//  1. catalog virtual ids (e.g. "-review", legacy "gpt-5.6") → UpstreamModelID
//  2. generic trailing "-review" strip for unknown combos
//  3. trailing "-<effort>" strip
//  4. legacy bare "gpt-5.6" → "gpt-5.6-sol" (covers gpt-5.6-high etc.)
//
// When no effort suffix is present, effort is "" and the caller defaults to "low".
func resolveCodexModel(model string) (upstream string, effort string) {
	m := codexReviewUpstream(model)
	if m == "" {
		return "gpt-5.5", ""
	}
	if strings.HasSuffix(m, "-review") {
		m = strings.TrimSuffix(m, "-review")
	}
	for _, level := range codexEffortSuffixes {
		suf := "-" + level
		if strings.HasSuffix(m, suf) {
			effort = level
			m = strings.TrimSuffix(m, suf)
			break
		}
	}
	if m == "gpt-5.6" {
		m = "gpt-5.6-sol"
	}
	if m == "" {
		m = "gpt-5.5"
	}
	return m, effort
}

// codexServerIDPattern matches Codex server-generated item ids that cannot be
// resolved when store=false (they must be stripped from input).
var codexServerIDPattern = regexp.MustCompile(`^(rs|fc|resp|msg)_`)

// codexResponsesAllowlist is the set of top-level fields Codex Responses accepts.
// Anything else is stripped to avoid upstream "routing_unsupported" errors.
var codexResponsesAllowlist = map[string]bool{
	"model": true, "input": true, "instructions": true, "tools": true,
	"tool_choice": true, "stream": true, "store": true, "reasoning": true,
	"service_tier": true, "include": true, "prompt_cache_key": true,
	"client_metadata": true, "text": true,
}

// ==================== Request conversion → OpenAI Responses (for Codex) ====================

// BuildCodexResponsesRequest constructs a Responses API request body from either
// a Claude request or an OpenAI request (whichever is preserved on the payload).
// It applies all Codex-required transforms: system→developer, store:false,
// reasoning effort from the model suffix, flat tools, and the field allowlist.
func BuildCodexResponsesRequest(sourceClaude *ClaudeRequest, sourceOpenAI *OpenAIRequest, model, sessionID string, thinking bool) (map[string]interface{}, error) {
	upstream, effort := resolveCodexModel(model)

	var input []map[string]interface{}
	var instructions string
	var tools []map[string]interface{}
	var toolChoice interface{}

	switch {
	case sourceClaude != nil:
		instructions = extractClaudeSystemPrompt(sourceClaude.System)
		input = claudeMessagesToResponsesInput(sourceClaude.Messages)
		tools = claudeToolsToResponsesTools(sourceClaude.Tools)
		toolChoice = sourceClaude.ToolChoice
	case sourceOpenAI != nil:
		instructions, input = openAIMessagesToResponsesInput(sourceOpenAI.Messages)
		tools = openAIToolsToResponsesTools(sourceOpenAI.Tools)
	default:
		return nil, fmt.Errorf("codex: no source request (need SourceClaude or SourceOpenAI)")
	}

	input = stripCodexStoredItems(input)
	input = convertCodexSystemRole(input)
	if len(input) == 0 {
		input = []map[string]interface{}{{
			"type":    "message",
			"role":    "user",
			"content": []map[string]interface{}{{"type": "input_text", "text": "..."}},
		}}
	}

	if strings.TrimSpace(instructions) == "" {
		instructions = codexDefaultInstructions
	}

	if effort == "" {
		effort = "low"
	}
	effort = normalizeCodexReasoningEffort(effort)
	reasoning := map[string]interface{}{"effort": effort, "summary": "auto"}

	body := map[string]interface{}{
		"model":            upstream,
		"input":            input,
		"instructions":     instructions,
		"store":            false,
		"stream":           true,
		"reasoning":        reasoning,
		"prompt_cache_key": sessionID,
	}
	if len(tools) > 0 {
		body["tools"] = tools
		if toolChoice != nil {
			body["tool_choice"] = toolChoice
		}
	}
	if effort != "none" {
		body["include"] = []string{"reasoning.encrypted_content"}
	}

	// Final allowlist filter (defensive; body is built explicitly above).
	for k := range body {
		if !codexResponsesAllowlist[k] {
			delete(body, k)
		}
	}
	return body, nil
}

// claudeMessagesToResponsesInput converts Claude messages into Responses input[]
// items. Text/image become input_text/input_image; tool_use becomes a
// function_call item; tool_result becomes a function_call_output item.
func claudeMessagesToResponsesInput(msgs []ClaudeMessage) []map[string]interface{} {
	var out []map[string]interface{}
	for _, m := range msgs {
		role := m.Role
		if role == "" {
			role = "user"
		}

		if s, ok := m.Content.(string); ok {
			if strings.TrimSpace(s) == "" {
				continue
			}
			out = append(out, map[string]interface{}{
				"type":    "message",
				"role":    role,
				"content": []map[string]interface{}{{"type": codexTextPartType(role), "text": s}},
			})
			continue
		}

		blocks, ok := m.Content.([]interface{})
		if !ok {
			continue
		}

		var parts []map[string]interface{}
		var trailing []map[string]interface{}
		for _, b := range blocks {
			mm, ok := b.(map[string]interface{})
			if !ok {
				continue
			}
			switch typ, _ := mm["type"].(string); typ {
			case "text":
				if t, ok := mm["text"].(string); ok && t != "" {
					parts = append(parts, map[string]interface{}{"type": codexTextPartType(role), "text": t})
				}
			case "image":
				if src, ok := mm["source"].(map[string]interface{}); ok {
					media, _ := src["media_type"].(string)
					data, _ := src["data"].(string)
					if data != "" {
						parts = append(parts, map[string]interface{}{
							"type":      "input_image",
							"image_url": fmt.Sprintf("data:%s;base64,%s", media, data),
							"detail":    "auto",
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
				trailing = append(trailing, map[string]interface{}{
					"type":      "function_call",
					"call_id":   id,
					"name":      name,
					"arguments": args,
				})
			case "tool_result":
				id, _ := mm["tool_use_id"].(string)
				trailing = append(trailing, map[string]interface{}{
					"type":    "function_call_output",
					"call_id": id,
					"output":  claudeToolResultContent(mm["content"]),
				})
			}
		}
		if len(parts) > 0 {
			out = append(out, map[string]interface{}{"type": "message", "role": role, "content": parts})
		}
		out = append(out, trailing...)
	}
	return out
}

// codexTextPartType returns the Responses content part type for a role. Assistant
// turns use output_text; everything else uses input_text.
func codexTextPartType(role string) string {
	if role == "assistant" {
		return "output_text"
	}
	return "input_text"
}

// claudeToolsToResponsesTools converts Claude tools to flat Responses function
// tools: {type:"function", name, description, parameters}.
func claudeToolsToResponsesTools(tools []ClaudeTool) []map[string]interface{} {
	if len(tools) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		params := t.InputSchema
		if params == nil {
			params = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
		}
		tool := map[string]interface{}{
			"type":       "function",
			"name":       t.Name,
			"parameters": params,
		}
		if t.Description != "" {
			tool["description"] = t.Description
		}
		out = append(out, tool)
	}
	return out
}

// openAIMessagesToResponsesInput converts OpenAI chat messages into Responses
// input[] items. A leading run of system messages is folded into the returned
// instructions string (kept in the cacheable prefix as role=developer would be);
// remaining messages become message / function_call / function_call_output items.
func openAIMessagesToResponsesInput(msgs []OpenAIMessage) (instructions string, input []map[string]interface{}) {
	var sysParts []string
	for _, m := range msgs {
		role := m.Role
		switch role {
		case "system", "developer":
			if s := openAIContentToString(m.Content); s != "" {
				sysParts = append(sysParts, s)
			}
		case "tool":
			input = append(input, map[string]interface{}{
				"type":    "function_call_output",
				"call_id": m.ToolCallID,
				"output":  openAIContentToString(m.Content),
			})
		default:
			if role == "" {
				role = "user"
			}
			parts := openAIContentToResponsesParts(m.Content, role)
			if len(parts) > 0 {
				input = append(input, map[string]interface{}{"type": "message", "role": role, "content": parts})
			}
			for _, tc := range m.ToolCalls {
				args := tc.Function.Arguments
				if args == "" {
					args = "{}"
				}
				input = append(input, map[string]interface{}{
					"type":      "function_call",
					"call_id":   tc.ID,
					"name":      tc.Function.Name,
					"arguments": args,
				})
			}
		}
	}
	return strings.Join(sysParts, "\n"), input
}

// openAIContentToString flattens an OpenAI message content field (string or
// array of parts) into plain text.
func openAIContentToString(content interface{}) string {
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
	default:
		return ""
	}
}

// openAIContentToResponsesParts converts an OpenAI content field into Responses
// content parts (input_text / input_image, or output_text for assistant turns).
func openAIContentToResponsesParts(content interface{}, role string) []map[string]interface{} {
	textType := codexTextPartType(role)
	switch v := content.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []map[string]interface{}{{"type": textType, "text": v}}
	case []interface{}:
		var parts []map[string]interface{}
		for _, b := range v {
			m, ok := b.(map[string]interface{})
			if !ok {
				continue
			}
			switch typ, _ := m["type"].(string); typ {
			case "text", "input_text", "output_text":
				if t, ok := m["text"].(string); ok && t != "" {
					parts = append(parts, map[string]interface{}{"type": textType, "text": t})
				}
			case "image_url", "input_image":
				url := ""
				detail := "auto"
				if iu, ok := m["image_url"].(map[string]interface{}); ok {
					url, _ = iu["url"].(string)
					if d, ok := iu["detail"].(string); ok && d != "" {
						detail = d
					}
				} else if s, ok := m["image_url"].(string); ok {
					url = s
				}
				if url != "" {
					parts = append(parts, map[string]interface{}{"type": "input_image", "image_url": url, "detail": detail})
				}
			}
		}
		return parts
	default:
		return nil
	}
}

// openAIToolsToResponsesTools converts OpenAI tools (nested function shape) into
// flat Responses function tools.
func openAIToolsToResponsesTools(tools []OpenAITool) []map[string]interface{} {
	if len(tools) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		if t.Function.Name == "" {
			continue
		}
		params := t.Function.Parameters
		if params == nil {
			params = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
		}
		tool := map[string]interface{}{
			"type":       "function",
			"name":       t.Function.Name,
			"parameters": params,
		}
		if t.Function.Description != "" {
			tool["description"] = t.Function.Description
		}
		out = append(out, tool)
	}
	return out
}

// CodexImageRequest is the parsed OpenAI images-compat request Kiro-Go accepts
// on /v1/images/generations and /v1/images/edits.
type CodexImageRequest struct {
	Model        string   `json:"model"`
	Prompt       string   `json:"prompt"`
	N            int      `json:"n,omitempty"`
	Size         string   `json:"size,omitempty"`
	Quality      string   `json:"quality,omitempty"`
	Background   string   `json:"background,omitempty"`
	OutputFormat string   `json:"output_format,omitempty"`
	ImageDetail  string   `json:"image_detail,omitempty"`
	Image        string   `json:"image,omitempty"`  // single reference image (data URL or base64)
	Images       []string `json:"images,omitempty"` // multiple reference images
}

// BuildCodexImageRequest constructs a Responses API request that drives the
// hosted image_generation tool. Reference images (for edits) are inlined as
// input_image parts, and generation params map onto the tool config.
func BuildCodexImageRequest(req *CodexImageRequest) map[string]interface{} {
	upstream, _ := resolveCodexModel(req.Model)
	upstream = strings.TrimSuffix(upstream, "-image")

	detail := req.ImageDetail
	if detail == "" {
		detail = "high"
	}

	refs := req.Images
	if len(refs) == 0 && strings.TrimSpace(req.Image) != "" {
		refs = []string{req.Image}
	}

	var content []map[string]interface{}
	for i, ref := range refs {
		content = append(content,
			map[string]interface{}{"type": "input_text", "text": fmt.Sprintf("<image name=image%d>", i+1)},
			map[string]interface{}{"type": "input_image", "image_url": codexToImageDataURL(ref), "detail": detail},
			map[string]interface{}{"type": "input_text", "text": "</image>"},
		)
	}
	content = append(content, map[string]interface{}{"type": "input_text", "text": req.Prompt})

	outputFormat := strings.ToLower(strings.TrimSpace(req.OutputFormat))
	if outputFormat == "" {
		outputFormat = "png"
	}
	imgTool := map[string]interface{}{"type": "image_generation", "output_format": outputFormat}
	if s := strings.TrimSpace(req.Size); s != "" {
		imgTool["size"] = s
	}
	if q := strings.TrimSpace(req.Quality); q != "" {
		imgTool["quality"] = q
	}
	if bg := strings.TrimSpace(req.Background); bg != "" {
		imgTool["background"] = bg
	}

	return map[string]interface{}{
		"model":        upstream,
		"instructions": "",
		"input": []map[string]interface{}{
			{"type": "message", "role": "user", "content": content},
		},
		"tools":            []map[string]interface{}{imgTool},
		"tool_choice":      "auto",
		"store":            false,
		"stream":           true,
		"prompt_cache_key": "",
	}
}

// codexToImageDataURL wraps a bare base64 reference image as a PNG data URL. Data
// URLs and http(s) URLs pass through unchanged.
func codexToImageDataURL(ref string) string {
	r := strings.TrimSpace(ref)
	if strings.HasPrefix(r, "data:") || strings.HasPrefix(r, "http://") || strings.HasPrefix(r, "https://") {
		return r
	}
	return "data:image/png;base64," + r
}

// stripCodexStoredItems removes input items Codex cannot resolve when store=false:
// item_reference items, and the id field of any item whose id is a server-generated
// id (rs_/fc_/resp_/msg_).
func stripCodexStoredItems(input []map[string]interface{}) []map[string]interface{} {
	if len(input) == 0 {
		return input
	}
	out := make([]map[string]interface{}, 0, len(input))
	for _, item := range input {
		if typ, _ := item["type"].(string); typ == "item_reference" {
			continue
		}
		if id, ok := item["id"].(string); ok && codexServerIDPattern.MatchString(id) {
			delete(item, "id")
		}
		out = append(out, item)
	}
	return out
}

// convertCodexSystemRole rewrites any message input item with role "system" to
// role "developer". The Codex backend rejects system messages inside input[]
// ("System messages are not allowed"); developer is the accepted equivalent and
// stays in the cacheable prefix. System content that arrives via the top-level
// instructions field is untouched (it never becomes an input item).
func convertCodexSystemRole(input []map[string]interface{}) []map[string]interface{} {
	for _, item := range input {
		if typ, _ := item["type"].(string); typ != "" && typ != "message" {
			continue
		}
		if role, _ := item["role"].(string); role == "system" {
			item["role"] = "developer"
		}
	}
	return input
}
