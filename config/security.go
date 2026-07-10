package config

import (
	"errors"
	"net"
	"strings"
	"sync"
	"time"
)

// BlockedIPEntry is the admin-facing view of a blocked IP.
type BlockedIPEntry struct {
	IP        string `json:"ip"`
	Reason    string `json:"reason,omitempty"`
	BlockedAt int64  `json:"blockedAt,omitempty"`
}

// blockedIPMeta stores optional reason/time for blocked IPs.
var (
	blockedIPSet  = map[string]struct{}{}
	blockedIPMeta = map[string]BlockedIPEntry{}
	blockedIPMu   sync.RWMutex
)

func rebuildBlockedIPSetLocked() {
	// Caller holds cfgLock. Rebuild the O(1) lookup set from cfg.BlockedIPs.
	if cfg == nil {
		return
	}
	blockedIPMu.Lock()
	defer blockedIPMu.Unlock()
	blockedIPSet = make(map[string]struct{}, len(cfg.BlockedIPs))
	nextMeta := make(map[string]BlockedIPEntry, len(cfg.BlockedIPs))
	for _, raw := range cfg.BlockedIPs {
		ip := normalizeIPString(raw)
		if ip == "" {
			continue
		}
		blockedIPSet[ip] = struct{}{}
		if m, ok := blockedIPMeta[ip]; ok {
			nextMeta[ip] = m
		} else {
			nextMeta[ip] = BlockedIPEntry{IP: ip}
		}
	}
	blockedIPMeta = nextMeta
}

// normalizeIPString validates and canonicalizes an IP string.
func normalizeIPString(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if host, _, err := net.SplitHostPort(s); err == nil {
		s = host
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String()
}

// GetTrustProxyHeaders returns whether reverse-proxy client IP headers are trusted.
func GetTrustProxyHeaders() bool {
	cfgLock.RLock()
	defer cfgLock.RUnlock()
	if cfg == nil {
		return false
	}
	return cfg.TrustProxyHeaders
}

// SetTrustProxyHeaders persists the trust-proxy toggle.
func SetTrustProxyHeaders(trust bool) error {
	cfgLock.Lock()
	defer cfgLock.Unlock()
	if cfg == nil {
		return errors.New("config not initialized")
	}
	cfg.TrustProxyHeaders = trust
	return saveLocked()
}

// IsIPBlocked reports whether ip is on the global deny list. O(1).
func IsIPBlocked(ip string) bool {
	ip = normalizeIPString(ip)
	if ip == "" {
		return false
	}
	blockedIPMu.RLock()
	_, ok := blockedIPSet[ip]
	blockedIPMu.RUnlock()
	return ok
}

// ListBlockedIPs returns blocked IPs with optional metadata for the admin UI.
func ListBlockedIPs() []BlockedIPEntry {
	cfgLock.RLock()
	defer cfgLock.RUnlock()
	if cfg == nil {
		return []BlockedIPEntry{}
	}
	blockedIPMu.RLock()
	defer blockedIPMu.RUnlock()
	out := make([]BlockedIPEntry, 0, len(cfg.BlockedIPs))
	for _, raw := range cfg.BlockedIPs {
		ip := normalizeIPString(raw)
		if ip == "" {
			continue
		}
		if m, ok := blockedIPMeta[ip]; ok {
			out = append(out, m)
		} else {
			out = append(out, BlockedIPEntry{IP: ip})
		}
	}
	return out
}

// BlockIP adds ip to the deny list and persists. Idempotent if already blocked.
func BlockIP(ip, reason string) error {
	ip = normalizeIPString(ip)
	if ip == "" {
		return errors.New("invalid IP address")
	}
	cfgLock.Lock()
	defer cfgLock.Unlock()
	if cfg == nil {
		return errors.New("config not initialized")
	}
	for _, existing := range cfg.BlockedIPs {
		if normalizeIPString(existing) == ip {
			blockedIPMu.Lock()
			m := blockedIPMeta[ip]
			m.IP = ip
			if reason != "" {
				m.Reason = reason
			}
			if m.BlockedAt == 0 {
				m.BlockedAt = time.Now().Unix()
			}
			blockedIPMeta[ip] = m
			blockedIPMu.Unlock()
			return nil
		}
	}
	cfg.BlockedIPs = append(cfg.BlockedIPs, ip)
	blockedIPMu.Lock()
	blockedIPSet[ip] = struct{}{}
	blockedIPMeta[ip] = BlockedIPEntry{
		IP:        ip,
		Reason:    strings.TrimSpace(reason),
		BlockedAt: time.Now().Unix(),
	}
	blockedIPMu.Unlock()
	if err := saveLocked(); err != nil {
		cfg.BlockedIPs = cfg.BlockedIPs[:len(cfg.BlockedIPs)-1]
		blockedIPMu.Lock()
		delete(blockedIPSet, ip)
		delete(blockedIPMeta, ip)
		blockedIPMu.Unlock()
		return err
	}
	return nil
}

// UnblockIP removes ip from the deny list. Idempotent.
func UnblockIP(ip string) error {
	ip = normalizeIPString(ip)
	if ip == "" {
		return errors.New("invalid IP address")
	}
	cfgLock.Lock()
	defer cfgLock.Unlock()
	if cfg == nil {
		return errors.New("config not initialized")
	}
	idx := -1
	for i, existing := range cfg.BlockedIPs {
		if normalizeIPString(existing) == ip {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	cfg.BlockedIPs = append(cfg.BlockedIPs[:idx], cfg.BlockedIPs[idx+1:]...)
	blockedIPMu.Lock()
	delete(blockedIPSet, ip)
	delete(blockedIPMeta, ip)
	blockedIPMu.Unlock()
	return saveLocked()
}
