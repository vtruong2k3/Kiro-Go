package proxy

import (
	"fmt"
	"kiro-go/config"
	"net/http"
	"strings"
)

const (
	kiroStreamingSDKVersion = "1.0.34"
	kiroRuntimeSDKVersion   = "1.0.0"
)

type kiroHeaderValues struct {
	UserAgent    string
	AmzUserAgent string
	Host         string
}

func buildStreamingHeaderValues(account *config.Account, host string) kiroHeaderValues {
	return buildKiroHeaderValues(account, host, "codewhispererstreaming", kiroStreamingSDKVersion, "m/E")
}

func buildRuntimeHeaderValues(account *config.Account, host string) kiroHeaderValues {
	return buildKiroHeaderValues(account, host, "codewhispererruntime", kiroRuntimeSDKVersion, "m/N,E")
}

func buildKiroHeaderValues(account *config.Account, host, apiName, sdkVersion, mode string) kiroHeaderValues {
	clientCfg := config.GetKiroClientConfig()
	machineID := ""
	if account != nil {
		machineID = account.MachineId
	}

	userAgent := fmt.Sprintf(
		"aws-sdk-js/%s ua/2.1 os/%s lang/js md/nodejs#%s api/%s#%s %s KiroIDE-%s",
		sdkVersion,
		clientCfg.SystemVersion,
		clientCfg.NodeVersion,
		apiName,
		sdkVersion,
		mode,
		clientCfg.KiroVersion,
	)
	amzUserAgent := fmt.Sprintf("aws-sdk-js/%s KiroIDE-%s", sdkVersion, clientCfg.KiroVersion)
	if machineID != "" {
		userAgent += "-" + machineID
		amzUserAgent += "-" + machineID
	}

	return kiroHeaderValues{
		UserAgent:    userAgent,
		AmzUserAgent: amzUserAgent,
		Host:         host,
	}
}

func applyKiroBaseHeaders(req *http.Request, account *config.Account, values kiroHeaderValues) {
	if account != nil && account.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+account.AccessToken)
	}
	if tokenType := kiroTokenType(account); tokenType != "" {
		// net/http canonicalizes the key; upstream treats TokenType case-insensitively.
		// external_idp → EXTERNAL_IDP; Kiro ksk_ api_key → API_KEY.
		req.Header.Set("TokenType", tokenType)
	}
	req.Header.Set("User-Agent", values.UserAgent)
	req.Header.Set("x-amz-user-agent", values.AmzUserAgent)
	req.Header.Set("x-amzn-codewhisperer-optout", "true")
	if values.Host != "" {
		req.Host = values.Host
	}
}

// isKiroAPIKeyAccount reports whether the account is a Kiro CLI API-key (ksk_)
// account. These use management/runtime.{region}.kiro.dev and need no OAuth refresh.
// Scoped only to AuthMethod == "api_key" so Grok/Codex API keys are never matched.
func isKiroAPIKeyAccount(account *config.Account) bool {
	return account != nil && strings.EqualFold(strings.TrimSpace(account.AuthMethod), "api_key")
}

// kiroTokenType maps an account's auth method to the TokenType header value the
// Kiro backend expects.
//   - external_idp → EXTERNAL_IDP (Microsoft Entra)
//   - api_key      → API_KEY (Kiro CLI ksk_ keys on kiro.dev)
//
// Other methods omit the header.
func kiroTokenType(account *config.Account) string {
	if account == nil {
		return ""
	}
	switch account.AuthMethod {
	case "external_idp":
		return "EXTERNAL_IDP"
	case "api_key":
		return "API_KEY"
	default:
		return ""
	}
}
