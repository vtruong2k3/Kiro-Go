package proxy

import (
	"sync"
	"time"
)

const maxIPsPerKey = 64

// keyIPStat is the per-key, per-IP request counter (memory only).
type keyIPStat struct {
	IP        string `json:"ip"`
	Requests  int64  `json:"requests"`
	FirstSeen int64  `json:"firstSeen"`
	LastSeen  int64  `json:"lastSeen"`
}

// ipTracker records which client IPs used each API key without disk I/O.
type ipTracker struct {
	mu   sync.Mutex
	data map[string]map[string]*keyIPStat // keyID -> ip -> stat
}

func newIPTracker() *ipTracker {
	return &ipTracker{data: make(map[string]map[string]*keyIPStat)}
}

// track increments the counter for (keyID, ip). No-op on empty inputs.
func (t *ipTracker) track(keyID, ip string) {
	if t == nil || keyID == "" || ip == "" {
		return
	}
	now := time.Now().Unix()
	t.mu.Lock()
	defer t.mu.Unlock()
	m := t.data[keyID]
	if m == nil {
		m = make(map[string]*keyIPStat)
		t.data[keyID] = m
	}
	if st, ok := m[ip]; ok {
		st.Requests++
		st.LastSeen = now
		return
	}
	if len(m) >= maxIPsPerKey {
		var oldestIP string
		var oldestSeen int64
		first := true
		for k, st := range m {
			if first || st.LastSeen < oldestSeen {
				oldestIP = k
				oldestSeen = st.LastSeen
				first = false
			}
		}
		if oldestIP != "" {
			delete(m, oldestIP)
		}
	}
	m[ip] = &keyIPStat{IP: ip, Requests: 1, FirstSeen: now, LastSeen: now}
}

// snapshot returns a copy of stats for keyID, sorted by LastSeen desc.
func (t *ipTracker) snapshot(keyID string) []keyIPStat {
	if t == nil || keyID == "" {
		return []keyIPStat{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	m := t.data[keyID]
	if len(m) == 0 {
		return []keyIPStat{}
	}
	out := make([]keyIPStat, 0, len(m))
	for _, st := range m {
		out = append(out, *st)
	}
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].LastSeen > out[j-1].LastSeen {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

// uniqueCount returns the number of distinct IPs seen for keyID.
func (t *ipTracker) uniqueCount(keyID string) int {
	if t == nil || keyID == "" {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.data[keyID])
}

// uniqueCounts returns a map of keyID -> unique IP count for all tracked keys.
func (t *ipTracker) uniqueCounts() map[string]int {
	if t == nil {
		return map[string]int{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[string]int, len(t.data))
	for id, m := range t.data {
		out[id] = len(m)
	}
	return out
}
