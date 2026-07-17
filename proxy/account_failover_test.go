package proxy

import (
	"errors"
	"kiro-go/config"
	"testing"
)

func TestAccountFailureClassifiers(t *testing.T) {
	tests := []struct {
		name string
		fn   func(string) bool
		msg  string
	}{
		{name: "quota", fn: isQuotaErrorMessage, msg: "HTTP 429: quota exhausted"},
		{name: "overage", fn: isOverageErrorMessage, msg: "HTTP 402 from Kiro IDE: OVERAGE limit exceeded"},
		{name: "suspension", fn: isSuspensionErrorMessage, msg: "Your User ID temporarily is suspended"},
		{name: "profile", fn: isProfileUnavailableErrorMessage, msg: "no available Kiro profile"},
		{name: "auth", fn: isAuthErrorMessage, msg: "Authentication failed - token invalid or expired"},
	}

	for _, tc := range tests {
		if !tc.fn(tc.msg) {
			t.Fatalf("%s classifier did not match %q", tc.name, tc.msg)
		}
	}
}

func TestClassifyAccountFailure(t *testing.T) {
	acc := &config.Account{ID: "a1", AuthMethod: "social"}
	ksk := &config.Account{ID: "k1", AuthMethod: "api_key"}

	cases := []struct {
		name    string
		acc     *config.Account
		err     error
		refresh bool
		want    string
	}{
		{"quota", acc, errors.New("HTTP 429 from kiro: quota"), false, EventQuota},
		{"overage", acc, errors.New("HTTP 402 from kiro: overage limit"), false, EventOverage},
		{"suspend", acc, errors.New("account temporarily_suspended"), false, EventBan},
		{"profile", acc, errors.New("no available Kiro profile"), false, EventSoft},
		{"auth request", acc, errors.New("HTTP 403 from kiro: forbidden"), false, EventBan},
		{"auth refresh", acc, errors.New("HTTP 401 from oidc: unauthorized"), true, EventTokenRefresh},
		{"default soft", acc, errors.New("connection reset"), false, EventSoft},
		{"default refresh", acc, errors.New("connection reset"), true, EventTokenRefresh},
		{"ksk soft", ksk, errors.New("HTTP 403 from kiro"), false, EventSoft},
	}
	for _, tc := range cases {
		got := classifyAccountFailure(tc.acc, tc.err, tc.refresh)
		if got != tc.want {
			t.Fatalf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}
