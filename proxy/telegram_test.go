package proxy

import (
	"encoding/json"
	"io"
	"kiro-go/config"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMaskBotToken(t *testing.T) {
	if masked, set := MaskBotToken(""); set || masked != "" {
		t.Fatalf("empty: masked=%q set=%v", masked, set)
	}
	if masked, set := MaskBotToken("abcd"); !set || masked != "••••" {
		t.Fatalf("short: masked=%q set=%v", masked, set)
	}
	if masked, set := MaskBotToken("123456:AAAxyz"); !set || masked != "••••Axyz" {
		// last 4 of "123456:AAAxyz" = "Axyz"
		t.Fatalf("long: masked=%q set=%v", masked, set)
	}
}

func TestShouldSendDedup(t *testing.T) {
	n := newTelegramNotifier()
	if !n.shouldSend("a|quota") {
		t.Fatal("first send should be allowed")
	}
	if n.shouldSend("a|quota") {
		t.Fatal("second send within TTL should be blocked")
	}
	if !n.shouldSend("a|ban") {
		t.Fatal("different event should be allowed")
	}
	if !n.shouldSend("b|quota") {
		t.Fatal("different account should be allowed")
	}
	// Force expire
	n.mu.Lock()
	n.lastSent["a|quota"] = time.Now().Add(-telegramDedupTTL - time.Second)
	n.mu.Unlock()
	if !n.shouldSend("a|quota") {
		t.Fatal("after TTL should be allowed again")
	}
}

func TestSendMessageHTTP(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	n := newTelegramNotifier()
	n.apiBase = srv.URL
	if err := n.sendMessage("tok123", "-1001", "hello"); err != nil {
		t.Fatalf("sendMessage: %v", err)
	}
	if gotPath != "/bottok123/sendMessage" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotBody["chat_id"] != "-1001" || gotBody["text"] != "hello" {
		t.Fatalf("body = %#v", gotBody)
	}
}

func TestSendMessageHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"ok":false,"description":"Unauthorized"}`))
	}))
	defer srv.Close()
	n := newTelegramNotifier()
	n.apiBase = srv.URL
	err := n.sendMessage("bad", "1", "x")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected 401 error, got %v", err)
	}
}

func TestNotifyAccountEventDisabledNoHTTP(t *testing.T) {
	if err := config.Init(filepath.Join(t.TempDir(), "config.json")); err != nil {
		t.Fatalf("Init: %v", err)
	}
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	// Swap global notifier base for this test.
	old := telegram
	telegram = newTelegramNotifier()
	telegram.apiBase = srv.URL
	defer func() { telegram = old }()

	acc := &config.Account{ID: "acc1", Email: "a@b.c"}
	NotifyAccountEvent(acc, EventQuota, "HTTP 429")
	time.Sleep(50 * time.Millisecond)
	if hits.Load() != 0 {
		t.Fatalf("disabled config must not hit HTTP, hits=%d", hits.Load())
	}
}

func TestFormatAccountEventMessage(t *testing.T) {
	acc := &config.Account{ID: "id1", Email: "user@example.com"}
	msg := formatAccountEventMessage(acc, EventBan, "HTTP 403 forbidden")
	for _, want := range []string{"Account banned", "user@example.com", "id1", "ban", "HTTP 403"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q:\n%s", want, msg)
		}
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("hello", 10); got != "hello" {
		t.Fatalf("short: %q", got)
	}
	if got := truncateRunes("abcdefghij", 5); got != "abcde…" {
		t.Fatalf("long: %q", got)
	}
}
