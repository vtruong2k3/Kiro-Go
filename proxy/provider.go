package proxy

import (
	"kiro-go/config"
	"strings"
)

// CallProvider dispatches a generation request to the upstream provider that
// owns the selected account. AWS Kiro/CodeWhisperer/AmazonQ accounts go through
// CallKiroAPI; Antigravity (Google Cloud Code / Gemini) accounts go through
// CallAntigravityAPI. Both share the provider-neutral KiroStreamCallback so all
// SSE-emitting logic in the handlers stays unchanged.
//
// The dispatch keys on account.Provider (set to "Antigravity" at onboarding)
// rather than the requested model, because a single account is bound to exactly
// one upstream and model->account routing has already happened in the pool.
func CallProvider(account *config.Account, payload *KiroPayload, callback *KiroStreamCallback) error {
	if account != nil && isAntigravityAccount(account) {
		return CallAntigravityAPI(account, payload, callback)
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
