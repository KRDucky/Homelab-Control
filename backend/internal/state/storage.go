// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package state

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const MaxStorageSnapshotBytes = 8 * 1024 * 1024
const StorageSnapshotRetention = 4

// StorageSnapshot is historical evidence, never a current target authorization.
// Identifier-bearing graphs remain inside the existing private state database.
type StorageSnapshot struct {
	ID          int64
	CollectedAt time.Time
	BootID      string
	Graph       []byte
}

// Stale rejects unknown/different boots, future dates and expired observations.
func (s StorageSnapshot) Stale(now time.Time, currentBootID string, maxAge time.Duration) bool {
	age := now.Sub(s.CollectedAt)
	return currentBootID == "" || s.BootID == "" || currentBootID != s.BootID || maxAge <= 0 || age < 0 || age > maxAge
}

type StorageSnapshotStore struct{ db *DB }

func (d *DB) StorageSnapshots() *StorageSnapshotStore { return &StorageSnapshotStore{db: d} }

// Save atomically appends and bounds retention; no prior snapshot is returned
// as fresh when collection fails. Only application-owned state is changed.
func (s *StorageSnapshotStore) Save(ctx context.Context, at time.Time, bootID string, graph []byte) (int64, error) {
	if at.IsZero() || len(bootID) > 128 || len(graph) == 0 || len(graph) > MaxStorageSnapshotBytes || !json.Valid(graph) {
		return 0, fmt.Errorf("invalid or oversized storage snapshot")
	}
	tx, e := s.db.sql.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	// Repeated read requests must not churn identical graphs into the WAL.
	// Only deduplicate after a fresh collection, on the same known boot, and
	// for at most one minute. The existing record remains historical evidence.
	var previousID int64
	var previousAt, previousBoot string
	var previousGraph []byte
	previousError := tx.QueryRowContext(ctx, `SELECT id,collected_at,boot_id,graph FROM storage_snapshots ORDER BY id DESC LIMIT 1`).Scan(&previousID, &previousAt, &previousBoot, &previousGraph)
	if previousError != nil && !errors.Is(previousError, sql.ErrNoRows) {
		return 0, previousError
	}
	if previousError == nil && bootID != "" && bootID == previousBoot && bytes.Equal(graph, previousGraph) {
		stamp, err := time.Parse(time.RFC3339Nano, previousAt)
		if err != nil {
			return 0, fmt.Errorf("invalid stored storage observation time")
		}
		age := at.Sub(stamp)
		if age >= 0 && age < time.Minute {
			return previousID, nil
		}
	}
	result, e := tx.ExecContext(ctx, `INSERT INTO storage_snapshots(collected_at,boot_id,graph) VALUES (?,?,?)`, at.UTC().Format(time.RFC3339Nano), bootID, graph)
	if e != nil {
		return 0, e
	}
	id, e := result.LastInsertId()
	if e != nil {
		return 0, e
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM storage_snapshots WHERE id NOT IN (SELECT id FROM storage_snapshots ORDER BY id DESC LIMIT ?)`, StorageSnapshotRetention); e != nil {
		return 0, e
	}
	if e = tx.Commit(); e != nil {
		return 0, e
	}
	return id, nil
}
func (s *StorageSnapshotStore) Latest(ctx context.Context) (StorageSnapshot, bool, error) {
	var out StorageSnapshot
	var stamp string
	e := s.db.sql.QueryRowContext(ctx, `SELECT id,collected_at,boot_id,graph FROM storage_snapshots ORDER BY id DESC LIMIT 1`).Scan(&out.ID, &stamp, &out.BootID, &out.Graph)
	if errors.Is(e, sql.ErrNoRows) {
		return out, false, nil
	}
	if e != nil {
		return out, false, e
	}
	out.CollectedAt, e = time.Parse(time.RFC3339Nano, stamp)
	if e != nil {
		return StorageSnapshot{}, false, fmt.Errorf("invalid stored storage observation time")
	}
	if len(out.Graph) > MaxStorageSnapshotBytes || !json.Valid(out.Graph) {
		return StorageSnapshot{}, false, fmt.Errorf("invalid stored storage graph")
	}
	return out, true, nil
}
