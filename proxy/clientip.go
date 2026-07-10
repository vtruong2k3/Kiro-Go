package proxy

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP returns the best-effort client IP for the request.
// When trustProxy is false only RemoteAddr is used (spoof-safe for direct exposure).
// When trustProxy is true, X-Forwarded-For (leftmost) then X-Real-IP are preferred.
func ClientIP(r *http.Request, trustProxy bool) string {
	if r == nil {
		return ""
	}
	remote := hostFromRemoteAddr(r.RemoteAddr)
	if !trustProxy {
		return remote
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		for _, p := range parts {
			if ip := normalizeIP(strings.TrimSpace(p)); ip != "" {
				return ip
			}
		}
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		if ip := normalizeIP(strings.TrimSpace(xri)); ip != "" {
			return ip
		}
	}
	return remote
}

func hostFromRemoteAddr(addr string) string {
	if addr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		return normalizeIP(host)
	}
	return normalizeIP(addr)
}

func normalizeIP(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	ip := net.ParseIP(s)
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String()
}
