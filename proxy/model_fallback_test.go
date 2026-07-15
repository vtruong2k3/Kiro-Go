package proxy

import (
	"path/filepath"
	"testing"

	"kiro-go/config"
)

func TestEstimateCreditsForModel(t *testing.T) {
	// default rate 0.003 / 1k tokens
	got := estimateCreditsForModel("claude-opus-4.8", 2000)
	// 2000/1000 * 0.003 = 0.006
	if got < 0.0059 || got > 0.0061 {
		t.Fatalf("expected ~0.006, got %v", got)
	}
	if estimateCreditsForModel("x", 0) != 0 {
		t.Fatalf("zero tokens should be 0")
	}
}

func TestResolveCreditsPrefersUpstream(t *testing.T) {
	if got := resolveCredits(1.25, "claude-opus-4.8", 2000); got != 1.25 {
		t.Fatalf("expected upstream credits, got %v", got)
	}
	if got := resolveCredits(0, "claude-opus-4.8", 1000); got <= 0 {
		t.Fatalf("expected estimated credits when upstream is 0, got %v", got)
	}
}

func TestBillUsageMultiplierScalesTokensAndEstimate(t *testing.T) {
	if err := config.Init(filepath.Join(t.TempDir(), "config.json")); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Ensure default mult=1 identity first.
	tokens, credits := billUsage(0, "claude-opus-4.8", 1000, 1000)
	if tokens != 2000 {
		t.Fatalf("identity tokens = %d", tokens)
	}
	if credits < 0.0059 || credits > 0.0061 {
		t.Fatalf("identity credits = %v", credits)
	}

	// Metering is never multiplied.
	tokens, credits = billUsage(1.5, "claude-opus-4.8", 1000, 1000)
	if tokens != 2000 {
		t.Fatalf("metering tokens = %d", tokens)
	}
	if credits != 1.5 {
		t.Fatalf("metering credits should be unchanged, got %v", credits)
	}

	if err := config.UpdateBillingConfig(map[string]float64{"default": 0.003}, 1.5); err != nil {
		t.Fatalf("UpdateBillingConfig: %v", err)
	}
	tokens, credits = billUsage(0, "claude-opus-4.8", 1000, 1000)
	// 2000 * 1.5 = 3000; credits = 3000/1000*0.003 = 0.009
	if tokens != 3000 {
		t.Fatalf("scaled tokens = %d, want 3000", tokens)
	}
	if credits < 0.0089 || credits > 0.0091 {
		t.Fatalf("scaled credits = %v, want ~0.009", credits)
	}
	// Metering still unscaled.
	_, credits = billUsage(2.0, "claude-opus-4.8", 1000, 1000)
	if credits != 2.0 {
		t.Fatalf("metering must not scale, got %v", credits)
	}
}

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
