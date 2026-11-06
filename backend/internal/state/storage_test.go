// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package state

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestStorageSnapshotsSurviveReopenAndBoundRetention(t *testing.T) {
	path := filepath.Join(dir(t), "state.db")
	db, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Date(2026, 11, 5, 20, 0, 0, 0, time.UTC)
	ctx := context.Background()
	if _, ok, e := db.StorageSnapshots().Latest(ctx); e != nil || ok {
		t.Fatal("new store nonempty", e)
	}
	var last int64
	for i := 0; i < 8; i++ {
		last, e = db.StorageSnapshots().Save(ctx, now.Add(time.Duration(i)*time.Second), "boot-A", []byte(fmt.Sprintf(`{"nodes":[],"status":"partial","observation":%d}`, i)))
		if e != nil {
			t.Fatal(e)
		}
	}
	var count int
	if e = db.sql.QueryRow(`SELECT count(*) FROM storage_snapshots`).Scan(&count); e != nil || count != StorageSnapshotRetention {
		t.Fatal(count, e)
	}
	if e = db.Close(); e != nil {
		t.Fatal(e)
	}
	db, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	got, ok, e := db.StorageSnapshots().Latest(ctx)
	if e != nil || !ok || got.ID != last || got.BootID != "boot-A" {
		t.Fatal(got, ok, e)
	}
	if got.Stale(now.Add(10*time.Second), "boot-A", time.Minute) {
		t.Fatal("fresh observation discarded")
	}
	for _, c := range []struct {
		now  time.Time
		boot string
	}{{now.Add(time.Hour), "boot-A"}, {now, "boot-A"}, {now.Add(10 * time.Second), "boot-B"}, {now.Add(10 * time.Second), ""}} {
		if !got.Stale(c.now, c.boot, time.Minute) {
			t.Fatal("stale/reboot/future observation trusted")
		}
	}
}
func TestStorageSnapshotInvalidOrFailedSavePreservesPrevious(t *testing.T) {
	db, e := Open(filepath.Join(dir(t), "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	ctx := context.Background()
	id, e := db.StorageSnapshots().Save(ctx, time.Now(), "boot-A", []byte(`{}`))
	if e != nil {
		t.Fatal(e)
	}
	for _, data := range [][]byte{nil, []byte(`not json`), make([]byte, MaxStorageSnapshotBytes+1)} {
		if _, e = db.StorageSnapshots().Save(ctx, time.Now(), "boot-A", data); e == nil {
			t.Fatal("invalid snapshot accepted")
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = db.StorageSnapshots().Save(cancelled, time.Now(), "boot-A", []byte(`{}`)); e == nil {
		t.Fatal("cancelled write accepted")
	}
	got, ok, e := db.StorageSnapshots().Latest(ctx)
	if e != nil || !ok || got.ID != id {
		t.Fatal("previous observation lost", e)
	}
}

func TestStorageSnapshotMigrationFromVersionOnePreservesExistingState(t *testing.T) {
	original := migrations
	defer func() { migrations = original; failMigration = 0 }()
	migrations = migrations[:1]
	path := filepath.Join(dir(t), "state.db")
	db, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.sql.Exec(`INSERT INTO settings(key,value) VALUES ('existing','preserve')`); e != nil {
		t.Fatal(e)
	}
	db.Close()
	migrations = original
	failMigration = 2
	if _, e = Open(path); e == nil {
		t.Fatal("injected migration failure was ignored")
	}
	check, e := OpenReadOnly(path)
	if e != nil {
		t.Fatal(e)
	}
	if version, e := check.Version(context.Background()); e != nil || version != 1 {
		t.Fatal("failed migration changed version", version, e)
	}
	var value string
	if e = check.sql.QueryRow(`SELECT value FROM settings WHERE key='existing'`).Scan(&value); e != nil || value != "preserve" {
		t.Fatal("failed migration damaged existing state")
	}
	check.Close()
	failMigration = 0
	db, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if db.Backup == "" {
		t.Fatal("upgrade lacks recovery backup")
	}
	backups, e := filepath.Glob(path + ".bak-v1-*")
	if e != nil || len(backups) != 2 {
		t.Fatal("retry did not preserve distinct recovery backups", backups, e)
	}
	if version, e := db.Version(context.Background()); e != nil || version != 2 {
		t.Fatal(version, e)
	}
	if _, e = db.StorageSnapshots().Save(context.Background(), time.Now(), "boot-A", []byte(`{"nodes":[]}`)); e != nil {
		t.Fatal(e)
	}
}

func TestStorageDeduplicatesOnlyFreshIdenticalKnownBootObservations(t *testing.T) {
	db, e := Open(filepath.Join(dir(t), "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	ctx := context.Background()
	at := time.Now().UTC()
	graph := []byte(`{"nodes":[]}`)
	first, e := db.StorageSnapshots().Save(ctx, at, "boot-A", graph)
	if e != nil {
		t.Fatal(e)
	}
	again, e := db.StorageSnapshots().Save(ctx, at.Add(time.Second), "boot-A", graph)
	if e != nil || again != first {
		t.Fatal("identical rapid reads churn snapshots", again, e)
	}
	otherBoot, e := db.StorageSnapshots().Save(ctx, at.Add(2*time.Second), "boot-B", graph)
	if e != nil || otherBoot == first {
		t.Fatal("reused prior boot snapshot", otherBoot, e)
	}
	expired, e := db.StorageSnapshots().Save(ctx, at.Add(2*time.Minute), "boot-B", graph)
	if e != nil || expired == otherBoot {
		t.Fatal("reused expired snapshot", expired, e)
	}
}
