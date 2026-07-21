package config

import "testing"

func TestGetModelFallback(t *testing.T) {
	cfgLock.Lock()
	old := cfg
	cfg = &Config{ModelFallback: map[string][]ModelFallbackTarget{
		"claude-opus-4.8": {{Model: "grok-4.5"}, {Model: "gemini-2.5-pro"}, {Model: ""}},
	}}
	cfgLock.Unlock()
	defer func() {
		cfgLock.Lock()
		cfg = old
		cfgLock.Unlock()
	}()

	got := GetModelFallback("Claude-Opus-4.8")
	if len(got) != 2 || got[0].Model != "grok-4.5" || got[1].Model != "gemini-2.5-pro" {
		t.Fatalf("unexpected fallback: %+v", got)
	}
	if GetModelFallback("nope") != nil {
		t.Fatalf("expected nil for unknown model")
	}
}
