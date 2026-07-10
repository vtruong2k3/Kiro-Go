package proxy

import (
	"path/filepath"
	"testing"
	"time"

	"kiro-go/store"
)

func TestIPTrackBasicAndEvict(t *testing.T) {
	tr := newIPTracker()
	tr.track("k1", "1.1.1.1")
	tr.track("k1", "1.1.1.1")
	tr.track("k1", "2.2.2.2")

	if tr.uniqueCount("k1") != 2 {
		t.Fatalf("unique=%d", tr.uniqueCount("k1"))
	}
	snap := tr.snapshot("k1")
	if len(snap) != 2 {
		t.Fatalf("snap len %d", len(snap))
	}
	// 1.1.1.1 has 2 requests
	var found bool
	for _, s := range snap {
		if s.IP == "1.1.1.1" {
			found = true
			if s.Requests != 2 {
				t.Fatalf("requests=%d", s.Requests)
			}
			if s.RPM < 1 {
				t.Fatalf("rpm should be >=1, got %d", s.RPM)
			}
		}
	}
	if !found {
		t.Fatal("missing 1.1.1.1")
	}
	if tr.keyRPMValue("k1") < 2 {
		t.Fatalf("key rpm=%d", tr.keyRPMValue("k1"))
	}

	// Eviction at maxIPsPerKey
	for i := 0; i < maxIPsPerKey+5; i++ {
		tr.track("k2", "10.0.0."+itoa(i))
	}
	if tr.uniqueCount("k2") != maxIPsPerKey {
		t.Fatalf("evict want %d got %d", maxIPsPerKey, tr.uniqueCount("k2"))
	}
}

func TestIPTrackHydrateAndFlush(t *testing.T) {
	tr := newIPTracker()
	tr.hydrate(map[string]map[string]store.KeyIPRow{
		"k1": {
			"9.9.9.9": {KeyID: "k1", IP: "9.9.9.9", Requests: 42, FirstSeen: 100, LastSeen: 200},
		},
	})
	if tr.isDirty() {
		t.Fatal("hydrate should not dirty")
	}
	snap := tr.snapshot("k1")
	if len(snap) != 1 || snap[0].Requests != 42 || snap[0].RPM != 0 {
		t.Fatalf("hydrate snap: %+v", snap)
	}

	tr.track("k1", "9.9.9.9")
	if !tr.isDirty() {
		t.Fatal("track should dirty")
	}
	rows := tr.snapshotAllForFlush()
	if len(rows) != 1 || rows[0].Requests != 43 {
		t.Fatalf("flush rows: %+v", rows)
	}
	tr.markClean()
	if tr.isDirty() {
		t.Fatal("markClean failed")
	}

	tr.removeKey("k1")
	if tr.uniqueCount("k1") != 0 {
		t.Fatal("removeKey failed")
	}
	if !tr.isDirty() {
		t.Fatal("removeKey should dirty")
	}
}

func TestRPMBucketsWindow(t *testing.T) {
	var b rpmBuckets
	now := time.Now().Unix()
	for i := 0; i < 5; i++ {
		b.hit(now)
	}
	if b.sum(now) != 5 {
		t.Fatalf("sum=%d", b.sum(now))
	}
	// Jump 61s into the future → window cleared
	if b.sum(now+61) != 0 {
		t.Fatalf("after 61s sum=%d", b.sum(now+61))
	}
}

func TestIPTrackPersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	tr := newIPTracker()
	tr.track("k1", "1.2.3.4")
	tr.track("k1", "1.2.3.4")
	tr.track("k1", "5.6.7.8")
	rows := tr.snapshotAllForFlush()
	if err := s.UpsertKeyIPStats(rows); err != nil {
		t.Fatal(err)
	}

	tr2 := newIPTracker()
	loaded, err := s.LoadKeyIPStats()
	if err != nil {
		t.Fatal(err)
	}
	tr2.hydrate(loaded)
	if tr2.uniqueCount("k1") != 2 {
		t.Fatalf("unique=%d", tr2.uniqueCount("k1"))
	}
	snap := tr2.snapshot("k1")
	var r int64
	for _, st := range snap {
		if st.IP == "1.2.3.4" {
			r = st.Requests
		}
	}
	if r != 2 {
		t.Fatalf("requests after hydrate=%d", r)
	}
	// RPM not persisted
	if tr2.keyRPMValue("k1") != 0 {
		t.Fatalf("rpm after hydrate should be 0, got %d", tr2.keyRPMValue("k1"))
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [12]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}
