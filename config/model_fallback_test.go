package config

import "testing"

func TestGetModelCreditRateDefault(t *testing.T) {
	// Without Load, cfg may be nil — function must not panic and return builtin default.
	cfgLock.Lock()
	old := cfg
	cfg = &Config{}
	cfgLock.Unlock()
	defer func() {
		cfgLock.Lock()
		cfg = old
		cfgLock.Unlock()
	}()

	if got := GetModelCreditRate("claude-opus-4.8"); got != 0.003 {
		t.Fatalf("default rate = %v", got)
	}

	cfgLock.Lock()
	cfg.ModelCreditRates = map[string]float64{
		"claude-opus": 0.015,
		"default":     0.002,
	}
	cfgLock.Unlock()
	if got := GetModelCreditRate("claude-opus-4.8"); got != 0.015 {
		t.Fatalf("prefix rate = %v", got)
	}
	if got := GetModelCreditRate("other-model"); got != 0.002 {
		t.Fatalf("default key rate = %v", got)
	}
}

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
