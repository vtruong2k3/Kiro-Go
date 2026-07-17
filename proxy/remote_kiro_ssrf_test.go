package proxy

import (
	"net"
	"os"
	"testing"
)

func TestNormalizeRemoteBaseURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"  https://ex.com  ", "https://ex.com"},
		{"https://ex.com/", "https://ex.com"},
		{"https://ex.com/v1", "https://ex.com"},
		{"https://ex.com/v1/", "https://ex.com"},
		{"https://ex.com/V1/", "https://ex.com"},
		{"https://ex.com/proxy/v1", "https://ex.com/proxy"},
	}
	for _, tc := range cases {
		if got := normalizeRemoteBaseURL(tc.in); got != tc.want {
			t.Errorf("normalizeRemoteBaseURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestValidateRemoteBaseURLRejectsBadSchemeAndUserinfo(t *testing.T) {
	// Force private-remote off for these cases.
	t.Setenv("KIRO_ALLOW_PRIVATE_REMOTE", "")
	// Stub DNS so public-looking hosts don't hit network.
	prev := lookupIP
	lookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("1.1.1.1")}, nil
	}
	t.Cleanup(func() { lookupIP = prev })

	if _, err := validateRemoteBaseURL("ftp://ex.com"); err == nil {
		t.Fatal("expected ftp scheme rejected")
	}
	if _, err := validateRemoteBaseURL("https://user:pass@ex.com"); err == nil {
		t.Fatal("expected userinfo rejected")
	}
	if _, err := validateRemoteBaseURL(""); err == nil {
		t.Fatal("expected empty rejected")
	}
}

func TestValidateRemoteBaseURLRejectsPrivateAndLoopback(t *testing.T) {
	t.Setenv("KIRO_ALLOW_PRIVATE_REMOTE", "")
	prev := lookupIP
	lookupIP = func(host string) ([]net.IP, error) {
		switch host {
		case "private.example":
			return []net.IP{net.ParseIP("10.0.0.5")}, nil
		case "loop.example":
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		default:
			return []net.IP{net.ParseIP("8.8.8.8")}, nil
		}
	}
	t.Cleanup(func() { lookupIP = prev })

	if _, err := validateRemoteBaseURL("http://127.0.0.1:8080"); err == nil {
		t.Fatal("expected loopback IP rejected")
	}
	if _, err := validateRemoteBaseURL("http://10.1.2.3"); err == nil {
		t.Fatal("expected private IP rejected")
	}
	if _, err := validateRemoteBaseURL("https://private.example"); err == nil {
		t.Fatal("expected private DNS rejected")
	}
	if _, err := validateRemoteBaseURL("https://loop.example"); err == nil {
		t.Fatal("expected loopback DNS rejected")
	}
}

func TestValidateRemoteBaseURLAllowsPublicAndStripsV1(t *testing.T) {
	t.Setenv("KIRO_ALLOW_PRIVATE_REMOTE", "")
	prev := lookupIP
	lookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("1.2.3.4")}, nil
	}
	t.Cleanup(func() { lookupIP = prev })

	got, err := validateRemoteBaseURL("https://peer.example.com/v1/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://peer.example.com" {
		t.Fatalf("got %q", got)
	}
}

func TestValidateRemoteBaseURLPrivateAllowedWithEnv(t *testing.T) {
	t.Setenv("KIRO_ALLOW_PRIVATE_REMOTE", "1")
	// No DNS stub needed for literal private IP when allow is on.
	got, err := validateRemoteBaseURL("http://127.0.0.1:8080/v1")
	if err != nil {
		t.Fatalf("expected private allowed: %v", err)
	}
	if got != "http://127.0.0.1:8080" {
		t.Fatalf("got %q", got)
	}
	_ = os.Unsetenv // keep import used if env helpers change
}
