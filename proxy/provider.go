package proxy

import (
	"kiro-go/config"
	"strings"
)

// CallProvider dispatches a generation request to the upstream provider that
// owns the selected account. AWS Kiro/CodeWhisperer/AmazonQ accounts go through
// CallKiroAPI; Antigravity (Google Cloud Code / Gemini) accounts go through
// CallAntigravityAPI; Grok/xAI accounts go through CallGrokAPI.
// All share the provider-neutral KiroStreamCallback so all SSE-emitting logic
// in the handlers stays unchanged.
//
// The dispatch keys on account fields (Provider / AuthMethod / Grok* fields)
// rather than the requested model, because a single account is bound to exactly
// one upstream and model->account routing has already happened in the pool.
func CallProvider(account *config.Account, payload *KiroPayload, callback *KiroStreamCallback) error {
	if account != nil && isAntigravityAccount(account) {
		return CallAntigravityAPI(account, payload, callback)
	}
	if account != nil && isGrokAccount(account) {
		return CallGrokAPI(account, payload, callback)
	}
	return CallKiroAPI(account, payload, callback)
}

// isAntigravityAccount reports whether an account is served by the Antigravity
// (Google Cloud Code / Gemini) upstream.
func isAntigravityAccount(account *config.Account) bool {
	if account == nil {
		return false
	}
	return strings.EqualFold(account.AuthMethod, "antigravity") ||
		strings.EqualFold(account.Provider, "Antigravity")
}

// isGrokAccount reports whether an account should be routed to Grok/xAI.
// Both auth modes (official API key and Grok Build OAuth) share the same
// upstream (https://api.x.ai) and dispatch through CallGrokAPI.
func isGrokAccount(account *config.Account) bool {
	if account == nil {
		return false
	}
	if strings.EqualFold(account.Provider, "grok") ||
		strings.EqualFold(account.Provider, "xai") ||
		strings.EqualFold(account.AuthMethod, "grok") {
		return true
	}
	// Also treat accounts that carry an explicit Grok API key.
	if account.GrokAPIKey != "" {
		return true
	}
	return false
}

// isGrokOAuthAccount reports whether a Grok account authenticates via the Grok
// Build OAuth flow (Bearer access_token refreshed against xAI) rather than a
// static API key.
func isGrokOAuthAccount(account *config.Account) bool {
	if account == nil {
		return false
	}
	if strings.EqualFold(account.GrokAuthType, "oauth") {
		return true
	}
	// Fall back to credential shape: an OAuth account has tokens but no API key.
	if account.GrokAuthType == "" && account.GrokAPIKey == "" && account.RefreshToken != "" {
		return true
	}
	return false
}
