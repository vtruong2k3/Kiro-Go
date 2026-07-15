package store

import (
	"database/sql"
	"fmt"
)

// RequestLogRow is the persisted form of an admin request log entry.
type RequestLogRow struct {
	Time      int64
	Endpoint  string
	Model     string
	AccountID string
	Status    string
	Error     string
	ErrorType string
	Tokens    int
	Credits   float64
	Duration  int64
	ClientIP  string
	ApiKeyID  string
	// Provider is the real upstream that served the request (kiro/grok/codex/...).
	// Admin-only; never exposed on the public check-key page.
	Provider string
}

// InsertRequestLogs appends rows in a single transaction. No-op on nil store or empty input.
func (s *Store) InsertRequestLogs(rows []RequestLogRow) error {
	if s == nil || s.db == nil || len(rows) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: begin insert logs: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
INSERT INTO request_logs(
  ts, endpoint, model, account_id, status, error, error_type,
  tokens, credits, duration_ms, client_ip, api_key_id, provider
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("store: prepare insert logs: %w", err)
	}
	defer stmt.Close()

	for _, r := range rows {
		if _, err := stmt.Exec(
			r.Time, r.Endpoint, r.Model, r.AccountID, r.Status, r.Error, r.ErrorType,
			r.Tokens, r.Credits, r.Duration, r.ClientIP, r.ApiKeyID, r.Provider,
		); err != nil {
			return fmt.Errorf("store: insert log: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit insert logs: %w", err)
	}
	return nil
}

// LoadRecentRequestLogs returns up to limit rows, oldest→newest (ready to seed a ring buffer).
// If limit <= 0, returns empty.
func (s *Store) LoadRecentRequestLogs(limit int) ([]RequestLogRow, error) {
	if s == nil || s.db == nil || limit <= 0 {
		return nil, nil
	}

	// Fetch newest first, then reverse for ring order.
	rows, err := s.db.Query(`
SELECT ts, endpoint, model, account_id, status, error, error_type,
       tokens, credits, duration_ms, client_ip, api_key_id, provider
FROM request_logs
ORDER BY id DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: load recent logs: %w", err)
	}
	defer rows.Close()

	tmp := make([]RequestLogRow, 0, limit)
	for rows.Next() {
		var r RequestLogRow
		if err := rows.Scan(
			&r.Time, &r.Endpoint, &r.Model, &r.AccountID, &r.Status, &r.Error, &r.ErrorType,
			&r.Tokens, &r.Credits, &r.Duration, &r.ClientIP, &r.ApiKeyID, &r.Provider,
		); err != nil {
			return nil, fmt.Errorf("store: scan log: %w", err)
		}
		tmp = append(tmp, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Reverse to oldest→newest.
	out := make([]RequestLogRow, len(tmp))
	for i := range tmp {
		out[len(tmp)-1-i] = tmp[i]
	}
	return out, nil
}

// LoadRequestLogsByApiKeyID returns up to limit rows for one API key, newest→oldest.
// Reads the full SQLite history (not just the RAM ring buffer). Empty on nil store,
// empty apiKeyID, or limit <= 0.
func (s *Store) LoadRequestLogsByApiKeyID(apiKeyID string, limit int) ([]RequestLogRow, error) {
	if s == nil || s.db == nil || apiKeyID == "" || limit <= 0 {
		return nil, nil
	}

	rows, err := s.db.Query(`
SELECT ts, endpoint, model, account_id, status, error, error_type,
       tokens, credits, duration_ms, client_ip, api_key_id, provider
FROM request_logs
WHERE api_key_id = ?
ORDER BY id DESC
LIMIT ?`, apiKeyID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: load logs by api key: %w", err)
	}
	defer rows.Close()

	out := make([]RequestLogRow, 0, limit)
	for rows.Next() {
		var r RequestLogRow
		if err := rows.Scan(
			&r.Time, &r.Endpoint, &r.Model, &r.AccountID, &r.Status, &r.Error, &r.ErrorType,
			&r.Tokens, &r.Credits, &r.Duration, &r.ClientIP, &r.ApiKeyID, &r.Provider,
		); err != nil {
			return nil, fmt.Errorf("store: scan log by api key: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ClearRequestLogs deletes all persisted request logs.
func (s *Store) ClearRequestLogs() error {
	if s == nil || s.db == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(`DELETE FROM request_logs`); err != nil {
		return fmt.Errorf("store: clear logs: %w", err)
	}
	// Intentionally no VACUUM: not required for correctness and can fail under
	// active connections / locks depending on the SQLite driver.
	return nil
}

// PruneRequestLogs keeps only the newest maxRows. No-op if maxRows <= 0 or under cap.
func (s *Store) PruneRequestLogs(maxRows int) error {
	if s == nil || s.db == nil || maxRows <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM request_logs`).Scan(&n); err != nil {
		return fmt.Errorf("store: count logs: %w", err)
	}
	if n <= maxRows {
		return nil
	}
	// Delete everything older than the newest maxRows rows.
	_, err := s.db.Exec(`
DELETE FROM request_logs
WHERE id < (
  SELECT id FROM request_logs ORDER BY id DESC LIMIT 1 OFFSET ?
)`, maxRows-1)
	if err != nil {
		return fmt.Errorf("store: prune logs: %w", err)
	}
	return nil
}

// CountRequestLogs returns the number of persisted log rows (0 on nil store).
func (s *Store) CountRequestLogs() (int, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM request_logs`).Scan(&n)
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	return n, nil
}
