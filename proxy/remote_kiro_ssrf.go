package proxy

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

// lookupIP is injectable so unit tests can stub DNS without network access.
var lookupIP = net.LookupIP

// allowPrivateRemote reports whether private/loopback remote base URLs are permitted.
// Operators running multi-instance locally can set KIRO_ALLOW_PRIVATE_REMOTE=1.
func allowPrivateRemote() bool {
	v := strings.TrimSpace(os.Getenv("KIRO_ALLOW_PRIVATE_REMOTE"))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// normalizeRemoteBaseURL trims whitespace, strips a trailing slash, and removes a
// trailing "/v1" (with or without a final slash) so callers can append /v1/... paths.
func normalizeRemoteBaseURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	// Strip trailing slashes first, then optional /v1, then trailing slash again.
	for {
		s = strings.TrimRight(s, "/")
		if strings.HasSuffix(strings.ToLower(s), "/v1") {
			s = s[:len(s)-3]
			continue
		}
		break
	}
	return s
}

// validateRemoteBaseURL checks that raw is a safe http(s) base URL for a remote
// peer. Returns the canonical base (no trailing slash, no /v1) on success.
//
// Rejects: empty, non-http(s), missing host, userinfo, and hosts that resolve
// only/also to loopback, private, link-local, ULA, unspecified, or multicast IPs
// unless KIRO_ALLOW_PRIVATE_REMOTE is set.
func validateRemoteBaseURL(raw string) (string, error) {
	canonical := normalizeRemoteBaseURL(raw)
	if canonical == "" {
		return "", fmt.Errorf("remote base URL is required")
	}

	u, err := url.Parse(canonical)
	if err != nil {
		return "", fmt.Errorf("invalid remote base URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("remote base URL must use http or https scheme")
	}
	if u.Host == "" {
		return "", fmt.Errorf("remote base URL must include a host")
	}
	if u.User != nil {
		return "", fmt.Errorf("remote base URL must not include userinfo")
	}

	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("remote base URL must include a host")
	}

	if !allowPrivateRemote() {
		if err := rejectPrivateHost(host); err != nil {
			return "", err
		}
	}

	// Rebuild without path/query/fragment (path already stripped by normalize for /v1).
	// Keep any non-/v1 path segment if the operator intentionally hosts under a prefix.
	out := &url.URL{
		Scheme: u.Scheme,
		Host:   u.Host,
		Path:   strings.TrimRight(u.Path, "/"),
	}
	return out.String(), nil
}

// validateRemoteCheckKeyURL checks that a check-key endpoint URL is a safe
// http(s) URL. Unlike validateRemoteBaseURL it preserves the full path (the
// endpoint path differs per deployment) and does not strip a trailing /v1.
func validateRemoteCheckKeyURL(raw string) error {
	s := strings.TrimSpace(raw)
	if s == "" {
		return fmt.Errorf("check-key URL is required")
	}
	u, err := url.Parse(s)
	if err != nil {
		return fmt.Errorf("invalid check-key URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("check-key URL must use http or https scheme")
	}
	if u.Hostname() == "" {
		return fmt.Errorf("check-key URL must include a host")
	}
	if u.User != nil {
		return fmt.Errorf("check-key URL must not include userinfo")
	}
	if !allowPrivateRemote() {
		if err := rejectPrivateHost(u.Hostname()); err != nil {
			return err
		}
	}
	return nil
}

func rejectPrivateHost(host string) error {
	// Literal IP?
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return fmt.Errorf("remote base URL host resolves to a private or loopback address")
		}
		return nil
	}

	ips, err := lookupIP(host)
	if err != nil {
		return fmt.Errorf("remote base URL host DNS lookup failed: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("remote base URL host resolved to no addresses")
	}
	for _, ip := range ips {
		if isBlockedIP(ip) {
			return fmt.Errorf("remote base URL host resolves to a private or loopback address")
		}
	}
	return nil
}

func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	// IPv6 unique local addresses (fc00::/7) are covered by IsPrivate in recent Go;
	// keep an explicit check for older behavior.
	if ip4 := ip.To4(); ip4 == nil {
		if len(ip) >= 1 && (ip[0]&0xfe) == 0xfc {
			return true
		}
	}
	return false
}
