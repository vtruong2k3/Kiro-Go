package proxy

import (
	"testing"

	"kiro-go/config"
)

func TestOverridePayloadModel(t *testing.T) {
	p := &KiroPayload{}
	p.SourceClaude = &ClaudeRequest{Model: "claude-opus-4.8"}
	p.SourceOpenAI = &OpenAIRequest{Model: "claude-opus-4.8"}
	p.ConversationState.CurrentMessage.UserInputMessage.ModelID = "claude-opus-4.8"

	overridePayloadModel(p, "grok-4.5")
	if p.ConversationState.CurrentMessage.UserInputMessage.ModelID != "grok-4.5" {
		t.Fatalf("payload ModelID not overridden")
	}
	if p.SourceClaude.Model != "grok-4.5" || p.SourceOpenAI.Model != "grok-4.5" {
		t.Fatalf("source models not overridden")
	}
}

func TestProviderLabel(t *testing.T) {
	if providerLabel(nil) != "" {
		t.Fatal("nil account")
	}
	if providerLabel(&config.Account{Provider: "grok"}) != "grok" {
		t.Fatal("grok")
	}
	if providerLabel(&config.Account{Provider: "codex"}) != "codex" {
		t.Fatal("codex")
	}
	if providerLabel(&config.Account{AuthMethod: "antigravity"}) != "antigravity" {
		t.Fatal("antigravity")
	}
	if providerLabel(&config.Account{}) != "kiro" {
		t.Fatal("default kiro")
	}
}
