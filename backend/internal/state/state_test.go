// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package state

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/auth"
)

func dir(t *testing.T) string {
	t.Helper()
	d := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(d, 0o700); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestOpenCreatesPrivateFileAndMigrates(t *testing.T) {
	p := filepath.Join(dir(t), "state.db")
	d, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	fi, err := os.Stat(p)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", fi.Mode(), err)
	}
	if v, _ := d.Version(context.Background()); v != SchemaVersion() {
		t.Fatalf("version %d", v)
	}
	var mode, sync string
	_ = d.sql.QueryRow(`PRAGMA journal_mode`).Scan(&mode)
	_ = d.sql.QueryRow(`PRAGMA synchronous`).Scan(&sync)
	if mode != "wal" || sync != "2" { // 2 = FULL
		t.Fatalf("journal_mode=%s synchronous=%s", mode, sync)
	}
	if d.Backup != "" {
		t.Fatal("backup written for a new database")
	}
}

func TestOpenRefusesUnsafePaths(t *testing.T) {
	d := dir(t)
	// Group/world-accessible directory.
	loose := filepath.Join(t.TempDir(), "loose")
	_ = os.Mkdir(loose, 0o755)
	if _, err := Open(filepath.Join(loose, "s.db")); err == nil || !strings.Contains(err.Error(), "0700") {
		t.Fatalf("loose dir accepted: %v", err)
	}
	// Symlinked database file.
	target := filepath.Join(d, "real.db")
	_ = os.WriteFile(target, nil, 0o600)
	link := filepath.Join(d, "link.db")
	_ = os.Symlink(target, link)
	if _, err := Open(link); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink accepted: %v", err)
	}
	// Symlinked directory.
	ldir := filepath.Join(t.TempDir(), "ldir")
	_ = os.Symlink(d, ldir)
	if _, err := Open(filepath.Join(ldir, "s.db")); err == nil {
		t.Fatal("symlinked directory accepted")
	}
	// World-readable file.
	wr := filepath.Join(d, "wr.db")
	_ = os.WriteFile(wr, nil, 0o644)
	if _, err := Open(wr); err == nil || !strings.Contains(err.Error(), "0600") {
		t.Fatalf("0644 file accepted: %v", err)
	}
	// Planted WAL symlink.
	p := filepath.Join(d, "w.db")
	_ = os.Symlink("/etc/passwd", p+"-wal")
	if _, err := Open(p); err == nil {
		t.Fatal("symlinked -wal accepted")
	}
}

func TestMigrationFailureRollsBackAndRefuses(t *testing.T) {
	p := filepath.Join(dir(t), "state.db")
	failMigration = 1
	defer func() { failMigration = 0 }()
	if _, err := Open(p); err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("want failure, got %v", err)
	}
	failMigration = 0
	d, err := Open(p) // a clean retry applies the migration from scratch
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var n int
	_ = d.sql.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&n)
	if n != len(migrations) {
		t.Fatalf("migrations recorded: %d", n)
	}
}

func TestMigrationBackupAndDowngradeRefusal(t *testing.T) {
	p := filepath.Join(dir(t), "state.db")
	d, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	_ = d.Sessions().Insert(auth.Session{Key: "k", Username: "u", Scopes: []string{"system.read"}, CSRFToken: "c", Created: time.Now(), LastSeen: time.Now()})
	d.Close()

	// Pretend a newer migration exists: the existing database is backed up first.
	migrations = append(migrations, migration{99, "test", []string{`CREATE TABLE t99 (x INTEGER)`}})
	d, err = Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if d.Backup == "" {
		t.Fatal("no backup before migrating")
	}
	fi, err := os.Stat(d.Backup)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("backup %v %v", fi, err)
	}
	b, err := OpenReadOnly(d.Backup)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := b.Version(context.Background()); v != 1 {
		t.Fatalf("backup version %d", v)
	}
	if _, ok, _ := b.Sessions().Get("k"); !ok {
		t.Fatal("backup lacks data")
	}
	b.Close()
	d.Close()

	// The real binary only knows version 1: a version-99 database is refused.
	migrations = migrations[:len(migrations)-1]
	if _, err := Open(p); err == nil || !strings.Contains(err.Error(), "newer than this release") {
		t.Fatalf("downgrade not refused: %v", err)
	}
}

func TestSessionsPersistAcrossReopen(t *testing.T) {
	p := filepath.Join(dir(t), "state.db")
	d, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	m := auth.NewSessionManagerWith(d.Sessions())
	id, s, err := m.Create(auth.User{Username: "admin", Scopes: []string{"system.read", "services.logs"}}, "192.0.2.1", "Firefox")
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
	raw, _ := os.ReadFile(p)
	wal, _ := os.ReadFile(p + "-wal")
	if bytes.Contains(raw, []byte(id)) || bytes.Contains(wal, []byte(id)) {
		t.Fatal("raw session ID written to disk")
	}

	d, err = Open(p) // "restart"
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	m = auth.NewSessionManagerWith(d.Sessions())
	got, ok := m.Get(id)
	if !ok || got.Username != "admin" || !got.HasScope("services.logs") || got.CSRFToken != s.CSRFToken || got.UserAgent != "Firefox" {
		t.Fatalf("session not restored: %+v %v", got, ok)
	}
	// Revocation: other users cannot revoke it; the owner can.
	if ok, _ := m.Revoke("mallory", got.Key); ok {
		t.Fatal("another user revoked the session")
	}
	if ok, _ := m.Revoke("admin", got.Key); !ok {
		t.Fatal("owner could not revoke")
	}
	if _, ok := m.Get(id); ok {
		t.Fatal("revoked session still valid")
	}
}

func TestSessionLimitsWithDatabase(t *testing.T) {
	d, err := Open(filepath.Join(dir(t), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	m := auth.NewSessionManagerWith(d.Sessions())
	m.MaxSessions = 3
	for i := 0; i < 6; i++ {
		if _, _, err := m.Create(auth.User{Username: "u"}, "ip", ""); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := d.Sessions().Count(); n != 3 {
		t.Fatalf("cap not enforced: %d", n)
	}
	// Idle expiry: a stored session older than the idle timeout is rejected.
	old := time.Now().Add(-2 * time.Hour)
	_ = d.Sessions().Insert(auth.Session{Key: auth.KeyFor("stale"), Username: "u", Scopes: []string{}, CSRFToken: "c", Created: old, LastSeen: old})
	if _, ok := m.Get("stale"); ok {
		t.Fatal("idle session accepted after restart")
	}
	if _, ok, _ := d.Sessions().Get(auth.KeyFor("stale")); ok {
		t.Fatal("expired session not deleted")
	}
}

func TestAuditChainInDatabase(t *testing.T) {
	p := filepath.Join(dir(t), "state.db")
	d, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	l, err := audit.OpenStore(d.Audit())
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range []string{"auth.login", "services.logs.read", "auth.logout"} {
		if err := l.Log(audit.Event{Event: ev, User: "admin", Outcome: "success"}); err != nil {
			t.Fatal(err)
		}
	}
	seq, head := l.Head()
	d.Close()

	d, err = Open(p)
	if err != nil {
		t.Fatal(err)
	}
	l, err = audit.OpenStore(d.Audit()) // continues the chain
	if err != nil {
		t.Fatal(err)
	}
	if s2, _ := l.Head(); s2 != seq+1 {
		t.Fatalf("chain not continued: %d after %d", s2, seq)
	}
	var buf bytes.Buffer
	if err := d.Audit().Export(&buf); err != nil {
		t.Fatal(err)
	}
	rep := audit.Verify(bytes.NewReader(buf.Bytes()), audit.Checkpoint{Seq: seq, Hash: head})
	if !rep.OK() || rep.Records != 5 || rep.Segments != 1 {
		t.Fatalf("%+v", rep)
	}
	page, err := d.Audit().Page(0, 2, "auth.")
	if err != nil || len(page) != 2 || page[0].Event.Event != "auth.logout" || page[1].Event.Event != "auth.login" {
		t.Fatalf("page %+v %v", page, err)
	}
	older, _ := d.Audit().Page(page[1].ID, 10, "")
	if len(older) != 1 || older[0].Event.Event != "audit.chain" {
		t.Fatalf("older %+v", older)
	}

	// Tampering with a row (by someone with write access to the file) is detected.
	if _, err := d.sql.Exec(`UPDATE audit_events SET outcome = 'failure' WHERE event = 'auth.login'`); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	_ = d.Audit().Export(&buf)
	if rep := audit.Verify(&buf); rep.OK() {
		t.Fatal("modified row not detected")
	}
	// Deleting a row is detected too.
	_, _ = d.sql.Exec(`UPDATE audit_events SET outcome = 'success' WHERE event = 'auth.login'`)
	_, _ = d.sql.Exec(`DELETE FROM audit_events WHERE event = 'services.logs.read'`)
	buf.Reset()
	_ = d.Audit().Export(&buf)
	if rep := audit.Verify(&buf); rep.OK() {
		t.Fatal("deleted row not detected")
	}
	// A tampered last row is not extended: reopening starts a new segment.
	_, _ = d.sql.Exec(`UPDATE audit_events SET detail = 'x' WHERE id = (SELECT max(id) FROM audit_events)`)
	if _, err := audit.OpenStore(d.Audit()); err != nil {
		t.Fatal(err)
	}
	last, _ := d.Audit().Last()
	if last.Event != "audit.chain" || last.Seq != 1 || !strings.Contains(last.Detail, "does not verify") {
		t.Fatalf("%+v", last)
	}
	d.Close()
}

func TestConcurrentUse(t *testing.T) {
	d, err := Open(filepath.Join(dir(t), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	l, err := audit.OpenStore(d.Audit())
	if err != nil {
		t.Fatal(err)
	}
	m := auth.NewSessionManagerWith(d.Sessions())
	m.TouchEvery = 0
	id, _, _ := m.Create(auth.User{Username: "u"}, "", "")
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if _, ok := m.Get(id); !ok {
					t.Error("lost session")
				}
				if err := l.Log(audit.Event{Event: "x", Outcome: "ok"}); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	var buf bytes.Buffer
	_ = d.Audit().Export(&buf)
	if rep := audit.Verify(&buf); !rep.OK() || rep.Records != 321 {
		t.Fatalf("%+v", rep)
	}
}
