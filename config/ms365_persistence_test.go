package config

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"
)

// ms365PersistenceReadAccount returns the stored account with the given id, or
// (Account{}, false) when it is not present.
func ms365PersistenceReadAccount(id string) (Account, bool) {
	for _, a := range GetAccounts() {
		if a.ID == id {
			return a, true
		}
	}
	return Account{}, false
}

// TestMs365PersistenceRefreshTokenRetentionProperty verifies Property 5.
//
// Feature: microsoft-365-sso, Property 5: Refresh persistence retains the
// refresh token when none is returned — for any successful refresh result,
// persisting the update via config.UpdateAccountToken always overwrites the
// account's access token and expiry with the refreshed values, and overwrites
// the refresh token if and only if the refresh response returned a non-empty
// refresh token; when the response returns no new refresh token, the account's
// existing refresh token is retained unchanged.
//
// Validates: Requirements 4.2, 4.3, 4.4
func TestMs365PersistenceRefreshTokenRetentionProperty(t *testing.T) {
	// Use an isolated temp config path so this property test does not interfere
	// with other config tests. Save and restore the package-level globals.
	prevCfg := cfg
	prevPath := cfgPath
	defer func() {
		cfg = prevCfg
		cfgPath = prevPath
	}()

	if err := Init(filepath.Join(t.TempDir(), "config.json")); err != nil {
		t.Fatalf("init config: %v", err)
	}

	rapid.Check(t, func(rt *rapid.T) {
		// Seed a known MS365 account with a known original refresh token. Each
		// case uses a unique id and cleans up afterward so the store stays small
		// and cases do not cross-contaminate.
		id := uuid.New().String()
		originalRefresh := "orig-refresh-" + rapid.String().Draw(rt, "originalRefresh")
		seed := Account{
			ID:           id,
			AccessToken:  "orig-access-" + rapid.String().Draw(rt, "originalAccess"),
			RefreshToken: originalRefresh,
			AuthMethod:   "social",
			Provider:     "MicrosoftEntra",
			ExpiresAt:    rapid.Int64().Draw(rt, "originalExpiresAt"),
			Enabled:      true,
		}
		if err := AddAccount(seed); err != nil {
			rt.Fatalf("seed AddAccount: %v", err)
		}
		defer func() { _ = DeleteAccount(id) }()

		// Generate the refresh result triple. The access token is always
		// non-empty (a successful refresh always yields one); the refresh token
		// may be empty (no new refresh token returned) or non-empty.
		newAccessToken := "new-access-" + rapid.String().Draw(rt, "newAccessToken")
		var newRefreshToken string
		if rapid.Bool().Draw(rt, "returnsNewRefresh") {
			newRefreshToken = "new-refresh-" + rapid.String().Draw(rt, "newRefreshToken")
		}
		newExpiresAt := rapid.Int64().Draw(rt, "newExpiresAt")

		if err := UpdateAccountToken(id, newAccessToken, newRefreshToken, newExpiresAt); err != nil {
			rt.Fatalf("UpdateAccountToken: %v", err)
		}

		stored, ok := ms365PersistenceReadAccount(id)
		if !ok {
			rt.Fatalf("stored account %s not found after update", id)
		}

		// Access token is always overwritten with the refreshed value.
		if stored.AccessToken != newAccessToken {
			rt.Fatalf("access token not overwritten: got %q want %q", stored.AccessToken, newAccessToken)
		}
		// Expiry is always overwritten with the refreshed value.
		if stored.ExpiresAt != newExpiresAt {
			rt.Fatalf("expiry not overwritten: got %d want %d", stored.ExpiresAt, newExpiresAt)
		}
		// Refresh token is overwritten iff a non-empty new refresh token was
		// returned; otherwise the original is retained unchanged.
		if newRefreshToken != "" {
			if stored.RefreshToken != newRefreshToken {
				rt.Fatalf("refresh token not overwritten with new value: got %q want %q",
					stored.RefreshToken, newRefreshToken)
			}
		} else {
			if stored.RefreshToken != originalRefresh {
				rt.Fatalf("refresh token not retained: got %q want %q",
					stored.RefreshToken, originalRefresh)
			}
		}
	})
}
