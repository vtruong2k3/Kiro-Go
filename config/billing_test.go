package config

import (
	"path/filepath"
	"testing"
)

func TestGetTokenUsageMultiplierDefault(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "config.json")); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if got := GetTokenUsageMultiplier(); got != 1.0 {
		t.Fatalf("default mult = %v", got)
	}
	if err := UpdateBillingConfig(nil, 1.25); err != nil {
		t.Fatalf("UpdateBillingConfig: %v", err)
	}
	if got := GetTokenUsageMultiplier(); got != 1.25 {
		t.Fatalf("custom mult = %v", got)
	}
}

func TestGetModelCreditRatesCopy(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "config.json")); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := UpdateBillingConfig(map[string]float64{"default": 0.01}, 1); err != nil {
		t.Fatalf("UpdateBillingConfig: %v", err)
	}
	got := GetModelCreditRates()
	got["default"] = 9
	if GetModelCreditRates()["default"] != 0.01 {
		t.Fatalf("GetModelCreditRates must return a copy")
	}
}

func TestUpdateBillingConfigValidation(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "config.json")); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := UpdateBillingConfig(nil, 0); err == nil {
		t.Fatal("expected error for mult <= 0")
	}
	if err := UpdateBillingConfig(map[string]float64{"a": -1}, 1); err == nil {
		t.Fatal("expected error for negative rate")
	}
	if err := UpdateBillingConfig(map[string]float64{" default ": 0.02, "": 1}, 1.2); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if GetTokenUsageMultiplier() != 1.2 {
		t.Fatalf("mult not saved")
	}
	rates := GetModelCreditRates()
	if rates["default"] != 0.02 || len(rates) != 1 {
		t.Fatalf("rates = %#v", rates)
	}
	if GetModelCreditRate("unknown-model") != 0.02 {
		t.Fatalf("default key should apply, got %v", GetModelCreditRate("unknown-model"))
	}
}
