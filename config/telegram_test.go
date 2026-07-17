package config

import (
	"path/filepath"
	"testing"
)

func TestGetTelegramConfigDefault(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "config.json")); err != nil {
		t.Fatalf("Init: %v", err)
	}
	got := GetTelegramConfig()
	if got.Enabled || got.BotToken != "" || got.ChatID != "" {
		t.Fatalf("expected empty default, got %#v", got)
	}
}

func TestUpdateTelegramConfigValidation(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "config.json")); err != nil {
		t.Fatalf("Init: %v", err)
	}
	token := "123:ABC"
	chat := "-1001"
	if err := UpdateTelegramConfig(true, nil, &chat); err == nil {
		t.Fatal("expected error when enabling without token")
	}
	if err := UpdateTelegramConfig(true, &token, nil); err == nil {
		t.Fatal("expected error when enabling without chat id")
	}
	empty := ""
	if err := UpdateTelegramConfig(true, &token, &empty); err == nil {
		t.Fatal("expected error when enabling with empty chat id")
	}
	if err := UpdateTelegramConfig(true, &token, &chat); err != nil {
		t.Fatalf("UpdateTelegramConfig: %v", err)
	}
	got := GetTelegramConfig()
	if !got.Enabled || got.BotToken != token || got.ChatID != chat {
		t.Fatalf("saved config = %#v", got)
	}
}

func TestUpdateTelegramConfigPreserveToken(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "config.json")); err != nil {
		t.Fatalf("Init: %v", err)
	}
	token := "123:SECRET"
	chat := "42"
	if err := UpdateTelegramConfig(true, &token, &chat); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Omit botToken (nil) → keep existing.
	newChat := "99"
	if err := UpdateTelegramConfig(true, nil, &newChat); err != nil {
		t.Fatalf("preserve update: %v", err)
	}
	got := GetTelegramConfig()
	if got.BotToken != token {
		t.Fatalf("token not preserved: %q", got.BotToken)
	}
	if got.ChatID != "99" {
		t.Fatalf("chat not updated: %q", got.ChatID)
	}
	// Explicit empty clears token; must disable first or validation fails.
	empty := ""
	if err := UpdateTelegramConfig(false, &empty, &newChat); err != nil {
		t.Fatalf("clear token: %v", err)
	}
	got = GetTelegramConfig()
	if got.BotToken != "" {
		t.Fatalf("token not cleared: %q", got.BotToken)
	}
}
