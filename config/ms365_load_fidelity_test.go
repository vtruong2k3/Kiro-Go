package config

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"
)

// ms365LoadFidelityDrawAccount generates a single Account whose fields exercise
// the round-trip contract of Property 7. Optional fields (ClientID,
// ClientSecret, Region, Provider, ProfileArn) are independently either omitted
// (left at their zero value, so save serializes them away via omitempty) or
// populated. AuthMethod/Provider combinations include MS365-style social
// accounts (AuthMethod="social", Provider="MicrosoftEntra") alongside idc and
// other account shapes so the property holds across account types.
func ms365LoadFidelityDrawAccount(rt *rapid.T, label string) Account {
	// AuthMethod has no omitempty tag, so it always round-trips; pick from a
	// representative set including the MS365 social branch.
	authMethod := rapid.SampledFrom([]string{"social", "idc", "builderid", "github", "google"}).
		Draw(rt, label+"-authMethod")

	// Optionally omit each optional field to cover Requirement 8.4 (omitted
	// fields load as zero values). When "omit" is drawn we deliberately leave
	// the field empty so the persisted JSON omits it entirely.
	drawOptional := func(name string, values []string) string {
		if rapid.Bool().Draw(rt, label+"-omit-"+name) {
			return ""
		}
		return rapid.SampledFrom(values).Draw(rt, label+"-"+name)
	}

	provider := drawOptional("provider", []string{"MicrosoftEntra", "BuilderId", "GitHub", "Google", "Enterprise"})
	clientID := drawOptional("clientId", []string{"client-abc", "client-xyz", "idc-client-1"})
	clientSecret := drawOptional("clientSecret", []string{"secret-1", "secret-2", "hunter2"})
	region := drawOptional("region", []string{"us-east-1", "eu-west-1", "ap-southeast-2"})
	profileArn := drawOptional("profileArn", []string{
		"arn:aws:codewhisperer:us-east-1:123:profile/A",
		"arn:aws:codewhisperer:eu-west-1:456:profile/B",
	})

	return Account{
		ID:           uuid.New().String(),
		AccessToken:  "access-" + rapid.String().Draw(rt, label+"-accessToken"),
		RefreshToken: "refresh-" + rapid.String().Draw(rt, label+"-refreshToken"),
		ClientID:     clientID,
		ClientSecret: clientSecret,
		AuthMethod:   authMethod,
		Provider:     provider,
		Region:       region,
		ProfileArn:   profileArn,
		Enabled:      rapid.Bool().Draw(rt, label+"-enabled"),
	}
}

// TestMs365LoadFidelityRoundTripProperty verifies Property 7.
//
// Feature: microsoft-365-sso, Property 7: Loading a pre-existing configuration
// preserves account fields. For any set of previously stored accounts
// serialized to the configuration file, loading the configuration reproduces
// every account's AccessToken, RefreshToken, ClientID, ClientSecret, Region,
// AuthMethod, Provider, and ProfileArn unchanged and returns no error; account
// records that omit the optional fields load with those fields at their zero
// values.
//
// Validates: Requirements 8.3, 8.4
func TestMs365LoadFidelityRoundTripProperty(t *testing.T) {
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
		accounts := rapid.SliceOfN(
			rapid.Custom(func(r *rapid.T) Account {
				return ms365LoadFidelityDrawAccount(r, "acct")
			}),
			0, 8,
		).Draw(rt, "accounts")

		// Persist the generated accounts through the real save path, then load
		// them back from the same file — exercising the actual serialization
		// used in production (omitempty applies, so omitted optional fields are
		// absent from the JSON on disk).
		cfgLock.Lock()
		cfg.Accounts = accounts
		cfgLock.Unlock()
		if err := Save(); err != nil {
			rt.Fatalf("Save: %v", err)
		}

		// Loading must return no error (Requirements 8.3, 8.4).
		if err := Load(); err != nil {
			rt.Fatalf("Load returned error: %v", err)
		}

		loaded := GetAccounts()
		if len(loaded) != len(accounts) {
			rt.Fatalf("account count changed: got %d want %d", len(loaded), len(accounts))
		}

		for i := range accounts {
			want := accounts[i]
			got := loaded[i]

			if got.AccessToken != want.AccessToken {
				rt.Fatalf("account %d AccessToken: got %q want %q", i, got.AccessToken, want.AccessToken)
			}
			if got.RefreshToken != want.RefreshToken {
				rt.Fatalf("account %d RefreshToken: got %q want %q", i, got.RefreshToken, want.RefreshToken)
			}
			if got.ClientID != want.ClientID {
				rt.Fatalf("account %d ClientID: got %q want %q", i, got.ClientID, want.ClientID)
			}
			if got.ClientSecret != want.ClientSecret {
				rt.Fatalf("account %d ClientSecret: got %q want %q", i, got.ClientSecret, want.ClientSecret)
			}
			if got.Region != want.Region {
				rt.Fatalf("account %d Region: got %q want %q", i, got.Region, want.Region)
			}
			if got.AuthMethod != want.AuthMethod {
				rt.Fatalf("account %d AuthMethod: got %q want %q", i, got.AuthMethod, want.AuthMethod)
			}
			if got.Provider != want.Provider {
				rt.Fatalf("account %d Provider: got %q want %q", i, got.Provider, want.Provider)
			}
			if got.ProfileArn != want.ProfileArn {
				rt.Fatalf("account %d ProfileArn: got %q want %q", i, got.ProfileArn, want.ProfileArn)
			}

			// Requirement 8.4: any optional field the record omitted (left at
			// its zero value, hence absent from the persisted JSON) must load
			// back as the zero value rather than acquiring a spurious default.
			if want.ClientID == "" && got.ClientID != "" {
				rt.Fatalf("account %d omitted ClientID loaded as %q, want zero value", i, got.ClientID)
			}
			if want.ClientSecret == "" && got.ClientSecret != "" {
				rt.Fatalf("account %d omitted ClientSecret loaded as %q, want zero value", i, got.ClientSecret)
			}
			if want.Region == "" && got.Region != "" {
				rt.Fatalf("account %d omitted Region loaded as %q, want zero value", i, got.Region)
			}
			if want.Provider == "" && got.Provider != "" {
				rt.Fatalf("account %d omitted Provider loaded as %q, want zero value", i, got.Provider)
			}
			if want.ProfileArn == "" && got.ProfileArn != "" {
				rt.Fatalf("account %d omitted ProfileArn loaded as %q, want zero value", i, got.ProfileArn)
			}
		}
	})
}
