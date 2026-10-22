// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package state

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"homelab-control/backend/internal/auth"
)

// Sessions is the persistent auth.SessionStore (owner decision D-6). Only
// SHA-256(session ID) is stored; the raw cookie value never reaches disk.
type Sessions struct{ db *DB }

// Sessions returns the session store backed by this database.
func (d *DB) Sessions() *Sessions { return &Sessions{db: d} }

var _ auth.SessionStore = (*Sessions)(nil)

const sessionCols = `id_hash, username, scopes, csrf_token, created, last_seen, remote_ip, user_agent`

func scanSession(sc interface{ Scan(...any) error }) (auth.Session, error) {
	var s auth.Session
	var scopes string
	var created, seen int64
	if err := sc.Scan(&s.Key, &s.Username, &scopes, &s.CSRFToken, &created, &seen, &s.RemoteIP, &s.UserAgent); err != nil {
		return s, err
	}
	if err := json.Unmarshal([]byte(scopes), &s.Scopes); err != nil {
		return s, err
	}
	s.Created, s.LastSeen = time.UnixMicro(created).UTC(), time.UnixMicro(seen).UTC()
	return s, nil
}

// Insert stores a new session.
func (s *Sessions) Insert(x auth.Session) error {
	sc, err := json.Marshal(x.Scopes)
	if err != nil {
		return err
	}
	_, err = s.db.sql.Exec(`INSERT INTO sessions(`+sessionCols+`) VALUES (?,?,?,?,?,?,?,?)`,
		x.Key, x.Username, string(sc), x.CSRFToken, x.Created.UnixMicro(), x.LastSeen.UnixMicro(), x.RemoteIP, x.UserAgent)
	return err
}

// Get returns a session by key.
func (s *Sessions) Get(key string) (auth.Session, bool, error) {
	x, err := scanSession(s.db.sql.QueryRow(`SELECT `+sessionCols+` FROM sessions WHERE id_hash = ?`, key))
	if errors.Is(err, sql.ErrNoRows) {
		return x, false, nil
	}
	return x, err == nil, err
}

// Touch refreshes last-seen.
func (s *Sessions) Touch(key string, t time.Time) error {
	_, err := s.db.sql.Exec(`UPDATE sessions SET last_seen = ? WHERE id_hash = ?`, t.UnixMicro(), key)
	return err
}

// Delete removes one session.
func (s *Sessions) Delete(key string) error {
	_, err := s.db.sql.Exec(`DELETE FROM sessions WHERE id_hash = ?`, key)
	return err
}

// DeleteUser removes all of a user's sessions.
func (s *Sessions) DeleteUser(u string) error {
	_, err := s.db.sql.Exec(`DELETE FROM sessions WHERE username = ?`, u)
	return err
}

// ListUser returns a user's sessions.
func (s *Sessions) ListUser(u string) ([]auth.Session, error) {
	rows, err := s.db.sql.Query(`SELECT `+sessionCols+` FROM sessions WHERE username = ? ORDER BY last_seen DESC`, u)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []auth.Session
	for rows.Next() {
		x, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// Expire deletes idle or too-old sessions.
func (s *Sessions) Expire(idleBefore, createdBefore time.Time) error {
	_, err := s.db.sql.Exec(`DELETE FROM sessions WHERE last_seen < ? OR created < ?`, idleBefore.UnixMicro(), createdBefore.UnixMicro())
	return err
}

// Count returns the number of stored sessions.
func (s *Sessions) Count() (int, error) {
	var n int
	err := s.db.sql.QueryRow(`SELECT count(*) FROM sessions`).Scan(&n)
	return n, err
}

// DeleteOldest removes the least recently used session.
func (s *Sessions) DeleteOldest() error {
	_, err := s.db.sql.Exec(`DELETE FROM sessions WHERE id_hash = (SELECT id_hash FROM sessions ORDER BY last_seen ASC LIMIT 1)`)
	return err
}
