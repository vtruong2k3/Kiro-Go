package proxy

import (
	"sync"
	"time"

	"kiro-go/store"
)

const maxIPsPerKey = 64

// keyIPStat is the per-key, per-IP request counter.
// Lifetime fields are persisted; RPM is computed live from RAM buckets only.
type keyIPStat struct {
	IP        string `json:"ip"`
	Requests  int64  `json:"requests"`
	FirstSeen int64  `json:"firstSeen"`
	LastSeen  int64  `json:"lastSeen"`
	RPM       int64  `json:"rpm"` // snapshot-only; never stored
}

// rpmBuckets is a sliding 60-second counter (one slot per second).
type rpmBuckets struct {
	slots [60]int64
	// epochSec is the unix second corresponding to slots[idx] being "current".
	epochSec int64
	idx      int
}

func (b *rpmBuckets) hit(now int64) {
	if now <= 0 {
		now = time.Now().Unix()
	}
	b.advance(now)
	b.slots[b.idx]++
}

func (b *rpmBuckets) advance(now int64) {
	if b.epochSec == 0 {
		b.epochSec = now
		b.idx = 0
		return
	}
	if now <= b.epochSec {
		return
	}
	delta := now - b.epochSec
	if delta >= 60 {
		for i := range b.slots {
			b.slots[i] = 0
		}
		b.epochSec = now
		b.idx = 0
		return
	}
	for i := int64(0); i < delta; i++ {
		b.idx = (b.idx + 1) % 60
		b.slots[b.idx] = 0
		b.epochSec++
	}
}

func (b *rpmBuckets) sum(now int64) int64 {
	if b.epochSec == 0 {
		return 0
	}
	b.advance(now)
	var total int64
	for _, v := range b.slots {
		total += v
	}
	return total
}

type ipEntry struct {
	stat keyIPStat
	rpm  rpmBuckets
}

// ipTracker records which client IPs used each API key.
// Lifetime stats can be hydrated from / flushed to the runtime store.
// RPM is memory-only and resets on process restart.
type ipTracker struct {
	mu     sync.Mutex
	data   map[string]map[string]*ipEntry // keyID -> ip -> entry
	keyRPM map[string]*rpmBuckets         // keyID -> aggregate RPM
	dirty  bool
}

func newIPTracker() *ipTracker {
	return &ipTracker{
		data:   make(map[string]map[string]*ipEntry),
		keyRPM: make(map[string]*rpmBuckets),
	}
}

// hydrate loads lifetime stats from the store. RPM stays zero.
func (t *ipTracker) hydrate(rows map[string]map[string]store.KeyIPRow) {
	if t == nil || len(rows) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for keyID, ips := range rows {
		m := t.data[keyID]
		if m == nil {
			m = make(map[string]*ipEntry, len(ips))
			t.data[keyID] = m
		}
		for ip, r := range ips {
			if ip == "" {
				continue
			}
			m[ip] = &ipEntry{
				stat: keyIPStat{
					IP:        ip,
					Requests:  r.Requests,
					FirstSeen: r.FirstSeen,
					LastSeen:  r.LastSeen,
				},
			}
		}
		// Bound after hydrate (defensive).
		for len(m) > maxIPsPerKey {
			t.evictOldestLocked(m)
		}
	}
	// Hydrate is a load of already-persisted data; not dirty.
	t.dirty = false
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
		m = make(map[string]*ipEntry)
		t.data[keyID] = m
	}
	if e, ok := m[ip]; ok {
		e.stat.Requests++
		e.stat.LastSeen = now
		e.rpm.hit(now)
	} else {
		if len(m) >= maxIPsPerKey {
			t.evictOldestLocked(m)
		}
		e := &ipEntry{
			stat: keyIPStat{IP: ip, Requests: 1, FirstSeen: now, LastSeen: now},
		}
		e.rpm.hit(now)
		m[ip] = e
	}
	kr := t.keyRPM[keyID]
	if kr == nil {
		kr = &rpmBuckets{}
		t.keyRPM[keyID] = kr
	}
	kr.hit(now)
	t.dirty = true
}

func (t *ipTracker) evictOldestLocked(m map[string]*ipEntry) {
	var oldestIP string
	var oldestSeen int64
	first := true
	for k, e := range m {
		if first || e.stat.LastSeen < oldestSeen {
			oldestIP = k
			oldestSeen = e.stat.LastSeen
			first = false
		}
	}
	if oldestIP != "" {
		delete(m, oldestIP)
	}
}

// snapshot returns a copy of stats for keyID with live RPM, sorted by rpm then lastSeen desc.
func (t *ipTracker) snapshot(keyID string) []keyIPStat {
	if t == nil || keyID == "" {
		return []keyIPStat{}
	}
	now := time.Now().Unix()
	t.mu.Lock()
	defer t.mu.Unlock()
	m := t.data[keyID]
	if len(m) == 0 {
		return []keyIPStat{}
	}
	out := make([]keyIPStat, 0, len(m))
	for _, e := range m {
		st := e.stat
		st.RPM = e.rpm.sum(now)
		out = append(out, st)
	}
	// Sort: rpm desc, then lastSeen desc (insertion sort; N ≤ 64).
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 {
			if out[j].RPM > out[j-1].RPM ||
				(out[j].RPM == out[j-1].RPM && out[j].LastSeen > out[j-1].LastSeen) {
				out[j], out[j-1] = out[j-1], out[j]
				j--
				continue
			}
			break
		}
	}
	return out
}

// keyRPMValue returns live aggregate RPM for keyID.
func (t *ipTracker) keyRPMValue(keyID string) int64 {
	if t == nil || keyID == "" {
		return 0
	}
	now := time.Now().Unix()
	t.mu.Lock()
	defer t.mu.Unlock()
	b := t.keyRPM[keyID]
	if b == nil {
		return 0
	}
	return b.sum(now)
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

// rpmByKey returns keyID -> live RPM for all keys that have RPM state or IP data.
func (t *ipTracker) rpmByKey() map[string]int64 {
	if t == nil {
		return map[string]int64{}
	}
	now := time.Now().Unix()
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[string]int64, len(t.keyRPM))
	for id, b := range t.keyRPM {
		if b != nil {
			out[id] = b.sum(now)
		}
	}
	return out
}

// removeKey drops all tracking for a deleted API key and marks dirty.
func (t *ipTracker) removeKey(keyID string) {
	if t == nil || keyID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.data[keyID]; ok {
		delete(t.data, keyID)
		t.dirty = true
	}
	delete(t.keyRPM, keyID)
}

// isDirty reports whether lifetime IP state needs flushing.
func (t *ipTracker) isDirty() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.dirty
}

// snapshotAllForFlush returns a flat list of lifetime rows and clears dirty on success path.
// Caller should persist then call markClean, or leave dirty if flush failed.
func (t *ipTracker) snapshotAllForFlush() []store.KeyIPRow {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []store.KeyIPRow
	for keyID, m := range t.data {
		for ip, e := range m {
			out = append(out, store.KeyIPRow{
				KeyID:     keyID,
				IP:        ip,
				Requests:  e.stat.Requests,
				FirstSeen: e.stat.FirstSeen,
				LastSeen:  e.stat.LastSeen,
			})
		}
	}
	return out
}

// markClean clears the dirty flag after a successful flush.
func (t *ipTracker) markClean() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.dirty = false
}

// keyIDs returns all tracked key IDs (for replace-on-evict style flushes if needed).
func (t *ipTracker) keyIDs() []string {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, 0, len(t.data))
	for id := range t.data {
		out = append(out, id)
	}
	return out
}
