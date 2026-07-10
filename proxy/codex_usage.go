package proxy

// codex_usage.go fetches ChatGPT WHAM rate-limit / usage windows for Codex accounts.
//
//   GET https://chatgpt.com/backend-api/wham/usage
//   Authorization: Bearer <access token>
//
// Source of truth: 9router open-sse/services/usage/codex.js

import (
	"encoding/json"
	"fmt"
	"io"
	"kiro-go/auth"
	"kiro-go/config"
	"kiro-go/logger"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const codexWhamUsageURL = "https://chatgpt.com/backend-api/wham/usage"

// CodexUsageSnapshot is the normalized result of a WHAM usage fetch.
type CodexUsageSnapshot struct {
	Plan         string
	LimitReached bool
	ResetCredits int
	Windows      []config.CodexQuotaWindow
}

// FetchCodexUsage calls ChatGPT WHAM and returns session/weekly/review windows.
func FetchCodexUsage(account *config.Account) (*CodexUsageSnapshot, error) {
	if account == nil {
		return nil, fmt.Errorf("codex usage: account is nil")
	}
	bearer := strings.TrimSpace(account.AccessToken)
	if bearer == "" {
		return nil, fmt.Errorf("codex usage: no access token")
	}

	client := GetClientForProxy(ResolveAccountProxyURL(account))
	req, err := http.NewRequest(http.MethodGet, codexWhamUsageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("codex usage: new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", codexUserAgent)
	req.Header.Set("originator", codexOriginator)
	req.Header.Set("version", codexVersion)

	accountID := strings.TrimSpace(account.CodexAccountID)
	if accountID == "" {
		_, accountID, _ = auth.DecodeCodexIDToken(account.CodexIDToken)
	}
	if accountID != "" {
		req.Header.Set("chatgpt-account-id", accountID)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("codex usage: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("codex usage: read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("codex usage: upstream %d: %s", resp.StatusCode, truncateForErr(string(body), 240))
	}

	snap, err := parseCodexWhamUsage(body)
	if err != nil {
		return nil, err
	}
	return snap, nil
}

func truncateForErr(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// parseCodexWhamUsage decodes the WHAM JSON payload. Exported for tests via
// the unexported name used by codex_usage_test.go in the same package.
func parseCodexWhamUsage(body []byte) (*CodexUsageSnapshot, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("codex usage: invalid JSON: %w", err)
	}

	snap := &CodexUsageSnapshot{}

	// Plan
	if p, ok := raw["plan_type"].(string); ok && strings.TrimSpace(p) != "" {
		snap.Plan = strings.TrimSpace(p)
	} else if summary, ok := raw["summary"].(map[string]interface{}); ok {
		if p, ok := summary["plan"].(string); ok {
			snap.Plan = strings.TrimSpace(p)
		}
	}

	// Reset credits
	if rc, ok := raw["rate_limit_reset_credits"].(map[string]interface{}); ok {
		snap.ResetCredits = int(math.Max(0, asFloat(rc["available_count"])))
	}

	// Primary rate limit object
	normal := firstMap(
		raw["rate_limit"],
		raw["rate_limits"],
		nestedMap(raw, "rate_limits_by_limit_id", "codex"),
	)
	review := firstMap(
		raw["code_review_rate_limit"],
		raw["review_rate_limit"],
		nestedMap(raw, "rate_limits_by_limit_id", "code_review"),
		nestedMap(raw, "rate_limits_by_limit_id", "codex_review"),
		nestedMap(raw, "rate_limits_by_limit_id", "review"),
	)
	if review == nil {
		if arr, ok := raw["additional_rate_limits"].([]interface{}); ok {
			for _, item := range arr {
				m, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				id := strings.ToLower(strings.TrimSpace(asString(m["limit_name"]) + asString(m["metered_feature"]) + asString(m["id"])))
				if strings.Contains(id, "review") {
					review = m
					break
				}
			}
		}
	}

	normalBody := rateLimitBody(normal)
	reviewBody := rateLimitBody(review)

	if normalBody != nil {
		if asBool(normalBody["limit_reached"]) {
			snap.LimitReached = true
		}
	}

	appendWindows := func(prefix string, body map[string]interface{}, source map[string]interface{}) {
		if body == nil && source == nil {
			return
		}
		// Prefer nested rate_limit body for windows, but also accept top-level.
		src := body
		if src == nil {
			src = source
		}
		primary := firstMap(src["primary_window"], src["primary"])
		if primary == nil && source != nil {
			primary = firstMap(source["primary_window"], source["primary"])
		}
		secondary := firstMap(src["secondary_window"], src["secondary"])
		if secondary == nil && source != nil {
			secondary = firstMap(source["secondary_window"], source["secondary"])
		}
		limitHit := asBool(src["limit_reached"])
		if primary != nil {
			key := "session"
			label := "Session"
			if prefix != "" {
				key = prefix + "_session"
				label = prefix + " session"
			}
			snap.Windows = append(snap.Windows, formatCodexWindow(key, label, primary, limitHit))
		}
		if secondary != nil {
			key := "weekly"
			label := "Weekly"
			if prefix != "" {
				key = prefix + "_weekly"
				label = prefix + " weekly"
			}
			snap.Windows = append(snap.Windows, formatCodexWindow(key, label, secondary, limitHit))
		}
	}

	appendWindows("", normalBody, normal)
	appendWindows("review", reviewBody, review)

	if logger.GetLevel() == logger.LevelDebug {
		logger.Debugf("[CodexUsage] plan=%s windows=%d limitReached=%v credits=%d",
			snap.Plan, len(snap.Windows), snap.LimitReached, snap.ResetCredits)
	}
	return snap, nil
}

func rateLimitBody(snapshot map[string]interface{}) map[string]interface{} {
	if snapshot == nil {
		return nil
	}
	if nested, ok := snapshot["rate_limit"].(map[string]interface{}); ok {
		return nested
	}
	return snapshot
}

func formatCodexWindow(key, label string, window map[string]interface{}, parentLimit bool) config.CodexQuotaWindow {
	used := asFloat(firstNonNil(window["used_percent"], window["percent_used"]))
	if used < 0 {
		used = 0
	}
	if used > 100 {
		used = 100
	}
	resetAt := parseCodexResetTime(firstNonNil(window["reset_at"], window["resets_at"], window["resetAt"]))
	limitHit := parentLimit || asBool(window["limit_reached"])
	return config.CodexQuotaWindow{
		Key:       key,
		Label:     label,
		UsedPct:   used,
		Remaining: math.Max(0, 100-used),
		ResetAt:   resetAt,
		LimitHit:  limitHit,
	}
}

func parseCodexResetTime(v interface{}) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return ""
		}
		// Already RFC3339-ish
		if _, err := time.Parse(time.RFC3339, s); err == nil {
			return s
		}
		if _, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return s
		}
		// Unix seconds as string
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
			if n > 1e12 {
				n = n / 1000
			}
			return time.Unix(n, 0).UTC().Format(time.RFC3339)
		}
		return s
	case float64:
		n := int64(t)
		if n <= 0 {
			return ""
		}
		if n > 1e12 {
			n = n / 1000
		}
		return time.Unix(n, 0).UTC().Format(time.RFC3339)
	case json.Number:
		n, err := t.Int64()
		if err != nil || n <= 0 {
			return ""
		}
		if n > 1e12 {
			n = n / 1000
		}
		return time.Unix(n, 0).UTC().Format(time.RFC3339)
	default:
		return ""
	}
}

func firstMap(vals ...interface{}) map[string]interface{} {
	for _, v := range vals {
		if m, ok := v.(map[string]interface{}); ok && m != nil {
			return m
		}
	}
	return nil
}

func nestedMap(root map[string]interface{}, keys ...string) map[string]interface{} {
	cur := interface{}(root)
	for _, k := range keys {
		m, ok := cur.(map[string]interface{})
		if !ok || m == nil {
			return nil
		}
		cur = m[k]
	}
	if m, ok := cur.(map[string]interface{}); ok {
		return m
	}
	return nil
}

func firstNonNil(vals ...interface{}) interface{} {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

func asFloat(v interface{}) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f
	default:
		return 0
	}
}

func asBool(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		return s == "true" || s == "1" || s == "yes"
	case float64:
		return t != 0
	default:
		return false
	}
}

func asString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		return ""
	}
}
