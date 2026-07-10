package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIPRemoteAddrOnly(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.5:12345"
	r.Header.Set("X-Forwarded-For", "198.51.100.1")
	if got := ClientIP(r, false); got != "203.0.113.5" {
		t.Fatalf("trust=false got %q", got)
	}
}

func TestClientIPTrustXFF(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:80"
	r.Header.Set("X-Forwarded-For", "198.51.100.7, 10.0.0.1")
	if got := ClientIP(r, true); got != "198.51.100.7" {
		t.Fatalf("xff got %q", got)
	}
}

func TestClientIPTrustXRealIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:80"
	r.Header.Set("X-Real-IP", "198.51.100.9")
	if got := ClientIP(r, true); got != "198.51.100.9" {
		t.Fatalf("xri got %q", got)
	}
}

func TestIPTrackerMultiIP(t *testing.T) {
	tr := newIPTracker()
	tr.track("k1", "1.1.1.1")
	tr.track("k1", "1.1.1.1")
	tr.track("k1", "2.2.2.2")
	if tr.uniqueCount("k1") != 2 {
		t.Fatalf("unique=%d", tr.uniqueCount("k1"))
	}
	snap := tr.snapshot("k1")
	if len(snap) != 2 {
		t.Fatalf("snap=%d", len(snap))
	}
	var found bool
	for _, s := range snap {
		if s.IP == "1.1.1.1" && s.Requests == 2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing 1.1.1.1 stats: %+v", snap)
	}
}
