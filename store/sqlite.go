// Package store provides a SQLite-backed runtime store for data that should
// survive process restarts without bloating config.json:
//   - request_logs (admin Logs page)
//   - key_ip_stats (per-API-key client IP lifetime counters)
//
// Hot-path request handling must not call this package synchronously for every
// request; callers batch writes via a background flusher. Open failures are
// non-fatal: a nil *Store is safe and all methods no-op / return empty data.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	schemaVersion = 1
	driverName    = "sqlite"
)

// Store is a thin SQLite wrapper. Methods are safe for concurrent use.
// A nil receiver is treated as "disabled" (fail-open).
type Store struct {
	db   *sql.DB
	path string
	mu   sync.Mutex // serialize writes; single-writer friendliness
}

// DefaultPath returns {configDir}/kiro-runtime.db.
func DefaultPath(configDir string) string {
	if configDir == "" {
		configDir = "."
	}
	return filepath.Join(configDir, "kiro-runtime.db")
}

// ResolvePath returns RUNTIME_DB_PATH if set, otherwise DefaultPath(configDir).
func ResolvePath(configDir string) string {
	if p := os.Getenv("RUNTIME_DB_PATH"); p != "" {
		return p
	}
	return DefaultPath(configDir)
}

// Open opens (or creates) the runtime DB at path, applies pragmas and migrations.
// On any error it returns (nil, err) so callers can fail-open.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("store: empty path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("store: mkdir: %w", err)
	}

	// Ensure file is created with restricted perms when new.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		f, createErr := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
		if createErr != nil {
			return nil, fmt.Errorf("store: create: %w", createErr)
		}
		_ = f.Close()
	}

	db, err := sql.Open(driverName, path)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	// Single connection keeps SQLite simple under WAL for our write pattern.
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(time.Hour)

	s := &Store{db: db, path: path}
	if err := s.applyPragmas(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) applyPragmas() error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	}
	for _, p := range pragmas {
		if _, err := s.db.Exec(p); err != nil {
			return fmt.Errorf("store: %s: %w", p, err)
		}
	}
	return nil
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS schema_version (
  version INTEGER NOT NULL
)`); err != nil {
		return fmt.Errorf("store: schema_version table: %w", err)
	}

	var ver int
	err := s.db.QueryRow(`SELECT version FROM schema_version LIMIT 1`).Scan(&ver)
	if err == sql.ErrNoRows {
		if _, err := s.db.Exec(`INSERT INTO schema_version(version) VALUES (?)`, schemaVersion); err != nil {
			return fmt.Errorf("store: seed schema_version: %w", err)
		}
		ver = schemaVersion
	} else if err != nil {
		return fmt.Errorf("store: read schema_version: %w", err)
	}

	if ver > schemaVersion {
		return fmt.Errorf("store: database schema version %d is newer than supported %d", ver, schemaVersion)
	}

	// v1 tables (idempotent).
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS request_logs (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  ts           INTEGER NOT NULL,
  endpoint     TEXT    NOT NULL DEFAULT '',
  model        TEXT    NOT NULL DEFAULT '',
  account_id   TEXT    NOT NULL DEFAULT '',
  status       TEXT    NOT NULL DEFAULT '',
  error        TEXT    NOT NULL DEFAULT '',
  error_type   TEXT    NOT NULL DEFAULT '',
  tokens       INTEGER NOT NULL DEFAULT 0,
  credits      REAL    NOT NULL DEFAULT 0,
  duration_ms  INTEGER NOT NULL DEFAULT 0,
  client_ip    TEXT    NOT NULL DEFAULT '',
  api_key_id   TEXT    NOT NULL DEFAULT ''
)`); err != nil {
		return fmt.Errorf("store: request_logs: %w", err)
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_request_logs_ts ON request_logs(ts DESC)`); err != nil {
		return fmt.Errorf("store: idx_request_logs_ts: %w", err)
	}

	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS key_ip_stats (
  key_id     TEXT    NOT NULL,
  ip         TEXT    NOT NULL,
  requests   INTEGER NOT NULL DEFAULT 0,
  first_seen INTEGER NOT NULL,
  last_seen  INTEGER NOT NULL,
  PRIMARY KEY (key_id, ip)
)`); err != nil {
		return fmt.Errorf("store: key_ip_stats: %w", err)
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_key_ip_last ON key_ip_stats(key_id, last_seen DESC)`); err != nil {
		return fmt.Errorf("store: idx_key_ip_last: %w", err)
	}

	if ver < schemaVersion {
		if _, err := s.db.Exec(`UPDATE schema_version SET version = ?`, schemaVersion); err != nil {
			return fmt.Errorf("store: bump schema_version: %w", err)
		}
	}
	return nil
}

// Path returns the on-disk path, or "" if s is nil.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Close closes the underlying DB. Safe on nil.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.db.Close()
	s.db = nil
	return err
}
