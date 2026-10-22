// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package state is hc-api's durable, non-secret local state: persistent
// sessions (hashes only) and the API audit trail. It is a single SQLite file
// (modernc.org/sqlite, pure Go; ADR 0006 amendment 1, owner decision D-1).
//
// Safety rules:
//   - the database directory must be owned by the current user and not be
//     group/world-accessible; the file must be a regular file (no symlinks)
//     with mode 0600;
//   - schema changes are forward-only migrations, each in one transaction,
//     recorded in schema_migrations; before migrating an existing database a
//     consistent copy is written next to it (VACUUM INTO), and a database newer
//     than this binary is refused (no silent downgrade);
//   - synchronous=FULL, so a committed audit record survives power loss.
package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// DB is an open state database.
type DB struct {
	sql    *sql.DB
	path   string
	Backup string // path of the pre-migration backup written by Open, if any
}

type migration struct {
	version int
	name    string
	stmts   []string
}

// migrations are append-only: never edit a released migration.
var migrations = []migration{
	{1, "sessions, audit_events, settings", []string{
		`CREATE TABLE sessions (
			id_hash     TEXT PRIMARY KEY,
			username    TEXT NOT NULL,
			scopes      TEXT NOT NULL,
			csrf_token  TEXT NOT NULL,
			created     INTEGER NOT NULL,
			last_seen   INTEGER NOT NULL,
			remote_ip   TEXT NOT NULL DEFAULT '',
			user_agent  TEXT NOT NULL DEFAULT ''
		) STRICT`,
		`CREATE INDEX sessions_user ON sessions(username)`,
		`CREATE TABLE audit_events (
			id       INTEGER PRIMARY KEY AUTOINCREMENT,
			seq      INTEGER NOT NULL,
			time     TEXT NOT NULL,
			event    TEXT NOT NULL,
			user     TEXT NOT NULL DEFAULT '',
			remote   TEXT NOT NULL DEFAULT '',
			outcome  TEXT NOT NULL,
			detail   TEXT NOT NULL DEFAULT '',
			prev     TEXT NOT NULL,
			hash     TEXT NOT NULL
		) STRICT`,
		`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL) STRICT`,
	}},
}

// SchemaVersion is the newest schema this binary understands.
func SchemaVersion() int { return migrations[len(migrations)-1].version }

// checkDir refuses a database directory that others could tamper with.
func checkDir(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s: not a directory (symlinks are refused)", dir)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s: mode %04o is group/world-accessible; expected 0700", dir, fi.Mode().Perm())
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Geteuid() {
		return fmt.Errorf("%s: owned by uid %d, not the service user (uid %d)", dir, st.Uid, os.Geteuid())
	}
	return nil
}

// checkFile refuses a symlinked, non-regular or loosely permissioned file.
// A missing file is fine (it will be created 0600).
func checkFile(p string) error {
	fi, err := os.Lstat(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s: not a regular file (symlinks are refused)", p)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s: mode %04o is group/world-accessible; expected 0600", p, fi.Mode().Perm())
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Geteuid() {
		return fmt.Errorf("%s: owned by uid %d, not the service user", p, st.Uid)
	}
	return nil
}

// Open opens (creating if needed) and migrates the database at path.
func Open(path string) (*DB, error) {
	return open(path, false)
}

// OpenReadOnly opens an existing database without migrating or writing
// (hlctl audit export/verify). Ownership is not required to match, so root
// can inspect the service's database, but symlinks are still refused.
func OpenReadOnly(path string) (*DB, error) {
	return open(path, true)
}

func open(path string, ro bool) (*DB, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if ro {
		fi, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !fi.Mode().IsRegular() {
			return nil, fmt.Errorf("%s: not a regular file (symlinks are refused)", path)
		}
	} else {
		if err := checkDir(filepath.Dir(path)); err != nil {
			return nil, err
		}
		if err := checkFile(path); err != nil {
			return nil, err
		}
		for _, side := range []string{"-wal", "-shm", "-journal"} {
			if err := checkFile(path + side); err != nil {
				return nil, err
			}
		}
		// Create the file 0600 ourselves so SQLite never creates it with the umask.
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
		if err != nil {
			return nil, err
		}
		f.Close()
	}
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_txlock", "immediate")
	if ro {
		q.Add("mode", "ro")
	} else {
		q.Add("_pragma", "journal_mode(WAL)")
		q.Add("_pragma", "synchronous(FULL)")
	}
	dsn := "file:" + (&url.URL{Path: path}).EscapedPath() + "?" + q.Encode()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if !ro {
		// One writer connection: SQLite serialises writers anyway, and this
		// keeps the audit chain and session updates strictly ordered.
		db.SetMaxOpenConns(1)
	}
	d := &DB{sql: db, path: path}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if ro {
		return d, nil
	}
	if err := d.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return d, nil
}

// Close closes the database.
func (d *DB) Close() error { return d.sql.Close() }

// Path returns the database path.
func (d *DB) Path() string { return d.path }

// Version returns the applied schema version (0 = empty database).
func (d *DB) Version(ctx context.Context) (int, error) {
	var exists int
	if err := d.sql.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'`).Scan(&exists); err != nil {
		return 0, err
	}
	if exists == 0 {
		return 0, nil
	}
	var v sql.NullInt64
	if err := d.sql.QueryRowContext(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&v); err != nil {
		return 0, err
	}
	return int(v.Int64), nil
}

// failMigration is a test hook: when set, migration N fails after its
// statements ran (inside the transaction).
var failMigration = 0

func (d *DB) migrate(ctx context.Context) error {
	cur, err := d.Version(ctx)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if cur > SchemaVersion() {
		return fmt.Errorf("%s has schema version %d, newer than this release supports (%d); refusing to start (downgrade?)", d.path, cur, SchemaVersion())
	}
	if cur == SchemaVersion() {
		return nil
	}
	if cur > 0 {
		// Consistent copy of the existing database before any change.
		b := fmt.Sprintf("%s.bak-v%d-%s", d.path, cur, time.Now().UTC().Format("20060102T150405Z"))
		if _, err := d.sql.ExecContext(ctx, `VACUUM INTO ?`, b); err != nil {
			return fmt.Errorf("backup before migration: %w", err)
		}
		if err := os.Chmod(b, 0o600); err != nil {
			return err
		}
		d.Backup = b
	}
	if _, err := d.sql.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL) STRICT`); err != nil {
		return err
	}
	for _, m := range migrations {
		if m.version <= cur {
			continue
		}
		if err := d.apply(ctx, m); err != nil {
			return fmt.Errorf("migration %d (%s) failed and was rolled back: %w", m.version, m.name, err)
		}
	}
	return nil
}

func (d *DB) apply(ctx context.Context, m migration) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	for _, s := range m.stmts {
		if _, err := tx.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, name, applied_at) VALUES (?, ?, ?)`,
		m.version, m.name, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	if failMigration == m.version {
		return errors.New("injected failure (test)")
	}
	return tx.Commit()
}
