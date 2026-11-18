// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

//! `state.db` access through SQLx. The Go `state` package owns the schema
//! and its migrations; this worker never migrates and refuses any schema
//! version other than the one it was written for.

use std::path::Path;
use std::time::Duration;

use sqlx::sqlite::{SqliteConnectOptions, SqliteJournalMode, SqliteSynchronous};
use sqlx::{Connection, Row, SqliteConnection};

use crate::record::Record;
use crate::Result;

/// The Go schema version this worker understands (`state.SchemaVersion()`).
pub const SUPPORTED_SCHEMA_VERSION: i64 = 4;

pub struct Db {
    pub conn: SqliteConnection,
    pub read_only: bool,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct PageStats {
    pub page_size: i64,
    pub page_count: i64,
    pub freelist_count: i64,
    /// 0 none, 1 full, 2 incremental.
    pub auto_vacuum: i64,
}

impl PageStats {
    /// Bytes of pages in use (the file size minus free pages): what the
    /// threshold compares against. Deleting rows lowers it at once, even
    /// though the file only shrinks after a vacuum.
    pub fn live_bytes(&self) -> i64 {
        (self.page_count - self.freelist_count).max(0) * self.page_size
    }
}

impl Db {
    /// Opens the database. Read-only opens never create or modify files:
    /// when no `-wal` and `-shm` exist (nothing is running) the file is
    /// opened `immutable`, otherwise `mode=ro` through the existing WAL.
    pub async fn open(path: &Path, read_only: bool) -> Result<Db> {
        let mut opts = SqliteConnectOptions::new()
            .filename(path)
            .create_if_missing(false)
            .busy_timeout(Duration::from_secs(5))
            .foreign_keys(true)
            .statement_cache_capacity(16);
        if read_only {
            let side =
                |s: &str| std::path::PathBuf::from(format!("{}{s}", path.display())).exists();
            opts = opts.read_only(true);
            if !side("-wal") && !side("-shm") {
                opts = opts.immutable(true);
            }
        } else {
            // hc-api already put the database in WAL mode; FULL keeps every
            // committed prune durable across power loss.
            opts = opts
                .journal_mode(SqliteJournalMode::Wal)
                .synchronous(SqliteSynchronous::Full);
        }
        let conn = SqliteConnection::connect_with(&opts)
            .await
            .map_err(|e| format!("open {}: {e}", path.display()))?;
        Ok(Db { conn, read_only })
    }

    /// Refuses anything but the supported schema (unknown, newer, older, or
    /// a database without the schema table).
    pub async fn check_schema(&mut self) -> Result<i64> {
        let has: i64 = sqlx::query_scalar("SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('schema_migrations','audit_events')")
            .fetch_one(&mut self.conn)
            .await?;
        if has != 2 {
            return Err("state database has no schema_migrations/audit_events table; refusing (start hc-api once to create it)".into());
        }
        let v: Option<i64> = sqlx::query_scalar("SELECT max(version) FROM schema_migrations")
            .fetch_one(&mut self.conn)
            .await?;
        match v {
            Some(SUPPORTED_SCHEMA_VERSION) => Ok(SUPPORTED_SCHEMA_VERSION),
            other => Err(format!(
                "state database schema version {} is not the supported version {SUPPORTED_SCHEMA_VERSION}; refusing (update hc-audit-retention together with hc-api)",
                other.map(|v| v.to_string()).unwrap_or_else(|| "none".into())
            )
            .into()),
        }
    }

    pub async fn page_stats(&mut self) -> Result<PageStats> {
        let page_size: i64 = sqlx::query_scalar("PRAGMA page_size")
            .fetch_one(&mut self.conn)
            .await?;
        let page_count: i64 = sqlx::query_scalar("PRAGMA page_count")
            .fetch_one(&mut self.conn)
            .await?;
        let freelist_count: i64 = sqlx::query_scalar("PRAGMA freelist_count")
            .fetch_one(&mut self.conn)
            .await?;
        let auto_vacuum: i64 = sqlx::query_scalar("PRAGMA auto_vacuum")
            .fetch_one(&mut self.conn)
            .await?;
        Ok(PageStats {
            page_size,
            page_count,
            freelist_count,
            auto_vacuum,
        })
    }

    /// (count, min id, max id) of `audit_events`.
    pub async fn extent(&mut self) -> Result<(i64, i64, i64)> {
        let row = sqlx::query(
            "SELECT count(*), COALESCE(min(id),0), COALESCE(max(id),0) FROM audit_events",
        )
        .fetch_one(&mut self.conn)
        .await?;
        Ok((row.try_get(0)?, row.try_get(1)?, row.try_get(2)?))
    }

    /// Records with `after < id <= upto`, oldest first, at most `limit`.
    pub async fn fetch(&mut self, after: i64, upto: i64, limit: i64) -> Result<Vec<Record>> {
        let rows = sqlx::query(
            "SELECT id, seq, time, event, user, remote, outcome, detail, prev, hash FROM audit_events WHERE id > ? AND id <= ? ORDER BY id LIMIT ?",
        )
        .bind(after)
        .bind(upto)
        .bind(limit)
        .fetch_all(&mut self.conn)
        .await?;
        rows.into_iter().map(row_record).collect()
    }

    /// Estimated stored bytes per row (`after < id <= upto`), oldest first.
    pub async fn row_sizes(
        &mut self,
        after: i64,
        upto: i64,
        limit: i64,
    ) -> Result<Vec<(i64, i64)>> {
        let rows = sqlx::query(
            "SELECT id, 32 + length(time) + length(event) + length(user) + length(remote) + length(outcome) + length(detail) + length(prev) + length(hash) \
             FROM audit_events WHERE id > ? AND id <= ? ORDER BY id LIMIT ?",
        )
        .bind(after)
        .bind(upto)
        .bind(limit)
        .fetch_all(&mut self.conn)
        .await?;
        rows.into_iter()
            .map(|r| Ok((r.try_get::<i64, _>(0)?, r.try_get::<i64, _>(1)?)))
            .collect()
    }

    /// Deletes exactly the archived rows, or nothing. In one immediate
    /// transaction it checks that the rows with `id <= to_id` are exactly
    /// `count` rows from `from_id` to `to_id`, that the first and last hashes
    /// are the archived ones, and that a newer row remains (the newest record
    /// is never pruned, so hc-api always continues the chain). Returns the
    /// number of rows deleted (0 when they were already gone).
    pub async fn guarded_delete(
        &mut self,
        from_id: i64,
        to_id: i64,
        count: u64,
        first_hash: &str,
        last_hash: &str,
    ) -> Result<u64> {
        let mut tx = self.conn.begin_with("BEGIN IMMEDIATE").await?;
        let row = sqlx::query("SELECT count(*), COALESCE(min(id),0), COALESCE(max(id),0) FROM audit_events WHERE id <= ?").bind(to_id).fetch_one(&mut *tx).await?;
        let (n, lo, hi): (i64, i64, i64) = (row.try_get(0)?, row.try_get(1)?, row.try_get(2)?);
        if n == 0 {
            tx.rollback().await?;
            return Ok(0);
        }
        if n as u64 != count || lo != from_id || hi != to_id {
            tx.rollback().await?;
            return Err(format!("prune guard: rows id <= {to_id} are {n} rows {lo}..{hi}, expected exactly {count} rows {from_id}..{to_id}; nothing deleted").into());
        }
        let h = |id: i64| {
            sqlx::query_scalar::<_, String>("SELECT hash FROM audit_events WHERE id = ?").bind(id)
        };
        let fh = h(from_id).fetch_one(&mut *tx).await?;
        let lh = h(to_id).fetch_one(&mut *tx).await?;
        if fh != first_hash || lh != last_hash {
            tx.rollback().await?;
            return Err(
                "prune guard: first/last row hashes differ from the archive; nothing deleted"
                    .into(),
            );
        }
        let newer: i64 = sqlx::query_scalar("SELECT count(*) FROM audit_events WHERE id > ?")
            .bind(to_id)
            .fetch_one(&mut *tx)
            .await?;
        if newer == 0 {
            tx.rollback().await?;
            return Err(
                "prune guard: refusing to prune the newest audit record; nothing deleted".into(),
            );
        }
        let done = sqlx::query("DELETE FROM audit_events WHERE id <= ?")
            .bind(to_id)
            .execute(&mut *tx)
            .await?
            .rows_affected();
        if done != count {
            tx.rollback().await?;
            return Err(format!(
                "prune guard: DELETE affected {done} rows, expected {count}; rolled back"
            )
            .into());
        }
        tx.commit().await?;
        Ok(done)
    }

    /// Returns free pages to the filesystem only where SQLite can do so
    /// incrementally (`auto_vacuum=INCREMENTAL`), in short bounded steps.
    /// Never runs a full VACUUM (exclusive, ~2x space, blocks hc-api).
    pub async fn reclaim(&mut self, max_steps: u32) -> Result<bool> {
        let st = self.page_stats().await?;
        if st.auto_vacuum != 2 {
            return Ok(false);
        }
        for _ in 0..max_steps {
            if self.page_stats().await?.freelist_count == 0 {
                break;
            }
            sqlx::query("PRAGMA incremental_vacuum(256)")
                .execute(&mut self.conn)
                .await?;
        }
        Ok(true)
    }

    pub async fn close(self) -> Result<()> {
        self.conn.close().await?;
        Ok(())
    }
}

fn row_record(r: sqlx::sqlite::SqliteRow) -> Result<Record> {
    let seq: i64 = r.try_get("seq")?;
    if seq < 0 {
        return Err(format!(
            "audit_events id {}: negative seq",
            r.try_get::<i64, _>("id")?
        )
        .into());
    }
    Ok(Record {
        id: r.try_get("id")?,
        seq: seq as u64,
        time: r.try_get("time")?,
        event: r.try_get("event")?,
        user: r.try_get("user")?,
        remote: r.try_get("remote")?,
        outcome: r.try_get("outcome")?,
        detail: r.try_get("detail")?,
        data: None,
        prev: r.try_get("prev")?,
        hash: r.try_get("hash")?,
    })
}
