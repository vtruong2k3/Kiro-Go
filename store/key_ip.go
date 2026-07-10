package store

import (
	"fmt"
)

// KeyIPRow is one lifetime IP usage row for an API key.
type KeyIPRow struct {
	KeyID     string
	IP        string
	Requests  int64
	FirstSeen int64
	LastSeen  int64
}

// LoadKeyIPStats returns keyID -> ip -> row for all persisted stats.
func (s *Store) LoadKeyIPStats() (map[string]map[string]KeyIPRow, error) {
	if s == nil || s.db == nil {
		return map[string]map[string]KeyIPRow{}, nil
	}
	rows, err := s.db.Query(`
SELECT key_id, ip, requests, first_seen, last_seen
FROM key_ip_stats`)
	if err != nil {
		return nil, fmt.Errorf("store: load key_ip_stats: %w", err)
	}
	defer rows.Close()

	out := make(map[string]map[string]KeyIPRow)
	for rows.Next() {
		var r KeyIPRow
		if err := rows.Scan(&r.KeyID, &r.IP, &r.Requests, &r.FirstSeen, &r.LastSeen); err != nil {
			return nil, fmt.Errorf("store: scan key_ip: %w", err)
		}
		m := out[r.KeyID]
		if m == nil {
			m = make(map[string]KeyIPRow)
			out[r.KeyID] = m
		}
		m[r.IP] = r
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// UpsertKeyIPStats writes the given rows. first_seen is preserved as MIN on conflict.
func (s *Store) UpsertKeyIPStats(rows []KeyIPRow) error {
	if s == nil || s.db == nil || len(rows) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: begin upsert key_ip: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
INSERT INTO key_ip_stats(key_id, ip, requests, first_seen, last_seen)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(key_id, ip) DO UPDATE SET
  requests   = excluded.requests,
  last_seen  = excluded.last_seen,
  first_seen = CASE
    WHEN key_ip_stats.first_seen = 0 OR key_ip_stats.first_seen > excluded.first_seen
    THEN excluded.first_seen
    ELSE key_ip_stats.first_seen
  END`)
	if err != nil {
		return fmt.Errorf("store: prepare upsert key_ip: %w", err)
	}
	defer stmt.Close()

	for _, r := range rows {
		if r.KeyID == "" || r.IP == "" {
			continue
		}
		if _, err := stmt.Exec(r.KeyID, r.IP, r.Requests, r.FirstSeen, r.LastSeen); err != nil {
			return fmt.Errorf("store: upsert key_ip: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit upsert key_ip: %w", err)
	}
	return nil
}

// ReplaceKeyIPStats replaces all IP rows for keyID with the provided set
// (used after eviction so orphan IPs are removed from DB).
func (s *Store) ReplaceKeyIPStats(keyID string, rows []KeyIPRow) error {
	if s == nil || s.db == nil || keyID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: begin replace key_ip: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM key_ip_stats WHERE key_id = ?`, keyID); err != nil {
		return fmt.Errorf("store: delete key_ip for replace: %w", err)
	}
	if len(rows) > 0 {
		stmt, err := tx.Prepare(`
INSERT INTO key_ip_stats(key_id, ip, requests, first_seen, last_seen)
VALUES (?, ?, ?, ?, ?)`)
		if err != nil {
			return fmt.Errorf("store: prepare replace insert: %w", err)
		}
		defer stmt.Close()
		for _, r := range rows {
			if r.IP == "" {
				continue
			}
			if _, err := stmt.Exec(keyID, r.IP, r.Requests, r.FirstSeen, r.LastSeen); err != nil {
				return fmt.Errorf("store: replace insert key_ip: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit replace key_ip: %w", err)
	}
	return nil
}

// DeleteKeyIPStats removes all IP rows for a deleted API key.
func (s *Store) DeleteKeyIPStats(keyID string) error {
	if s == nil || s.db == nil || keyID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(`DELETE FROM key_ip_stats WHERE key_id = ?`, keyID); err != nil {
		return fmt.Errorf("store: delete key_ip_stats: %w", err)
	}
	return nil
}
