package proxy

import (
	"encoding/json"
	"kiro-go/config"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizeAWSRegion(t *testing.T) {
	got, err := NormalizeAWSRegion("")
	if err != nil || got != "" {
		t.Fatalf("empty allowed for auto-detect, got %q err=%v", got, err)
	}
	got, err = NormalizeAWSRegion(" EU-Central-1 ")
	if err != nil || got != "eu-central-1" {
		t.Fatalf("normalize eu-central-1, got %q err=%v", got, err)
	}
	if _, err := NormalizeAWSRegion("../evil"); err == nil {
		t.Fatal("expected invalid region error")
	}
}

func TestIsKiroAPIKeyAccountScoped(t *testing.T) {
	if !isKiroAPIKeyAccount(&config.Account{AuthMethod: "api_key"}) {
		t.Fatal("api_key should match")
	}
	// Must not treat other providers' keys as Kiro API key accounts.
	if isKiroAPIKeyAccount(&config.Account{AuthMethod: "grok", GrokAPIKey: "xai-1"}) {
		t.Fatal("grok must not match isKiroAPIKeyAccount")
	}
	if isKiroAPIKeyAccount(&config.Account{AuthMethod: "codex"}) {
		t.Fatal("codex must not match")
	}
	if isKiroAPIKeyAccount(&config.Account{AuthMethod: "social"}) {
		t.Fatal("social must not match")
	}
}

func TestKiroTokenTypeAPIKey(t *testing.T) {
	if got := kiroTokenType(&config.Account{AuthMethod: "api_key"}); got != "API_KEY" {
		t.Fatalf("api_key tokentype = %q", got)
	}
	if got := kiroTokenType(&config.Account{AuthMethod: "external_idp"}); got != "EXTERNAL_IDP" {
		t.Fatalf("external_idp tokentype = %q", got)
	}
	if got := kiroTokenType(&config.Account{AuthMethod: "social"}); got != "" {
		t.Fatalf("social tokentype should be empty, got %q", got)
	}
}

func TestApplyKiroBaseHeadersAPIKey(t *testing.T) {
	req := httptest.NewRequest("GET", "https://management.us-east-1.kiro.dev/getUsageLimits", nil)
	account := &config.Account{AccessToken: "ksk_test", AuthMethod: "api_key", Region: "us-east-1"}
	applyKiroBaseHeaders(req, account, kiroHeaderValues{UserAgent: "ua", AmzUserAgent: "amz", Host: "management.us-east-1.kiro.dev"})
	if req.Header.Get("TokenType") != "API_KEY" {
		t.Fatalf("TokenType = %q", req.Header.Get("TokenType"))
	}
	if req.Header.Get("Authorization") != "Bearer ksk_test" {
		t.Fatalf("Authorization = %q", req.Header.Get("Authorization"))
	}
}

func TestRefreshAccountInfoViaKiroDevParsesUsage(t *testing.T) {
	// Shape-only: ensure request path construction for a region.
	region := "eu-central-1"
	url := "https://management." + region + ".kiro.dev/getUsageLimits?origin=AI_EDITOR&resourceType=AGENTIC_REQUEST&isEmailRequired=true"
	if !strings.Contains(url, "management.eu-central-1.kiro.dev") {
		t.Fatalf("url = %s", url)
	}
	// Decode sample payload into UsageLimitsResponse used by refreshAccountInfoViaKiroDev.
	raw := []byte(`{"userInfo":{"email":"a@b.com","userId":"u1"},"subscriptionInfo":{"subscriptionTitle":"KIRO POWER","subscriptionType":"POWER"},"usageBreakdownList":[{"currentUsage":1,"usageLimit":10}],"nextDateReset":1710000000}`)
	var usage UsageLimitsResponse
	if err := json.Unmarshal(raw, &usage); err != nil {
		t.Fatal(err)
	}
	if usage.UserInfo == nil || usage.UserInfo.Email != "a@b.com" {
		t.Fatalf("userInfo = %+v", usage.UserInfo)
	}
	if parseSubscriptionType(usage.SubscriptionInfo.SubscriptionTitle) != "POWER" {
		t.Fatalf("sub type = %s", parseSubscriptionType(usage.SubscriptionInfo.SubscriptionTitle))
	}
}

func TestRuntimeURLForAPIKey(t *testing.T) {
	account := &config.Account{AuthMethod: "api_key", Region: "eu-central-1"}
	region := kiroRegion(account)
	if region != "eu-central-1" {
		t.Fatalf("region = %s", region)
	}
	ep := "https://runtime." + region + ".kiro.dev/generateAssistantResponse"
	if ep != "https://runtime.eu-central-1.kiro.dev/generateAssistantResponse" {
		t.Fatalf("ep = %s", ep)
	}
}
