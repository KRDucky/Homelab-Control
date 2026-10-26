// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package state

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"time"

	"homelab-control/backend/internal/audit"
)

// AuditStore holds the hash-chained API audit trail (audit.Store).
type AuditStore struct{ db *DB }

// Audit returns the audit store backed by this database.
func (d *DB) Audit() *AuditStore { return &AuditStore{db: d} }

var _ audit.Store = (*AuditStore)(nil)

// AuditRecord is an audit event with its row id (stable paging cursor).
type AuditRecord struct {
	ID int64 `json:"id"`
	audit.Event
}

const auditCols = `id, seq, time, event, user, remote, outcome, detail, prev, hash`

func scanAudit(sc interface{ Scan(...any) error }) (AuditRecord, error) {
	var r AuditRecord
	var t string
	var seq int64
	err := sc.Scan(&r.ID, &seq, &t, &r.Event.Event, &r.User, &r.Remote, &r.Outcome, &r.Detail, &r.Prev, &r.Hash)
	if err != nil {
		return r, err
	}
	r.Seq = uint64(seq)
	r.Time, err = time.Parse(time.RFC3339Nano, t)
	return r, err
}

// Last returns the most recent record, or nil.
func (a *AuditStore) Last() (*audit.Event, error) {
	r, err := scanAudit(a.db.sql.QueryRow(`SELECT ` + auditCols + ` FROM audit_events ORDER BY id DESC LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r.Event, nil
}

// Append inserts a record (committed with synchronous=FULL before returning).
func (a *AuditStore) Append(e audit.Event) error {
	if len(e.Data) > 0 {
		return errors.New("the API audit store does not keep structured data")
	}
	_, err := a.db.sql.Exec(`INSERT INTO audit_events(seq, time, event, user, remote, outcome, detail, prev, hash) VALUES (?,?,?,?,?,?,?,?,?)`,
		int64(e.Seq), e.Time.UTC().Format(time.RFC3339Nano), e.Event, e.User, e.Remote, e.Outcome, e.Detail, e.Prev, e.Hash)
	return err
}

// Page returns up to limit records with id < before (before <= 0: newest),
// newest first, optionally only those whose event starts with prefix.
func (a *AuditStore) Page(before int64, limit int, prefix string) ([]AuditRecord, error) {
	if before <= 0 {
		before = 1<<62 - 1
	}
	q := `SELECT ` + auditCols + ` FROM audit_events WHERE id < ?`
	args := []any{before}
	if prefix != "" {
		q += ` AND substr(event, 1, ?) = ?`
		args = append(args, len(prefix), prefix)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := a.db.sql.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditRecord{}
	for rows.Next() {
		r, err := scanAudit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Export writes every record, oldest first, as JSON Lines in the same format
// as the file-based audit logs, so audit.Verify and external tools apply.
func (a *AuditStore) Export(w io.Writer) error {
	rows, err := a.db.sql.Query(`SELECT ` + auditCols + ` FROM audit_events ORDER BY id ASC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	enc := json.NewEncoder(w)
	for rows.Next() {
		r, err := scanAudit(rows)
		if err != nil {
			return err
		}
		if err := enc.Encode(r.Event); err != nil {
			return err
		}
	}
	return rows.Err()
}
