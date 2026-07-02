package auth

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"kiro-go/config"

	"pgregory.net/rapid"
)

// Feature: microsoft-365-sso, Property 4
//
// Property 4: Token refresh dispatches strictly on AuthMethod.
//
// auth.RefreshToken must branch purely on account.AuthMethod:
//   - AuthMethod == "social" (including MS365 / Provider == "MicrosoftEntra")
//     builds the social endpoint (socialTokenURL) and MUST NOT contact the
//     OIDC endpoint.
//   - any other AuthMethod contacts the OIDC endpoint (oidcTokenURL) and MUST
//     NOT contact the social endpoint.
//
// Validates: Requirements 4.1, 4.7, 8.5

// ms365DispatchSocialHost / ms365DispatchOIDCSuffix are distinctive markers so
// the recorded request URL can be classified unambiguously.
const (
	ms365DispatchSocialHost  = "social.ms365dispatch.test"
	ms365DispatchOIDCPattern = "ms365dispatch.test/token"
)

// ms365DispatchRecorder is an http.RoundTripper that records the URL of the
// last request it saw and returns a canned successful refresh response, so the
// test never touches the network while still observing which endpoint
// RefreshToken chose to call.
type ms365DispatchRecorder struct {
	mu      sync.Mutex
	lastURL string
}

func (r *ms365DispatchRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.lastURL = req.URL.String()
	r.mu.Unlock()

	const body = `{"accessToken":"at","refreshToken":"rt","expiresIn":3600,"profileArn":"arn"}`
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

func (r *ms365DispatchRecorder) reset() {
	r.mu.Lock()
	r.lastURL = ""
	r.mu.Unlock()
}

func (r *ms365DispatchRecorder) recorded() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastURL
}

func TestRefreshTokenDispatchProperty(t *testing.T) {
	// Initialize a temporary config so RefreshToken's config.GetProxyURL()
	// fallback (used when account.ProxyURL is empty) does not dereference a nil
	// global config. Default ProxyURL is empty, so the global recording client
	// installed below is used.
	cfgFile := filepath.Join(t.TempDir(), "config.json")
	if err := config.Init(cfgFile); err != nil {
		t.Fatalf("config.Init: %v", err)
	}

	// Override the endpoint builders with distinctive, offline URLs and restore
	// them after the test.
	origOIDC := oidcTokenURL
	origSocial := socialTokenURL
	oidcTokenURL = func(region string) string {
		return "https://oidc." + region + ".ms365dispatch.test/token"
	}
	socialTokenURL = func() string {
		return "https://" + ms365DispatchSocialHost + "/refresh"
	}
	defer func() {
		oidcTokenURL = origOIDC
		socialTokenURL = origSocial
	}()

	// Install a recording client (Proxy=nil so localhost-style hosts resolve
	// through our transport) and restore the previous global client afterwards.
	recorder := &ms365DispatchRecorder{}
	prevClient := SetGlobalAuthClientForTest(&http.Client{Transport: recorder})
	defer SetGlobalAuthClientForTest(prevClient)

	// AuthMethod values spanning both branches. "social" is the only value that
	// selects the social branch; everything else (including "", enterprise
	// variants and random strings) selects the OIDC branch.
	authMethods := []string{"social", "idc", "builderid", "enterprise", "google", "github", ""}
	providers := []string{"MicrosoftEntra", "GitHub", "Google", "BuilderId", ""}
	regions := []string{"us-east-1", "eu-west-1", "ap-southeast-2", "us-west-2"}

	rapid.Check(t, func(t *rapid.T) {
		var authMethod string
		// Bias generation across the known set plus occasional random strings so
		// the "any other AuthMethod" branch is exercised broadly.
		if rapid.Bool().Draw(t, "useKnownAuthMethod") {
			authMethod = rapid.SampledFrom(authMethods).Draw(t, "authMethod")
		} else {
			authMethod = rapid.String().Draw(t, "randomAuthMethod")
			// A random string equal to "social" would flip the expected branch;
			// that's fine because the assertion keys off the same value.
		}

		provider := rapid.SampledFrom(providers).Draw(t, "provider")
		// Guarantee MS365 accounts (social + MicrosoftEntra) are covered.
		if authMethod == "social" && rapid.Bool().Draw(t, "forceMs365") {
			provider = "MicrosoftEntra"
		}

		refreshToken := rapid.StringMatching(`[A-Za-z0-9._-]{1,40}`).Draw(t, "refreshToken")

		account := &config.Account{
			ID:           rapid.StringMatching(`[a-z0-9-]{1,12}`).Draw(t, "id"),
			AuthMethod:   authMethod,
			Provider:     provider,
			RefreshToken: refreshToken,
			// Non-social accounts require clientId/clientSecret/region for the
			// OIDC branch to actually issue a request.
			ClientID:     rapid.StringMatching(`[A-Za-z0-9]{1,20}`).Draw(t, "clientID"),
			ClientSecret: rapid.StringMatching(`[A-Za-z0-9]{1,20}`).Draw(t, "clientSecret"),
			Region:       rapid.SampledFrom(regions).Draw(t, "region"),
			// Empty ProxyURL so RefreshToken uses the global recording client.
			ProxyURL: "",
		}

		recorder.reset()
		_, _, _, _, _ = RefreshToken(account)
		hit := recorder.recorded()

		if hit == "" {
			t.Fatalf("RefreshToken issued no request for authMethod=%q provider=%q", authMethod, provider)
		}

		hitSocial := strings.Contains(hit, ms365DispatchSocialHost)
		hitOIDC := strings.Contains(hit, ms365DispatchOIDCPattern)

		if account.AuthMethod == "social" {
			if !hitSocial {
				t.Fatalf("social account (provider=%q) did not contact social endpoint; hit=%q", provider, hit)
			}
			if hitOIDC {
				t.Fatalf("social account (provider=%q) contacted OIDC endpoint; hit=%q", provider, hit)
			}
		} else {
			if !hitOIDC {
				t.Fatalf("non-social account (authMethod=%q) did not contact OIDC endpoint; hit=%q", authMethod, hit)
			}
			if hitSocial {
				t.Fatalf("non-social account (authMethod=%q) contacted social endpoint; hit=%q", authMethod, hit)
			}
		}
	})
}
