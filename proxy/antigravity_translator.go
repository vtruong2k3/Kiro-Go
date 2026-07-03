package proxy

// antigravity_translator.go converts client (Claude / OpenAI) requests into the
// Antigravity (Google Cloud Code / Gemini) wire format, and holds the anti-ban
// "tool cloaking" logic. It is the Go port of 9router's antigravity executor +
// gemini schema cleaner (open-sse/executors/antigravity.js,
// open-sse/translator/formats/gemini.js).
//
// The Gemini generateContent request is wrapped in an Antigravity envelope:
//
//	{project, model, userAgent, requestType, requestId, request:{contents,
//	 systemInstruction, tools, toolConfig, generationConfig, sessionId}}

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const (
	// maxAntigravityOutputTokens caps generationConfig.maxOutputTokens; Google
	// rejects larger values for the Cloud Code endpoint.
	maxAntigravityOutputTokens = 16384

	// agToolSuffix is appended to client tool names so they don't collide with
	// Antigravity's native tool names (anti-ban cloaking).
	agToolSuffix = "_ide"

	// defaultThinkingAGSignature is backfilled onto functionCall parts that arrive
	// without a thoughtSignature. Gemini 3+ rejects functionCall parts that lack
	// one, and clients don't persist it across turns.
	defaultThinkingAGSignature = "context_engineering_is_the_way_to_go"

	// antigravityDefaultSystem is the system-prompt spoof used when the client
	// sends no system prompt (mimics the real Antigravity IDE).
	antigravityDefaultSystem = "You are Antigravity, a powerful agentic AI coding assistant designed by the Google Deepmind team working on Advanced Agentic Coding.You are pair programming with a USER to solve their coding task. The task may require creating a new codebase, modifying or debugging an existing codebase, or simply answering a question.**Absolute paths only****Proactiveness**"
)

// ==================== Gemini wire types ====================

// GeminiEnvelope is the top-level Antigravity request body.
type GeminiEnvelope struct {
	Project     string        `json:"project"`
	Model       string        `json:"model"`
	UserAgent   string        `json:"userAgent"`
	RequestType string        `json:"requestType"`
	RequestID   string        `json:"requestId"`
	Request     *GeminiInner  `json:"request"`
}

// GeminiInner is the actual Gemini generateContent request.
type GeminiInner struct {
	Contents          []GeminiContent    `json:"contents"`
	SystemInstruction *GeminiContent     `json:"systemInstruction,omitempty"`
	Tools             []GeminiToolGroup  `json:"tools,omitempty"`
	ToolConfig        *GeminiToolConfig  `json:"toolConfig,omitempty"`
	GenerationConfig  *GeminiGenConfig   `json:"generationConfig,omitempty"`
	SessionID         string             `json:"sessionId,omitempty"`
}

type GeminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []GeminiPart `json:"parts"`
}

type GeminiPart struct {
	Text             string              `json:"text,omitempty"`
	Thought          bool                `json:"thought,omitempty"`
	ThoughtSignature string              `json:"thoughtSignature,omitempty"`
	FunctionCall     *GeminiFunctionCall `json:"functionCall,omitempty"`
	FunctionResponse *GeminiFunctionResp `json:"functionResponse,omitempty"`
	InlineData       *GeminiInlineData   `json:"inlineData,omitempty"`
}

type GeminiFunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args,omitempty"`
}

type GeminiFunctionResp struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response,omitempty"`
}

type GeminiInlineData struct {
	MimeType string `json:"mime_type"`
	Data     string `json:"data"`
}

type GeminiToolGroup struct {
	FunctionDeclarations []GeminiFunctionDecl `json:"functionDeclarations"`
}

type GeminiFunctionDecl struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters,omitempty"`
}

type GeminiToolConfig struct {
	FunctionCallingConfig struct {
		Mode string `json:"mode"`
	} `json:"functionCallingConfig"`
}

type GeminiGenConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
}

// ==================== Model list ====================

// antigravityModels is the catalog Antigravity accounts serve (ids + display
// names from 9router's antigravity registry).
var antigravityModels = []struct {
	ID   string
	Name string
}{
	{"gemini-3-flash-agent", "Gemini 3.5 Flash (High)"},
	{"gemini-3.5-flash-low", "Gemini 3.5 Flash (Medium)"},
	{"gemini-3.5-flash-extra-low", "Gemini 3.5 Flash (Low)"},
	{"gemini-pro-agent", "Gemini 3.1 Pro (High)"},
	{"gemini-3.1-pro-low", "Gemini 3.1 Pro (Low)"},
	{"claude-sonnet-4-6", "Claude Sonnet 4.6 (Thinking)"},
	{"claude-opus-4-6-thinking", "Claude Opus 4.6 (Thinking)"},
	{"gpt-oss-120b-medium", "GPT-OSS 120B (Medium)"},
	{"gemini-3-flash", "Gemini 3 Flash"},
	{"gemini-3.1-flash-image", "Gemini 3.1 Flash (Image)"},
}

// antigravityModelIDs returns the model ids Antigravity accounts serve. Registered
// with the pool so gemini-* requests route to Antigravity accounts.
func antigravityModelIDs() []string {
	ids := make([]string, len(antigravityModels))
	for i, m := range antigravityModels {
		ids[i] = m.ID
	}
	return ids
}

// antigravityModelInfos returns the AG catalog as ModelInfo for the aggregated
// /v1/models listing.
func antigravityModelInfos() []ModelInfo {
	infos := make([]ModelInfo, len(antigravityModels))
	for i, m := range antigravityModels {
		infos[i] = ModelInfo{ModelId: m.ID, ModelName: m.Name, Description: "Antigravity (Google Cloud Code)"}
	}
	return infos
}

// ==================== Function name sanitizer ====================

var agInvalidFuncNameChars = regexp.MustCompile(`[^a-zA-Z0-9_.:\-]`)
var agFuncNameLeading = regexp.MustCompile(`^[a-zA-Z_]`)

// sanitizeFunctionName enforces Gemini's function-name grammar
// [a-zA-Z_][a-zA-Z0-9_.:\-]{0,63}.
func sanitizeFunctionName(name string) string {
	if name == "" {
		return "_unknown"
	}
	s := agInvalidFuncNameChars.ReplaceAllString(name, "_")
	if !agFuncNameLeading.MatchString(s) {
		s = "_" + s
	}
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

// ==================== Schema cleaner ====================

var agUnsupportedSchemaKeys = map[string]bool{
	"minLength": true, "maxLength": true, "exclusiveMinimum": true, "exclusiveMaximum": true,
	"minItems": true, "maxItems": true, "format": true,
	"default": true, "examples": true,
	"$schema": true, "$defs": true, "definitions": true, "const": true, "$ref": true, "$comment": true,
	"deprecated": true, "readOnly": true, "writeOnly": true,
	"additionalProperties": true, "propertyNames": true, "patternProperties": true, "enumDescriptions": true,
	"anyOf": true, "oneOf": true, "allOf": true, "not": true,
	"dependencies": true, "dependentSchemas": true, "dependentRequired": true,
	"title": true, "optional": true, "if": true, "then": true, "else": true,
	"contentMediaType": true, "contentEncoding": true,
	"cornerRadius": true, "fillColor": true, "fontFamily": true, "fontSize": true, "fontWeight": true,
	"gap": true, "padding": true, "strokeColor": true, "strokeThickness": true, "textColor": true,
}

// cleanGeminiSchema sanitizes a JSON Schema for Antigravity/Gemini. It mutates and
// returns a normalized copy. Port of cleanJSONSchemaForAntigravity.
func cleanGeminiSchema(schema interface{}) interface{} {
	m, ok := toStringMap(schema)
	if !ok {
		return schema
	}
	agConvertConstToEnum(m)
	agConvertEnumValuesToStrings(m)
	agMergeAllOf(m)
	agFlattenAnyOfOneOf(m)
	agFlattenTypeArrays(m)
	agEnsureObjectType(m)
	agRemoveUnsupportedKeywords(m)
	agCleanupRequired(m)
	agAddPlaceholders(m)
	return m
}

// toStringMap coerces an arbitrary decoded-JSON value into a map[string]interface{}
// when possible (handles both map[string]interface{} from encoding/json).
func toStringMap(v interface{}) (map[string]interface{}, bool) {
	m, ok := v.(map[string]interface{})
	return m, ok
}

func agWalkChildren(m map[string]interface{}, fn func(map[string]interface{})) {
	for _, v := range m {
		switch child := v.(type) {
		case map[string]interface{}:
			fn(child)
		case []interface{}:
			for _, item := range child {
				if cm, ok := item.(map[string]interface{}); ok {
					fn(cm)
				}
			}
		}
	}
}

func agConvertConstToEnum(m map[string]interface{}) {
	if c, ok := m["const"]; ok {
		if _, hasEnum := m["enum"]; !hasEnum {
			m["enum"] = []interface{}{c}
			delete(m, "const")
		}
	}
	agWalkChildren(m, agConvertConstToEnum)
}

func agConvertEnumValuesToStrings(m map[string]interface{}) {
	if e, ok := m["enum"].([]interface{}); ok {
		strs := make([]interface{}, len(e))
		for i, v := range e {
			strs[i] = fmt.Sprintf("%v", v)
		}
		m["enum"] = strs
		if _, hasType := m["type"]; !hasType {
			m["type"] = "string"
		}
	}
	agWalkChildren(m, agConvertEnumValuesToStrings)
}

func agMergeAllOf(m map[string]interface{}) {
	if allOf, ok := m["allOf"].([]interface{}); ok {
		mergedProps := map[string]interface{}{}
		var mergedRequired []interface{}
		for _, item := range allOf {
			im, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if props, ok := im["properties"].(map[string]interface{}); ok {
				for k, v := range props {
					mergedProps[k] = v
				}
			}
			if req, ok := im["required"].([]interface{}); ok {
				mergedRequired = append(mergedRequired, req...)
			}
		}
		delete(m, "allOf")
		if len(mergedProps) > 0 {
			existing, _ := m["properties"].(map[string]interface{})
			if existing == nil {
				existing = map[string]interface{}{}
			}
			for k, v := range mergedProps {
				existing[k] = v
			}
			m["properties"] = existing
		}
		if len(mergedRequired) > 0 {
			existing, _ := m["required"].([]interface{})
			m["required"] = append(existing, mergedRequired...)
		}
	}
	agWalkChildren(m, agMergeAllOf)
}

func agSelectBest(items []interface{}) map[string]interface{} {
	bestScore := -1
	var best map[string]interface{}
	for _, it := range items {
		im, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		score := 0
		t, _ := im["type"].(string)
		_, hasProps := im["properties"]
		_, hasItems := im["items"]
		if t == "object" || hasProps {
			score = 3
		} else if t == "array" || hasItems {
			score = 2
		} else if t != "" && t != "null" {
			score = 1
		}
		if score > bestScore {
			bestScore = score
			best = im
		}
	}
	return best
}

func agFlattenAnyOfOneOf(m map[string]interface{}) {
	for _, key := range []string{"anyOf", "oneOf"} {
		if arr, ok := m[key].([]interface{}); ok && len(arr) > 0 {
			var nonNull []interface{}
			for _, s := range arr {
				sm, ok := s.(map[string]interface{})
				if !ok {
					continue
				}
				if t, _ := sm["type"].(string); t != "null" {
					nonNull = append(nonNull, s)
				}
			}
			if len(nonNull) > 0 {
				selected := agSelectBest(nonNull)
				delete(m, key)
				for k, v := range selected {
					m[k] = v
				}
			}
		}
	}
	agWalkChildren(m, agFlattenAnyOfOneOf)
}

func agFlattenTypeArrays(m map[string]interface{}) {
	if t, ok := m["type"].([]interface{}); ok {
		var nonNull []interface{}
		for _, v := range t {
			if s, _ := v.(string); s != "null" {
				nonNull = append(nonNull, v)
			}
		}
		if len(nonNull) > 0 {
			m["type"] = nonNull[0]
		} else {
			m["type"] = "string"
		}
	}
	agWalkChildren(m, agFlattenTypeArrays)
}

func agEnsureObjectType(m map[string]interface{}) {
	if _, hasProps := m["properties"]; hasProps {
		if _, hasType := m["type"]; !hasType {
			m["type"] = "object"
		}
	}
	agWalkChildren(m, agEnsureObjectType)
}

func agRemoveUnsupportedKeywords(m map[string]interface{}) {
	for k := range m {
		if agUnsupportedSchemaKeys[k] || strings.HasPrefix(k, "x-") {
			delete(m, k)
		}
	}
	agWalkChildren(m, agRemoveUnsupportedKeywords)
}

func agCleanupRequired(m map[string]interface{}) {
	if req, ok := m["required"].([]interface{}); ok {
		if props, ok := m["properties"].(map[string]interface{}); ok {
			var valid []interface{}
			for _, r := range req {
				if name, ok := r.(string); ok {
					if _, exists := props[name]; exists {
						valid = append(valid, r)
					}
				}
			}
			if len(valid) == 0 {
				delete(m, "required")
			} else {
				m["required"] = valid
			}
		}
	}
	agWalkChildren(m, agCleanupRequired)
}

func agAddPlaceholders(m map[string]interface{}) {
	if t, _ := m["type"].(string); t == "object" {
		props, _ := m["properties"].(map[string]interface{})
		if len(props) == 0 {
			m["properties"] = map[string]interface{}{
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "Brief explanation of why you are calling this tool",
				},
			}
			m["required"] = []interface{}{"reason"}
		}
	}
	agWalkChildren(m, agAddPlaceholders)
}

// emptyObjectSchema is the schema used for tools that declare no parameters
// (Antigravity requires a non-empty object schema).
func emptyObjectSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"reason": map[string]interface{}{
				"type":        "string",
				"description": "Brief explanation",
			},
		},
		"required": []interface{}{"reason"},
	}
}

// ==================== Tool cloaking ====================

// agDefaultTools are Antigravity's own native tool names; client tools matching
// these are kept unrenamed, and they're injected as decoys.
var agDefaultTools = map[string]bool{
	"browser_subagent": true, "command_status": true, "find_by_name": true,
	"generate_image": true, "grep_search": true, "list_dir": true,
	"list_resources": true, "multi_replace_file_content": true, "notify_user": true,
	"read_resource": true, "read_terminal": true, "read_url_content": true,
	"replace_file_content": true, "run_command": true, "search_web": true,
	"send_command_input": true, "task_boundary": true, "view_content_chunk": true,
	"view_file": true, "write_to_file": true,
}

// agDecoyToolNames are injected as no-op declarations so the request looks like
// real Antigravity IDE traffic. Order mirrors 9router AG_DECOY_TOOLS.
var agDecoyToolNames = []string{
	"browser_subagent", "command_status", "find_by_name", "generate_image",
	"grep_search", "list_dir", "list_resources",
	"mcp_sequential-thinking_sequentialthinking", "multi_replace_file_content",
	"notify_user", "read_resource", "read_terminal", "read_url_content",
	"replace_file_content", "run_command", "search_web", "send_command_input",
	"task_boundary", "view_content_chunk", "view_file", "write_to_file",
}

// cloakGeminiTools rewrites the inner request's tools and history to disguise
// client tools as Antigravity IDE tools. It appends "_ide" to non-native client
// tool names, injects the decoy tools, renames functionCall/functionResponse
// names in history, and returns a map from suffixed -> original names so the
// response parser can restore them. Mutates inner in place.
func cloakGeminiTools(inner *GeminiInner) map[string]string {
	if inner == nil || len(inner.Tools) == 0 {
		return nil
	}

	nameMap := map[string]string{}
	var clientDecls []GeminiFunctionDecl
	for _, group := range inner.Tools {
		for _, fn := range group.FunctionDeclarations {
			if agDefaultTools[fn.Name] {
				clientDecls = append(clientDecls, fn)
				continue
			}
			suffixed := fn.Name + agToolSuffix
			nameMap[suffixed] = fn.Name
			renamed := fn
			renamed.Name = suffixed
			clientDecls = append(clientDecls, renamed)
		}
	}

	// Client tools first, then decoys, de-duplicated by name.
	seen := map[string]bool{}
	var allDecls []GeminiFunctionDecl
	for _, d := range clientDecls {
		if d.Name == "" || seen[d.Name] {
			continue
		}
		seen[d.Name] = true
		allDecls = append(allDecls, d)
	}
	for _, name := range agDecoyToolNames {
		if seen[name] {
			continue
		}
		seen[name] = true
		allDecls = append(allDecls, GeminiFunctionDecl{
			Name:        name,
			Description: "This tool is currently unavailable.",
			Parameters:  emptyObjectSchema(),
		})
	}
	inner.Tools = []GeminiToolGroup{{FunctionDeclarations: allDecls}}

	// Rename tool names in conversation history.
	for ci := range inner.Contents {
		for pi := range inner.Contents[ci].Parts {
			p := &inner.Contents[ci].Parts[pi]
			if p.FunctionCall != nil && !agDefaultTools[p.FunctionCall.Name] {
				p.FunctionCall.Name = p.FunctionCall.Name + agToolSuffix
			}
			if p.FunctionResponse != nil && !agDefaultTools[p.FunctionResponse.Name] {
				p.FunctionResponse.Name = p.FunctionResponse.Name + agToolSuffix
			}
		}
	}

	return nameMap
}

// backfillThoughtSignatures adds the default thoughtSignature to any functionCall
// part lacking one (Gemini 3+ requirement).
func backfillThoughtSignatures(inner *GeminiInner) {
	for ci := range inner.Contents {
		for pi := range inner.Contents[ci].Parts {
			p := &inner.Contents[ci].Parts[pi]
			if p.FunctionCall != nil && p.ThoughtSignature == "" {
				p.ThoughtSignature = defaultThinkingAGSignature
			}
		}
	}
}

// ==================== Claude -> Gemini ====================

// ClaudeToGemini converts a Claude Messages request into an Antigravity request
// envelope (without project/session, filled in by the caller). Returns the
// envelope and the suffixed->original tool name map from cloaking.
func ClaudeToGemini(req *ClaudeRequest, thinking bool) (*GeminiInner, error) {
	inner := &GeminiInner{}

	// System instruction.
	systemPrompt := extractSystemPrompt(req.System)
	systemPrompt = applyPromptFilters(systemPrompt)
	if thinking {
		if systemPrompt == "" {
			systemPrompt = ThinkingModePrompt
		} else {
			systemPrompt = ThinkingModePrompt + "\n\n" + systemPrompt
		}
	}
	if strings.TrimSpace(systemPrompt) == "" {
		systemPrompt = antigravityDefaultSystem
	}
	inner.SystemInstruction = &GeminiContent{
		Parts: []GeminiPart{{Text: systemPrompt}},
	}

	// Resolve tool_use_id -> tool name from assistant tool_use blocks so
	// tool_result blocks (which only carry the id) produce a functionResponse
	// whose name matches the functionCall. Gemini requires the names to match,
	// and the cloaking rename keys on the tool name.
	toolNames := claudeToolUseNames(req.Messages)

	// Messages -> contents.
	for _, msg := range req.Messages {
		content := claudeMessageToGeminiContent(msg, toolNames)
		if content != nil {
			inner.Contents = append(inner.Contents, *content)
		}
	}

	// Tools.
	inner.Tools = claudeToolsToGemini(req.Tools)
	if len(inner.Tools) > 0 {
		tc := &GeminiToolConfig{}
		tc.FunctionCallingConfig.Mode = "VALIDATED"
		inner.ToolConfig = tc
	}

	// Generation config.
	inner.GenerationConfig = buildGeminiGenConfig(req.MaxTokens, req.Temperature, req.TopP)

	return inner, nil
}

// claudeToolUseNames scans all messages and maps each tool_use id to its tool
// name, so tool_result blocks (which reference only the id) can be given the
// matching functionResponse name.
func claudeToolUseNames(messages []ClaudeMessage) map[string]string {
	names := map[string]string{}
	for _, msg := range messages {
		blocks, ok := msg.Content.([]interface{})
		if !ok {
			continue
		}
		for _, raw := range blocks {
			block, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			if t, _ := block["type"].(string); t == "tool_use" {
				id, _ := block["id"].(string)
				name, _ := block["name"].(string)
				if id != "" && name != "" {
					names[id] = name
				}
			}
		}
	}
	return names
}

// claudeMessageToGeminiContent converts one Claude message. Roles map user->user,
// assistant->model; tool_result blocks become functionResponse parts under role
// "user".
func claudeMessageToGeminiContent(msg ClaudeMessage, toolNames map[string]string) *GeminiContent {
	role := "user"
	if msg.Role == "assistant" {
		role = "model"
	}

	var parts []GeminiPart
	hasFunctionResponse := false

	switch c := msg.Content.(type) {
	case string:
		if c != "" {
			parts = append(parts, GeminiPart{Text: c})
		}
	case []interface{}:
		for _, raw := range c {
			block, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			p, isFuncResp := claudeBlockToGeminiPart(block, toolNames)
			if p != nil {
				parts = append(parts, *p)
				if isFuncResp {
					hasFunctionResponse = true
				}
			}
		}
	}

	if len(parts) == 0 {
		return nil
	}
	// functionResponse parts must be role "user".
	if hasFunctionResponse {
		role = "user"
	}
	return &GeminiContent{Role: role, Parts: parts}
}

// claudeBlockToGeminiPart converts a single Claude content block (decoded as a
// generic map) into a Gemini part. Returns (part, isFunctionResponse).
func claudeBlockToGeminiPart(block map[string]interface{}, toolNames map[string]string) (*GeminiPart, bool) {
	blockType, _ := block["type"].(string)
	switch blockType {
	case "text":
		text, _ := block["text"].(string)
		if text == "" {
			return nil, false
		}
		return &GeminiPart{Text: text}, false
	case "thinking":
		text, _ := block["thinking"].(string)
		if text == "" {
			return nil, false
		}
		return &GeminiPart{Text: text, Thought: true}, false
	case "tool_use":
		name, _ := block["name"].(string)
		args, _ := block["input"].(map[string]interface{})
		return &GeminiPart{
			FunctionCall: &GeminiFunctionCall{Name: name, Args: args},
		}, false
	case "tool_result":
		id, _ := block["tool_use_id"].(string)
		name := toolNames[id]
		if name == "" {
			name = id
		}
		return &GeminiPart{
			FunctionResponse: &GeminiFunctionResp{
				Name:     name,
				Response: map[string]interface{}{"result": claudeToolResultText(block["content"])},
			},
		}, true
	case "image":
		if src, ok := block["source"].(map[string]interface{}); ok {
			mime, _ := src["media_type"].(string)
			data, _ := src["data"].(string)
			if data != "" {
				return &GeminiPart{InlineData: &GeminiInlineData{MimeType: mime, Data: data}}, false
			}
		}
	}
	return nil, false
}

// claudeToolResultText flattens a Claude tool_result content field (string or
// array of blocks) into a plain string.
func claudeToolResultText(content interface{}) string {
	switch c := content.(type) {
	case string:
		return c
	case []interface{}:
		var sb strings.Builder
		for _, raw := range c {
			if block, ok := raw.(map[string]interface{}); ok {
				if t, _ := block["type"].(string); t == "text" {
					if txt, _ := block["text"].(string); txt != "" {
						sb.WriteString(txt)
					}
				}
			}
		}
		return sb.String()
	}
	return ""
}

// claudeToolsToGemini builds the Gemini functionDeclarations group from Claude
// tools, sanitizing names and cleaning schemas.
func claudeToolsToGemini(tools []ClaudeTool) []GeminiToolGroup {
	if len(tools) == 0 {
		return nil
	}
	var decls []GeminiFunctionDecl
	seen := map[string]bool{}
	for _, t := range tools {
		name := sanitizeFunctionName(t.Name)
		if seen[name] {
			continue
		}
		seen[name] = true
		decls = append(decls, GeminiFunctionDecl{
			Name:        name,
			Description: t.Description,
			Parameters:  cleanToolParameters(t.InputSchema),
		})
	}
	if len(decls) == 0 {
		return nil
	}
	return []GeminiToolGroup{{FunctionDeclarations: decls}}
}

// ==================== OpenAI -> Gemini ====================

// OpenAIToGemini converts an OpenAI Chat Completions request into an Antigravity
// request inner body.
func OpenAIToGemini(req *OpenAIRequest, thinking bool) (*GeminiInner, error) {
	inner := &GeminiInner{}

	var systemPrompt string
	var nonSystem []OpenAIMessage
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			if s := extractOpenAIMessageText(msg.Content); s != "" {
				systemPrompt += s + "\n"
			}
		} else {
			nonSystem = append(nonSystem, msg)
		}
	}
	systemPrompt = applyPromptFilters(strings.TrimSpace(systemPrompt))
	if thinking {
		if systemPrompt == "" {
			systemPrompt = ThinkingModePrompt
		} else {
			systemPrompt = ThinkingModePrompt + "\n\n" + systemPrompt
		}
	}
	if strings.TrimSpace(systemPrompt) == "" {
		systemPrompt = antigravityDefaultSystem
	}
	inner.SystemInstruction = &GeminiContent{Parts: []GeminiPart{{Text: systemPrompt}}}

	// Resolve tool_call id -> function name from assistant tool_calls so tool
	// messages (which reference only the id) produce a functionResponse whose
	// name matches the functionCall (Gemini requirement; cloaking keys on name).
	toolNames := openAIToolCallNames(req.Messages)

	for _, msg := range nonSystem {
		content := openAIMessageToGeminiContent(msg, toolNames)
		if content != nil {
			inner.Contents = append(inner.Contents, *content)
		}
	}

	inner.Tools = openAIToolsToGemini(req.Tools)
	if len(inner.Tools) > 0 {
		tc := &GeminiToolConfig{}
		tc.FunctionCallingConfig.Mode = "VALIDATED"
		inner.ToolConfig = tc
	}

	inner.GenerationConfig = buildGeminiGenConfig(req.MaxTokens, req.Temperature, req.TopP)
	return inner, nil
}

// openAIToolCallNames maps each assistant tool_call id to its function name so
// tool-role messages (which reference only the id) can be given a matching
// functionResponse name.
func openAIToolCallNames(messages []OpenAIMessage) map[string]string {
	names := map[string]string{}
	for _, msg := range messages {
		for _, tc := range msg.ToolCalls {
			if tc.ID != "" && tc.Function.Name != "" {
				names[tc.ID] = tc.Function.Name
			}
		}
	}
	return names
}

func openAIMessageToGeminiContent(msg OpenAIMessage, toolNames map[string]string) *GeminiContent {
	switch msg.Role {
	case "tool":
		// Tool result -> functionResponse under role "user".
		name := toolNames[msg.ToolCallID]
		if name == "" {
			name = msg.ToolCallID
		}
		return &GeminiContent{
			Role: "user",
			Parts: []GeminiPart{{
				FunctionResponse: &GeminiFunctionResp{
					Name:     name,
					Response: map[string]interface{}{"result": extractOpenAIMessageText(msg.Content)},
				},
			}},
		}
	case "assistant":
		var parts []GeminiPart
		if text := extractOpenAIMessageText(msg.Content); text != "" {
			parts = append(parts, GeminiPart{Text: text})
		}
		for _, tc := range msg.ToolCalls {
			var args map[string]interface{}
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			if args == nil {
				args = map[string]interface{}{}
			}
			parts = append(parts, GeminiPart{
				FunctionCall: &GeminiFunctionCall{Name: tc.Function.Name, Args: args},
			})
		}
		if len(parts) == 0 {
			return nil
		}
		return &GeminiContent{Role: "model", Parts: parts}
	default: // user
		text := extractOpenAIMessageText(msg.Content)
		if text == "" {
			return nil
		}
		return &GeminiContent{Role: "user", Parts: []GeminiPart{{Text: text}}}
	}
}

func openAIToolsToGemini(tools []OpenAITool) []GeminiToolGroup {
	if len(tools) == 0 {
		return nil
	}
	var decls []GeminiFunctionDecl
	seen := map[string]bool{}
	for _, t := range tools {
		name := sanitizeFunctionName(t.Function.Name)
		if seen[name] {
			continue
		}
		seen[name] = true
		decls = append(decls, GeminiFunctionDecl{
			Name:        name,
			Description: t.Function.Description,
			Parameters:  cleanToolParameters(t.Function.Parameters),
		})
	}
	if len(decls) == 0 {
		return nil
	}
	return []GeminiToolGroup{{FunctionDeclarations: decls}}
}

// ==================== shared helpers ====================

// cleanToolParameters normalizes a tool parameter schema for Antigravity. An
// empty/nil schema becomes the placeholder object schema.
func cleanToolParameters(schema interface{}) interface{} {
	if schema == nil {
		return emptyObjectSchema()
	}
	m, ok := toStringMap(schema)
	if !ok {
		return emptyObjectSchema()
	}
	if len(m) == 0 {
		return emptyObjectSchema()
	}
	return cleanGeminiSchema(m)
}

func buildGeminiGenConfig(maxTokens int, temperature, topP float64) *GeminiGenConfig {
	cfg := &GeminiGenConfig{}
	set := false
	if maxTokens > 0 {
		cfg.MaxOutputTokens = maxTokens
		if cfg.MaxOutputTokens > maxAntigravityOutputTokens {
			cfg.MaxOutputTokens = maxAntigravityOutputTokens
		}
		set = true
	}
	if temperature > 0 {
		t := temperature
		cfg.Temperature = &t
		set = true
	}
	if topP > 0 {
		p := topP
		cfg.TopP = &p
		set = true
	}
	if !set {
		return nil
	}
	return cfg
}
