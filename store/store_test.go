package store

import (
	"path/filepath"
	"testing"
)

func TestOpenMigrateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kiro-runtime.db")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	logs := []RequestLogRow{
		{Time: 100, Endpoint: "openai", Model: "m1", Status: "success", Tokens: 10, ClientIP: "1.1.1.1"},
		{Time: 200, Endpoint: "claude", Model: "m2", Status: "error", Error: "boom", ErrorType: "unknown", ClientIP: "2.2.2.2"},
		{Time: 300, Endpoint: "openai", Model: "m3", Status: "success", Tokens: 5, ApiKeyID: "k1"},
	}
	if err := s.InsertRequestLogs(logs); err != nil {
		t.Fatalf("InsertRequestLogs: %v", err)
	}

	loaded, err := s.LoadRecentRequestLogs(10)
	if err != nil {
		t.Fatalf("LoadRecentRequestLogs: %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("want 3 logs, got %d", len(loaded))
	}
	// oldest → newest
	if loaded[0].Time != 100 || loaded[2].Time != 300 {
		t.Fatalf("order wrong: %+v", loaded)
	}
	if loaded[1].Error != "boom" || loaded[2].ApiKeyID != "k1" {
		t.Fatalf("fields mismatch: %+v", loaded)
	}

	// reopen
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	loaded2, err := s2.LoadRecentRequestLogs(2)
	if err != nil {
		t.Fatalf("LoadRecent after reopen: %v", err)
	}
	if len(loaded2) != 2 || loaded2[0].Time != 200 || loaded2[1].Time != 300 {
		t.Fatalf("limit/order after reopen: %+v", loaded2)
	}
}

func TestPruneAndClearLogs(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	rows := make([]RequestLogRow, 0, 20)
	for i := 1; i <= 20; i++ {
		rows = append(rows, RequestLogRow{Time: int64(i), Endpoint: "x", Status: "success"})
	}
	if err := s.InsertRequestLogs(rows); err != nil {
		t.Fatal(err)
	}
	if err := s.PruneRequestLogs(5); err != nil {
		t.Fatal(err)
	}
	n, err := s.CountRequestLogs()
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Fatalf("after prune want 5, got %d", n)
	}
	kept, err := s.LoadRecentRequestLogs(10)
	if err != nil {
		t.Fatal(err)
	}
	if kept[0].Time != 16 || kept[4].Time != 20 {
		t.Fatalf("prune kept wrong rows: %+v", kept)
	}

	if err := s.ClearRequestLogs(); err != nil {
		t.Fatal(err)
	}
	n, _ = s.CountRequestLogs()
	if n != 0 {
		t.Fatalf("clear want 0, got %d", n)
	}
}

func TestKeyIPUpsertLoadDelete(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "ip.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.UpsertKeyIPStats([]KeyIPRow{
		{KeyID: "k1", IP: "1.1.1.1", Requests: 5, FirstSeen: 10, LastSeen: 20},
		{KeyID: "k1", IP: "2.2.2.2", Requests: 1, FirstSeen: 11, LastSeen: 21},
		{KeyID: "k2", IP: "3.3.3.3", Requests: 9, FirstSeen: 1, LastSeen: 2},
	}); err != nil {
		t.Fatal(err)
	}

	// bump k1/1.1.1.1 — first_seen should stay 10
	if err := s.UpsertKeyIPStats([]KeyIPRow{
		{KeyID: "k1", IP: "1.1.1.1", Requests: 8, FirstSeen: 15, LastSeen: 30},
	}); err != nil {
		t.Fatal(err)
	}

	all, err := s.LoadKeyIPStats()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || len(all["k1"]) != 2 {
		t.Fatalf("unexpected map: %+v", all)
	}
	r := all["k1"]["1.1.1.1"]
	if r.Requests != 8 || r.FirstSeen != 10 || r.LastSeen != 30 {
		t.Fatalf("upsert merge wrong: %+v", r)
	}

	if err := s.DeleteKeyIPStats("k1"); err != nil {
		t.Fatal(err)
	}
	all, _ = s.LoadKeyIPStats()
	if _, ok := all["k1"]; ok {
		t.Fatalf("k1 should be gone: %+v", all)
	}
	if len(all["k2"]) != 1 {
		t.Fatalf("k2 should remain: %+v", all)
	}
}

func TestNilStoreSafe(t *testing.T) {
	var s *Store
	if err := s.InsertRequestLogs([]RequestLogRow{{Time: 1}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadRecentRequestLogs(10); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearRequestLogs(); err != nil {
		t.Fatal(err)
	}
	if err := s.PruneRequestLogs(1); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertKeyIPStats([]KeyIPRow{{KeyID: "k", IP: "1", Requests: 1}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadKeyIPStats(); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteKeyIPStats("k"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestResolvePath(t *testing.T) {
	t.Setenv("RUNTIME_DB_PATH", "")
	p := ResolvePath("/app/data")
	if p != filepath.Join("/app/data", "kiro-runtime.db") {
		t.Fatalf("default path: %s", p)
	}
	t.Setenv("RUNTIME_DB_PATH", "/tmp/custom.db")
	if ResolvePath("/app/data") != "/tmp/custom.db" {
		t.Fatal("env override failed")
	}
}
