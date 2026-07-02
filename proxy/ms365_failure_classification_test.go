package proxy

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"kiro-go/config"
	accountpool "kiro-go/pool"

	"pgregory.net/rapid"
)

// Feature: microsoft-365-sso, Property 6
//
// Property 6: Refresh-failure classification drives the disable/retain lifecycle.
//
// Over handleAccountFailure with generated error strings from the auth-marker and
// transient categories, credential-rejection (auth) errors must DISABLE the MS365
// account, while transient errors must leave it ENABLED with its stored credentials
// (AccessToken/RefreshToken) unchanged. MS365 (AuthMethod="social",
// Provider="MicrosoftEntra") must behave identically to any other account type.

// ms365FailureAuthMarkers enumerates the exact substrings recognized by
// isAuthErrorMessage in account_failover.go. Each is a pure credential-rejection
// marker that does NOT also trip the overage/quota/suspension/profile classifiers,
// so classification is unambiguous.
var ms365FailureAuthMarkers = []string{
	"http 401",
	"http 403",
	"unauthorized",
	"forbidden",
	"authentication failed",
	"token invalid",
	"token expired",
	"invalid_grant",
	"access token expired",
	"refresh token expired",
}

// ms365FailureBenignTransient is a curated set of transient/network error strings
// that are NOT matched by any classifier (auth, quota, overage, suspension, profile).
var ms365FailureBenignTransient = []string{
	"connection reset by peer",
	"i/o timeout",
	"EOF",
	"context deadline exceeded",
	"dial tcp: connection refused",
	"TLS handshake timeout",
	"temporary network failure",
	"unexpected end of stream",
	"read: broken pipe",
	"server closed idle connection",
}

// ms365FailureIsTransient reports whether msg trips none of the failure classifiers,
// i.e. it should be treated as a soft/transient failure that keeps the account enabled.
func ms365FailureIsTransient(msg string) bool {
	return !isAuthErrorMessage(msg) &&
		!isQuotaErrorMessage(msg) &&
		!isOverageErrorMessage(msg) &&
		!isSuspensionErrorMessage(msg) &&
		!isProfileUnavailableErrorMessage(msg)
}

// ms365FailureFindAccount returns a copy of the stored account with the given id.
func ms365FailureFindAccount(t *rapid.T, id string) config.Account {
	for _, a := range config.GetAccounts() {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("account %q not found in stored config", id)
	return config.Account{}
}

func TestMs365FailureClassificationProperty(t *testing.T) {
	baseDir := t.TempDir()
	counter := 0

	// Generator for credential-rejection error strings drawn from the exact auth markers.
	authErrGen := rapid.SampledFrom(ms365FailureAuthMarkers)

	// Generator for transient error strings: either a curated benign string or a random
	// lowercase/space string, filtered so it never trips any classifier.
	transientErrGen := rapid.OneOf(
		rapid.SampledFrom(ms365FailureBenignTransient),
		rapid.StringMatching(`[a-z ]{1,24}`).Filter(ms365FailureIsTransient),
	)

	rapid.Check(t, func(rt *rapid.T) {
		counter++

		// Reset global config + pool state so other tests are unaffected: each
		// iteration gets its own fresh config file.
		cfgFile := filepath.Join(baseDir, fmt.Sprintf("cfg-%d.json", counter))
		if err := config.Init(cfgFile); err != nil {
			rt.Fatalf("config.Init: %v", err)
		}

		const (
			ms365ID  = "ms365-acc"
			otherID  = "idc-acc"
			accessTk = "ms365-access-token"
			refreshTk = "ms365-refresh-token"
		)

		// MS365 account: social + MicrosoftEntra, enabled, with known credentials.
		if err := config.AddAccount(config.Account{
			ID:           ms365ID,
			Email:        "ms365@example.com",
			AccessToken:  accessTk,
			RefreshToken: refreshTk,
			AuthMethod:   "social",
			Provider:     "MicrosoftEntra",
			Enabled:      true,
		}); err != nil {
			rt.Fatalf("add ms365 account: %v", err)
		}
		// Baseline account of a different type to assert identical behavior.
		if err := config.AddAccount(config.Account{
			ID:           otherID,
			Email:        "idc@example.com",
			AccessToken:  "idc-access-token",
			RefreshToken: "idc-refresh-token",
			AuthMethod:   "idc",
			Provider:     "BuilderId",
			Enabled:      true,
		}); err != nil {
			rt.Fatalf("add baseline account: %v", err)
		}

		p := accountpool.GetPool()
		p.Reload()
		h := &Handler{pool: p}

		// Pick the error category for this iteration.
		isAuth := rapid.Bool().Draw(rt, "isAuthError")
		var errMsg string
		if isAuth {
			errMsg = authErrGen.Draw(rt, "authError")
		} else {
			errMsg = transientErrGen.Draw(rt, "transientError")
		}
		failure := errors.New(errMsg)

		ms365Before := ms365FailureFindAccount(rt, ms365ID)

		// Apply the same failure to both accounts.
		h.handleAccountFailure(&ms365Before, failure)
		otherBefore := ms365FailureFindAccount(rt, otherID)
		h.handleAccountFailure(&otherBefore, failure)

		ms365After := ms365FailureFindAccount(rt, ms365ID)
		otherAfter := ms365FailureFindAccount(rt, otherID)

		if isAuth {
			// Credential-rejection: MS365 account must be disabled.
			if ms365After.Enabled {
				rt.Fatalf("auth error %q should disable MS365 account, but it stayed enabled", errMsg)
			}
			// Identical to other account types.
			if otherAfter.Enabled {
				rt.Fatalf("auth error %q should disable baseline account too", errMsg)
			}
		} else {
			// Transient: MS365 account stays enabled with credentials unchanged.
			if !ms365After.Enabled {
				rt.Fatalf("transient error %q should leave MS365 account enabled, but it was disabled", errMsg)
			}
			if ms365After.AccessToken != accessTk {
				rt.Fatalf("transient error %q must not change AccessToken: got %q want %q", errMsg, ms365After.AccessToken, accessTk)
			}
			if ms365After.RefreshToken != refreshTk {
				rt.Fatalf("transient error %q must not change RefreshToken: got %q want %q", errMsg, ms365After.RefreshToken, refreshTk)
			}
			// Identical to other account types.
			if !otherAfter.Enabled {
				rt.Fatalf("transient error %q should leave baseline account enabled too", errMsg)
			}
		}

		// In every case, MS365 and baseline accounts share the same enabled outcome.
		if ms365After.Enabled != otherAfter.Enabled {
			rt.Fatalf("MS365 (%v) and baseline (%v) disagree on enabled state for error %q",
				ms365After.Enabled, otherAfter.Enabled, errMsg)
		}
	})
}
