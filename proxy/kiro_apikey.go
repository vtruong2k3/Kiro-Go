package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"kiro-go/config"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// awsRegionPattern matches standard AWS region codes (e.g. us-east-1, eu-central-1).
var awsRegionPattern = regexp.MustCompile(`^[a-z]{2}(?:-[a-z]+)+-\d{1,2}$`)

// Default probe order for Kiro CLI API keys (ksk_). User-selected region is tried first.
var kiroAPIKeyProbeRegions = []string{
	"us-east-1",
	"eu-central-1",
	"ap-northeast-1",
	"eu-west-1",
	"ap-southeast-1",
	"us-west-2",
}

// NormalizeAWSRegion trims and validates an AWS region code. Empty is allowed (auto-detect).
func NormalizeAWSRegion(region string) (string, error) {
	r := strings.TrimSpace(strings.ToLower(region))
	if r == "" {
		return "", nil
	}
	if !awsRegionPattern.MatchString(r) {
		return "", fmt.Errorf("invalid AWS region %q", region)
	}
	return r, nil
}

// KiroAPIKeyCredential is the validated, ready-to-persist shape for a Kiro CLI
// API key (ksk_...) import (authMethod=api_key).
type KiroAPIKeyCredential struct {
	AccessToken       string
	Region            string
	Email             string
	UserId            string
	SubscriptionType  string
	SubscriptionTitle string
	UsageCurrent      float64
	UsageLimit        float64
	UsagePercent      float64
	NextResetDate     string
	LastRefresh       int64
}

// ValidateKiroAPIKey validates a Kiro CLI API key (ksk_...) by probing
// management.{region}.kiro.dev/getUsageLimits with TokenType=API_KEY.
// Empty region means auto-detect across known regions (user region first if set).
//
// This is intentionally separate from OAuth / CodeWhisperer account paths.
func ValidateKiroAPIKey(apiKey, region string) (*KiroAPIKeyCredential, error) {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return nil, fmt.Errorf("API key is required")
	}
	given, err := NormalizeAWSRegion(region)
	if err != nil {
		return nil, err
	}

	tryRegions := make([]string, 0, len(kiroAPIKeyProbeRegions)+1)
	if given != "" {
		tryRegions = append(tryRegions, given)
	}
	for _, rg := range kiroAPIKeyProbeRegions {
		if rg != given {
			tryRegions = append(tryRegions, rg)
		}
	}

	var lastErr error
	for _, rg := range tryRegions {
		probe := &config.Account{
			AccessToken: key,
			AuthMethod:  "api_key",
			Provider:    "API Key",
			Region:      rg,
		}
		info, e := refreshAccountInfoViaKiroDev(probe)
		if e == nil && info != nil {
			email := strings.TrimSpace(info.Email)
			return &KiroAPIKeyCredential{
				AccessToken:       key,
				Region:            rg,
				Email:             email,
				UserId:            info.UserId,
				SubscriptionType:  info.SubscriptionType,
				SubscriptionTitle: info.SubscriptionTitle,
				UsageCurrent:      info.UsageCurrent,
				UsageLimit:        info.UsageLimit,
				UsagePercent:      info.UsagePercent,
				NextResetDate:     info.NextResetDate,
				LastRefresh:       info.LastRefresh,
			}, nil
		}
		lastErr = e
	}

	msg := "invalid in all probed regions"
	if lastErr != nil {
		msg = lastErr.Error()
	}
	return nil, fmt.Errorf("API key validation failed: %s", msg)
}

// refreshAccountInfoViaKiroDev fetches usage/subscription for Kiro CLI API-key
// accounts via management.{region}.kiro.dev (not AWS CodeWhisperer — ksk_ tokens
// are rejected there). A 200 also proves the key is live in that region.
func refreshAccountInfoViaKiroDev(account *config.Account) (*config.AccountInfo, error) {
	if account == nil {
		return nil, fmt.Errorf("account is nil")
	}
	region := strings.TrimSpace(account.Region)
	if region == "" {
		region = "us-east-1"
	}
	if !awsRegionPattern.MatchString(region) {
		return nil, fmt.Errorf("invalid AWS region %q", region)
	}

	url := fmt.Sprintf(
		"https://management.%s.kiro.dev/getUsageLimits?origin=AI_EDITOR&resourceType=AGENTIC_REQUEST&isEmailRequired=true",
		region,
	)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+account.AccessToken)
	req.Header.Set("TokenType", "API_KEY")

	resp, err := GetRestClientForProxy(ResolveAccountProxyURL(account)).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var usage UsageLimitsResponse
	if err := json.Unmarshal(body, &usage); err != nil {
		return nil, err
	}

	info := &config.AccountInfo{LastRefresh: time.Now().Unix()}
	if usage.UserInfo != nil {
		info.Email = usage.UserInfo.Email
		info.UserId = usage.UserInfo.UserId
	}
	if usage.SubscriptionInfo != nil {
		titleOrName := usage.SubscriptionInfo.SubscriptionTitle
		if titleOrName == "" {
			titleOrName = usage.SubscriptionInfo.SubscriptionName
		}
		if titleOrName == "" {
			titleOrName = usage.SubscriptionInfo.SubscriptionType
		}
		info.SubscriptionType = parseSubscriptionType(titleOrName)
		info.SubscriptionTitle = usage.SubscriptionInfo.SubscriptionTitle
		if info.SubscriptionTitle == "" {
			info.SubscriptionTitle = usage.SubscriptionInfo.SubscriptionName
		}
	}
	if len(usage.UsageBreakdownList) > 0 {
		b := usage.UsageBreakdownList[0]
		info.UsageCurrent = b.CurrentUsage
		info.UsageLimit = b.UsageLimit
		if info.UsageLimit > 0 {
			info.UsagePercent = info.UsageCurrent / info.UsageLimit
		}
	}
	if usage.NextDateReset != "" {
		if ts, err := usage.NextDateReset.Int64(); err == nil && ts > 0 {
			info.NextResetDate = time.Unix(ts, 0).Format("2006-01-02")
		} else if f, err := usage.NextDateReset.Float64(); err == nil && f > 0 {
			info.NextResetDate = time.Unix(int64(f), 0).Format("2006-01-02")
		}
	}
	return info, nil
}
