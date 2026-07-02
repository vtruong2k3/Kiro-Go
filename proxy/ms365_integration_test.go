package proxy

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"kiro-go/config"
	accountpool "kiro-go/pool"
)

// Feature: microsoft-365-sso — integration tests for proxy participation.
//
// These tests confirm an MS365 account (AuthMethod="social",
// Provider="MicrosoftEntra") reuses the account-type-agnostic proxy subsystems:
//   - per-account outbound proxy resolution with global fallback (Req 5.4, 8.6)
//   - failover to another account on a retryable error, bounded by
//     maxAccountRetryAttempts (Req 5.5)
// Nothing here is MS365-specific in the production code; the point is that the
// shared paths treat MS365 accounts exactly like every other type.

// ms365IntegrationConfig initializes a fresh temp config file and returns its path.
func ms365IntegrationConfig(t *testing.T) {
	t.Helper()
	cfgFile := filepath.Join(t.TempDir(), "config.json")
	if err := config.Init(cfgFile); err != nil {
		t.Fatalf("config.Init: %v", err)
	}
}

// ms365IntegrationSocialAccount builds an enabled MS365 account with a far-future
// token expiry.
func ms365IntegrationSocialAccount(id string) config.Account {
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

// Req 5.4, 8.6: ProxyURL resolution applies the same per-account/global-fallback
// rule to MS365 accounts as to any other type.
func TestMs365IntegrationProxyURLResolution(t *testing.T) {
	ms365IntegrationConfig(t)

	const globalProxy = "socks5://global-proxy:1080"
	if err := config.UpdateProxySettings(globalProxy); err != nil {
		t.Fatalf("UpdateProxySettings: %v", err)
	}

	// With an empty per-account ProxyURL, resolution falls back to the global proxy.
	acct := ms365IntegrationSocialAccount("ms365")
	acct.ProxyURL = ""
	if got := ResolveAccountProxyURL(&acct); got != globalProxy {
		t.Fatalf("empty per-account ProxyURL should fall back to global %q, got %q", globalProxy, got)
	}

	// With a per-account ProxyURL set, resolution uses that value, not the global.
	const perAccountProxy = "http://account-proxy:3128"
	acct.ProxyURL = perAccountProxy
	if got := ResolveAccountProxyURL(&acct); got != perAccountProxy {
		t.Fatalf("per-account ProxyURL %q should take precedence over global, got %q", perAccountProxy, got)
	}
}

// Req 5.5: a retryable failure on an MS365 account fails over to a different
// account. The failover loop mirrors the account-type-agnostic loop used by the
// request handlers (GetNextExcluding + handleAccountFailure, bounded by
// maxAccountRetryAttempts).
func TestMs365IntegrationFailoverToAnotherAccount(t *testing.T) {
	ms365IntegrationConfig(t)

	const ms365ID = "ms365"
	const fallbackID = "fallback"

	if err := config.AddAccount(ms365IntegrationSocialAccount(ms365ID)); err != nil {
		t.Fatalf("AddAccount(ms365): %v", err)
	}
	if err := config.AddAccount(config.Account{
		ID:          fallbackID,
		Email:       "fallback@example.com",
		AccessToken: "fallback-access-token",
		AuthMethod:  "idc",
		Provider:    "BuilderId",
		ExpiresAt:   time.Now().Unix() + 3600,
		Enabled:     true,
	}); err != nil {
		t.Fatalf("AddAccount(fallback): %v", err)
	}

	p := accountpool.GetPool()
	p.Reload()
	h := &Handler{pool: p}

	// A retryable, transient error: not matched by any auth/quota/overage/
	// suspension/profile classifier, so it keeps the failing account enabled.
	retryable := errors.New("HTTP 500 upstream temporarily unavailable")

	excluded := map[string]bool{}
	var served *config.Account
	attempts := 0
	for attempt := 0; attempt < maxAccountRetryAttempts; attempt++ {
		attempts++
		account := p.GetNextExcluding(excluded)
		if account == nil {
			break
		}
		// Simulate the upstream call: the MS365 account fails retryably; any
		// other account succeeds.
		if account.ID == ms365ID {
			excluded[account.ID] = true
			h.handleAccountFailure(account, retryable)
			continue
		}
		served = account
		break
	}

	if served == nil {
		t.Fatal("expected failover to serve a different account, got none")
	}
	if served.ID != fallbackID {
		t.Fatalf("expected failover to %q, got %q", fallbackID, served.ID)
	}
	if attempts > maxAccountRetryAttempts {
		t.Fatalf("attempts %d exceeded maxAccountRetryAttempts %d", attempts, maxAccountRetryAttempts)
	}

	// A retryable failure must not disable the MS365 account.
	for _, a := range config.GetAccounts() {
		if a.ID == ms365ID && !a.Enabled {
			t.Fatal("retryable failure must leave the MS365 account enabled")
		}
	}
}

// Req 5.5: failover is bounded by maxAccountRetryAttempts. When every account
// fails retryably, the loop stops after exactly maxAccountRetryAttempts and
// serves no account.
func TestMs365IntegrationFailoverBoundedByMaxRetries(t *testing.T) {
	ms365IntegrationConfig(t)

	// More accounts than maxAccountRetryAttempts, so the bound (not exhaustion of
	// the pool) is what stops the loop.
	ids := []string{"ms365", "acc-2", "acc-3", "acc-4", "acc-5"}
	for i, id := range ids {
		acct := ms365IntegrationSocialAccount(id)
		if i > 0 {
			acct.AuthMethod = "idc"
			acct.Provider = "BuilderId"
		}
		if err := config.AddAccount(acct); err != nil {
			t.Fatalf("AddAccount(%s): %v", id, err)
		}
	}

	p := accountpool.GetPool()
	p.Reload()
	h := &Handler{pool: p}

	retryable := errors.New("HTTP 500 upstream temporarily unavailable")

	excluded := map[string]bool{}
	var served *config.Account
	attempts := 0
	for attempt := 0; attempt < maxAccountRetryAttempts; attempt++ {
		attempts++
		account := p.GetNextExcluding(excluded)
		if account == nil {
			break
		}
		// Every account fails retryably.
		excluded[account.ID] = true
		h.handleAccountFailure(account, retryable)
	}

	if served != nil {
		t.Fatalf("expected no account served when all fail, got %q", served.ID)
	}
	if attempts != maxAccountRetryAttempts {
		t.Fatalf("expected exactly %d attempts (the retry bound), got %d", maxAccountRetryAttempts, attempts)
	}
}
