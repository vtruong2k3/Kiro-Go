package pool

import (
	"path/filepath"
	"testing"
	"time"

	"kiro-go/config"
)

// Feature: microsoft-365-sso — integration tests for pool participation.
//
// These tests confirm that an MS365 account (AuthMethod="social",
// Provider="MicrosoftEntra", Enabled=true) is an ordinary pool citizen: it is
// selectable via GetNext on the same terms as any other account type (Req 5.1,
// 8.2), and it is excluded from selection after being disabled and the pool is
// reloaded (Req 5.3). The pool selection logic is account-type agnostic, so
// these tests exercise the shared path rather than any MS365-specific code.

// ms365IntegrationAccount builds an enabled MS365 account with a far-future
// token expiry so GetNext's just-in-time-refresh skip never excludes it.
func ms365IntegrationAccount(id string) config.Account {
	return config.Account{
		ID:           id,
		Email:        id + "@example.com",
		AccessToken:  "ms365-access-token",
		RefreshToken: "ms365-refresh-token",
		AuthMethod:   "social",
		Provider:     "MicrosoftEntra",
		ExpiresAt:    time.Now().Unix() + 3600,
		Enabled:      true,
	}
}

// Req 5.1, 8.2: an enabled MS365 account is eligible for GetNext selection.
func TestMs365IntegrationEnabledAccountSelectable(t *testing.T) {
	cfgFile := filepath.Join(t.TempDir(), "config.json")
	if err := config.Init(cfgFile); err != nil {
		t.Fatalf("config.Init: %v", err)
	}
	if err := config.AddAccount(ms365IntegrationAccount("ms365")); err != nil {
		t.Fatalf("AddAccount: %v", err)
	}

	p := newTestPool()
	p.Reload()

	got := p.GetNext()
	if got == nil {
		t.Fatal("expected the enabled MS365 account to be selectable via GetNext, got nil")
	}
	if got.ID != "ms365" {
		t.Fatalf("expected account %q, got %q", "ms365", got.ID)
	}
	// Confirm the selected account is genuinely the social/MicrosoftEntra account,
	// so selection is not silently reclassifying it.
	if got.AuthMethod != "social" || got.Provider != "MicrosoftEntra" {
		t.Fatalf("expected social/MicrosoftEntra account, got AuthMethod=%q Provider=%q", got.AuthMethod, got.Provider)
	}
}

// Req 5.3: disabling an MS365 account and reloading excludes it from selection,
// while other enabled accounts remain selectable.
func TestMs365IntegrationExcludedAfterDisableReload(t *testing.T) {
	cfgFile := filepath.Join(t.TempDir(), "config.json")
	if err := config.Init(cfgFile); err != nil {
		t.Fatalf("config.Init: %v", err)
	}
	if err := config.AddAccount(ms365IntegrationAccount("ms365")); err != nil {
		t.Fatalf("AddAccount(ms365): %v", err)
	}
	// A second enabled account of a different type so the pool stays non-empty
	// after the MS365 account is disabled.
	other := config.Account{
		ID:          "idc",
		Email:       "idc@example.com",
		AccessToken: "idc-access-token",
		AuthMethod:  "idc",
		Provider:    "BuilderId",
		ExpiresAt:   time.Now().Unix() + 3600,
		Enabled:     true,
	}
	if err := config.AddAccount(other); err != nil {
		t.Fatalf("AddAccount(idc): %v", err)
	}

	p := newTestPool()
	p.Reload()

	// Before disabling: the MS365 account must appear in the weighted rotation.
	if !ms365IntegrationSeen(p, "ms365", 10) {
		t.Fatal("expected MS365 account to be selectable before disable")
	}

	// Disable the MS365 account (mirrors the auto-disable path, which sets
	// Enabled=false) and reload the pool.
	if err := config.SetAccountBanStatus("ms365", "DISABLED", "integration test disable"); err != nil {
		t.Fatalf("SetAccountBanStatus: %v", err)
	}
	p.Reload()

	// After reload: the MS365 account must never be selected, and the remaining
	// enabled account must still be served.
	for i := 0; i < 20; i++ {
		got := p.GetNext()
		if got == nil {
			t.Fatal("expected the remaining enabled account to be selectable, got nil")
		}
		if got.ID == "ms365" {
			t.Fatalf("disabled MS365 account was selected on iteration %d", i)
		}
	}
}

// Req 5.3 (single-account edge): disabling the only account empties the pool.
func TestMs365IntegrationDisabledSoleAccountYieldsEmptyPool(t *testing.T) {
	cfgFile := filepath.Join(t.TempDir(), "config.json")
	if err := config.Init(cfgFile); err != nil {
		t.Fatalf("config.Init: %v", err)
	}
	if err := config.AddAccount(ms365IntegrationAccount("ms365")); err != nil {
		t.Fatalf("AddAccount: %v", err)
	}

	p := newTestPool()
	p.Reload()
	if got := p.GetNext(); got == nil || got.ID != "ms365" {
		t.Fatalf("expected MS365 account selectable before disable, got %#v", got)
	}

	if err := config.SetAccountBanStatus("ms365", "DISABLED", "integration test disable"); err != nil {
		t.Fatalf("SetAccountBanStatus: %v", err)
	}
	p.Reload()

	if got := p.GetNext(); got != nil {
		t.Fatalf("expected nil after disabling the sole MS365 account, got %q", got.ID)
	}
}

// ms365IntegrationSeen reports whether GetNext returns the target account id
// within the given number of attempts.
func ms365IntegrationSeen(p *AccountPool, id string, attempts int) bool {
	for i := 0; i < attempts; i++ {
		if acc := p.GetNext(); acc != nil && acc.ID == id {
			return true
		}
	}
	return false
}
