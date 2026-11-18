// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

//! End-to-end retention tests on disposable fixture databases with the Go
//! schema (never a real state.db). Includes the reviewer regression probes
//! A–F and D2 from the review of cd05c1af.

use std::collections::BTreeMap;
use std::os::unix::fs::{MetadataExt, PermissionsExt};
use std::path::{Path, PathBuf};

use hc_audit_retention::archive;
use hc_audit_retention::config::Settings;
use hc_audit_retention::ledger::{Entry, Ledger};
use hc_audit_retention::record::{sha256_hex, Record, ZERO_HASH};
use hc_audit_retention::retention::{run, Fault, Options, Paths};
use hc_audit_retention::timeutil::{format_rfc3339, now_nanos, sub_months, NANOS_PER_DAY};
use hc_audit_retention::verify::verify;
use sqlx::sqlite::{SqliteConnectOptions, SqliteJournalMode};
use sqlx::{Connection, SqliteConnection};

fn block<F: std::future::Future>(f: F) -> F::Output {
    tokio::runtime::Builder::new_current_thread()
        .enable_all()
        .build()
        .unwrap()
        .block_on(f)
}

struct Fx {
    root: PathBuf,
    db: PathBuf,
    archive: PathBuf,
    now: i128,
    last: Option<Record>,
}

impl Drop for Fx {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.root);
    }
}

fn tmp(tag: &str) -> PathBuf {
    use std::sync::atomic::{AtomicU64, Ordering};
    static N: AtomicU64 = AtomicU64::new(0);
    let p = std::env::temp_dir().join(format!(
        "hc-ret-it-{tag}-{}-{}",
        std::process::id(),
        N.fetch_add(1, Ordering::Relaxed)
    ));
    let _ = std::fs::remove_dir_all(&p);
    std::fs::create_dir_all(&p).unwrap();
    std::fs::set_permissions(&p, std::fs::Permissions::from_mode(0o700)).unwrap();
    p
}

async fn conn(db: &Path) -> SqliteConnection {
    let o = SqliteConnectOptions::new()
        .filename(db)
        .create_if_missing(true)
        .journal_mode(SqliteJournalMode::Wal);
    SqliteConnection::connect_with(&o).await.unwrap()
}

impl Fx {
    /// A database with the current Go schema (version 4) and `ages` records (age in days, oldest first).
    fn new(tag: &str, ages: &[i128]) -> Fx {
        Self::with_schema(tag, ages, Some(4))
    }

    fn with_schema(tag: &str, ages: &[i128], version: Option<i64>) -> Fx {
        let root = tmp(tag);
        let db = root.join("state.db");
        let mut fx = Fx {
            archive: root.join("audit-archive"),
            db: db.clone(),
            root,
            now: now_nanos(),
            last: None,
        };
        block(async {
            let mut c = conn(&db).await;
            for s in [
                "CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL) STRICT",
                "CREATE TABLE audit_events (id INTEGER PRIMARY KEY AUTOINCREMENT, seq INTEGER NOT NULL, time TEXT NOT NULL, event TEXT NOT NULL, user TEXT NOT NULL DEFAULT '', remote TEXT NOT NULL DEFAULT '', outcome TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '', prev TEXT NOT NULL, hash TEXT NOT NULL) STRICT",
                "CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL) STRICT",
            ] {
                sqlx::query(s).execute(&mut c).await.unwrap();
            }
            if let Some(v) = version {
                for i in 1..=v {
                    sqlx::query(
                        "INSERT INTO schema_migrations VALUES (?, 'm', '2026-01-01T00:00:00Z')",
                    )
                    .bind(i)
                    .execute(&mut c)
                    .await
                    .unwrap();
                }
            }
            c.close().await.unwrap();
        });
        std::fs::set_permissions(&fx.db, std::fs::Permissions::from_mode(0o600)).unwrap();
        for a in ages {
            fx.add(*a);
        }
        fx
    }

    /// Appends one correctly chained record `age_days` old, as hc-api would.
    fn add(&mut self, age_days: i128) -> Record {
        self.add_at(self.now - age_days * NANOS_PER_DAY)
    }

    fn add_at(&mut self, t: i128) -> Record {
        let (seq, prev, event) = match &self.last {
            None => (1, ZERO_HASH.to_string(), "audit.chain"),
            Some(l) => (l.seq + 1, l.hash.clone(), "storage.inventory.read"),
        };
        let mut r = Record {
            id: 0,
            seq,
            time: format_rfc3339(t),
            event: event.into(),
            user: "admin".into(),
            remote: "192.0.2.1".into(),
            outcome: "ok".into(),
            detail: format!("nodes={seq} <x>&\"y\""),
            data: None,
            prev,
            hash: String::new(),
        };
        r.hash = r.digest();
        let db = self.db.clone();
        let rc = r.clone();
        let id: i64 = block(async move {
            let mut c = conn(&db).await;
            let id = sqlx::query("INSERT INTO audit_events (seq,time,event,user,remote,outcome,detail,prev,hash) VALUES (?,?,?,?,?,?,?,?,?)")
                .bind(rc.seq as i64).bind(&rc.time).bind(&rc.event).bind(&rc.user).bind(&rc.remote).bind(&rc.outcome).bind(&rc.detail).bind(&rc.prev).bind(&rc.hash)
                .execute(&mut c).await.unwrap().last_insert_rowid();
            c.close().await.unwrap();
            id
        });
        r.id = id;
        self.last = Some(r.clone());
        r
    }

    fn exec(&self, sql: &str) {
        let db = self.db.clone();
        let sql = sql.to_string();
        block(async move {
            let mut c = conn(&db).await;
            sqlx::query(&sql).execute(&mut c).await.unwrap();
            c.close().await.unwrap();
        });
    }

    fn rows(&self) -> Vec<i64> {
        let db = self.db.clone();
        block(async move {
            let mut c = conn(&db).await;
            let v: Vec<i64> = sqlx::query_scalar("SELECT id FROM audit_events ORDER BY id")
                .fetch_all(&mut c)
                .await
                .unwrap();
            c.close().await.unwrap();
            v
        })
    }

    fn opts(&self, active: u32, months: u32) -> Options {
        let s = Settings {
            active_retention_days: active,
            archive_retention_months: months,
            active_db_threshold_gib: 5,
        };
        Options::new(
            Paths {
                state_db: self.db.clone(),
                archive_dir: self.archive.clone(),
            },
            s,
            self.now,
        )
    }

    fn archives(&self) -> Vec<String> {
        let mut v: Vec<String> = std::fs::read_dir(&self.archive)
            .map(|r| {
                r.filter_map(|e| e.ok())
                    .map(|e| e.file_name().to_string_lossy().into_owned())
                    .filter(|n| archive::is_archive_name(n))
                    .collect()
            })
            .unwrap_or_default();
        v.sort();
        v
    }

    fn verify_ok(&self) {
        let r = block(verify(&Paths {
            state_db: self.db.clone(),
            archive_dir: self.archive.clone(),
        }))
        .unwrap();
        assert!(r.ok(), "verify problems: {:?}", r.problems);
    }

    fn verify_problems(&self) -> Vec<String> {
        block(verify(&Paths {
            state_db: self.db.clone(),
            archive_dir: self.archive.clone(),
        }))
        .unwrap()
        .problems
    }
}

/// Every path under `dir` with type, size, mode, mtime and content hash.
fn snapshot(dir: &Path) -> BTreeMap<String, String> {
    let mut out = BTreeMap::new();
    fn walk(base: &Path, p: &Path, out: &mut BTreeMap<String, String>) {
        for e in std::fs::read_dir(p).unwrap() {
            let e = e.unwrap();
            let path = e.path();
            let Ok(m) = std::fs::symlink_metadata(&path) else {
                continue;
            };
            let rel = path.strip_prefix(base).unwrap().display().to_string();
            let content = if m.is_file() {
                sha256_hex(&std::fs::read(&path).unwrap())
            } else {
                String::new()
            };
            out.insert(
                rel,
                format!(
                    "{} {} {:o} {} {}",
                    m.is_dir(),
                    m.len(),
                    m.mode(),
                    m.mtime_nsec() + m.mtime() * 1_000_000_000,
                    content
                ),
            );
            if m.is_dir() {
                walk(base, &path, out);
            }
        }
    }
    walk(dir, dir, &mut out);
    out
}

#[test]
fn archives_verifies_prunes_and_is_idempotent() {
    let fx = Fx::new("basic", &[100, 100, 100, 100, 100, 100, 0, 0, 0, 0]);
    let s = block(run(&fx.opts(30, 60))).unwrap();
    assert_eq!((s.archived_rows, s.pruned_rows), (6, 6));
    assert_eq!(fx.rows(), vec![7, 8, 9, 10]);
    assert_eq!(fx.archives(), vec![archive::archive_name(1, 6)]);
    fx.verify_ok();
    let s2 = block(run(&fx.opts(30, 60))).unwrap();
    assert_eq!(s2.pruned_rows, 0);
    fx.verify_ok();
    // The archive's audit.jsonl is the Go export format, and the ledger verifies as a Go audit log.
    let bytes = std::fs::read(fx.archive.join(&fx.archives()[0])).unwrap();
    let p = archive::parse(&bytes, None).unwrap();
    assert_eq!(p.records.len(), 6);
    let l = Ledger::load(&fx.archive, false).unwrap();
    assert!(l.pending_prune().is_none() && l.records.len() >= 4);
    // Archive and ledger files are private.
    for n in fx
        .archives()
        .iter()
        .chain(["ledger.jsonl".to_string()].iter())
    {
        assert_eq!(
            std::fs::metadata(fx.archive.join(n)).unwrap().mode() & 0o777,
            0o600,
            "{n}"
        );
    }
    assert_eq!(
        std::fs::metadata(&fx.archive).unwrap().mode() & 0o777,
        0o700
    );
}

#[test]
fn chunks_chain_across_archives() {
    let fx = Fx::new("chunks", &[100; 10]);
    let mut o = fx.opts(30, 60);
    o.chunk_rows = 3;
    let s = block(run(&o)).unwrap();
    assert_eq!(s.pruned_rows, 9, "the newest record is always kept");
    assert_eq!(fx.archives().len(), 3);
    assert_eq!(fx.rows(), vec![10]);
    fx.verify_ok();
}

/// Probe A (and the dry-run requirement): a dry run makes no filesystem
/// change at all, even when it would archive, prune, expire, clean up temp
/// files and repair a torn ledger line.
#[test]
fn probe_a_dry_run_makes_zero_filesystem_changes() {
    let mut fx = Fx::new("dry", &[200, 200, 200, 100, 0]);
    block(run(&fx.opts(150, 60))).unwrap();
    fx.add(0);
    std::fs::write(fx.archive.join(".tmp-leftover"), b"x").unwrap();
    std::fs::OpenOptions::new()
        .append(true)
        .open(fx.archive.join("ledger.jsonl"))
        .unwrap()
        .write_all_torn();
    let before = snapshot(&fx.root);
    let mut o = fx.opts(30, 1);
    o.dry_run = true;
    let s = block(run(&o)).unwrap();
    assert_eq!(snapshot(&fx.root), before, "dry run changed the filesystem");
    assert!(
        s.archived_rows > 0 && !s.expired.is_empty(),
        "{:?}",
        s.notes
    );
    assert!(
        s.notes.iter().any(|n| n.contains("temporary"))
            && s.notes.iter().any(|n| n.contains("torn"))
    );

    // No archive directory yet: a dry run does not create it.
    let fx2 = Fx::new("dry2", &[200, 200, 0]);
    let before = snapshot(&fx2.root);
    let mut o = fx2.opts(30, 60);
    o.dry_run = true;
    block(run(&o)).unwrap();
    assert_eq!(snapshot(&fx2.root), before);
    assert!(!fx2.archive.exists());
}

trait Torn {
    fn write_all_torn(self);
}
impl Torn for std::fs::File {
    fn write_all_torn(mut self) {
        use std::io::Write;
        self.write_all(b"{\"seq\":99,\"ti").unwrap();
    }
}

/// While hc-api has the database open (WAL and SHM present), a dry run
/// creates no file and changes neither the database nor its WAL.
#[test]
fn dry_run_with_a_live_writer_changes_no_data() {
    let fx = Fx::new("drylive", &[200, 200, 0]);
    block(async {
        let mut other = conn(&fx.db).await;
        sqlx::query("SELECT count(*) FROM audit_events")
            .execute(&mut other)
            .await
            .unwrap();
        let names = |d: &Path| {
            std::fs::read_dir(d)
                .unwrap()
                .map(|e| e.unwrap().file_name())
                .collect::<std::collections::BTreeSet<_>>()
        };
        let before_names = names(&fx.root);
        let db_before = sha256_hex(&std::fs::read(&fx.db).unwrap());
        let mut o = fx.opts(30, 60);
        o.dry_run = true;
        let s = run(&o).await.unwrap();
        assert_eq!(s.archived_rows, 2);
        assert_eq!(names(&fx.root), before_names);
        assert_eq!(sha256_hex(&std::fs::read(&fx.db).unwrap()), db_before);
        other.close().await.unwrap();
    });
}

/// Probe B and every other crash point: the next pass completes the work.
#[test]
fn probe_b_every_crash_point_recovers() {
    for f in [
        Fault::AfterArchiveFile,
        Fault::AfterArchiveRecorded,
        Fault::AfterPruneIntent,
        Fault::AfterDelete,
    ] {
        let fx = Fx::new("crash", &[100, 100, 100, 0, 0]);
        let mut o = fx.opts(30, 60);
        o.fault = Some(f);
        assert!(block(run(&o)).is_err(), "{f:?}");
        let s = block(run(&fx.opts(30, 60))).unwrap();
        assert_eq!(fx.rows(), vec![4, 5], "{f:?}: {:?}", s.notes);
        assert_eq!(fx.archives().len(), 1, "{f:?}");
        let l = Ledger::load(&fx.archive, false).unwrap();
        assert!(l.pending_prune().is_none(), "{f:?}");
        assert_eq!(
            l.entries
                .iter()
                .filter(|e| matches!(e, Entry::ArchiveCreated(_)))
                .count(),
            1,
            "{f:?}"
        );
        fx.verify_ok();
    }
    for f in [Fault::AfterExpireIntent, Fault::AfterExpireUnlink] {
        let mut fx = Fx::new("crashx", &[100, 100, 0]);
        block(run(&fx.opts(30, 60))).unwrap();
        fx.now += (6 * 365) * NANOS_PER_DAY; // six years later
        fx.add(0);
        let mut o = fx.opts(30, 60);
        o.fault = Some(f);
        assert!(block(run(&o)).is_err(), "{f:?}");
        block(run(&fx.opts(30, 60))).unwrap();
        let l = Ledger::load(&fx.archive, false).unwrap();
        assert!(l.pending_expire().is_none(), "{f:?}");
        assert!(
            !fx.archives().contains(&archive::archive_name(1, 2)),
            "{f:?}"
        );
        fx.verify_ok();
    }
}

/// A lost or replaced ledger fails safe: nothing is pruned.
#[test]
fn lost_ledger_or_missing_archive_fails_safe() {
    let mut fx = Fx::new("lost", &[100, 100, 100, 0, 0]);
    block(run(&fx.opts(30, 60))).unwrap();
    fx.add(100);
    std::fs::rename(fx.archive.join("ledger.jsonl"), fx.root.join("ledger.bak")).unwrap();
    assert!(block(run(&fx.opts(30, 60))).is_err());
    std::fs::rename(fx.root.join("ledger.bak"), fx.archive.join("ledger.jsonl")).unwrap();
    let a = fx.archives()[0].clone();
    std::fs::rename(fx.archive.join(&a), fx.root.join("a.bak")).unwrap();
    let e = block(run(&fx.opts(30, 60))).unwrap_err().to_string();
    assert!(e.contains("missing"), "{e}");
    assert_eq!(fx.rows(), vec![4, 5, 6]);
}

/// Probe C: when every record is old, the newest is kept, and the chain
/// continues cleanly when hc-api appends more.
#[test]
fn probe_c_newest_record_is_kept_and_chain_continues() {
    let mut fx = Fx::new("probec", &[100, 100, 100]);
    let s = block(run(&fx.opts(30, 60))).unwrap();
    assert_eq!(s.pruned_rows, 2);
    assert_eq!(fx.rows(), vec![3]);
    fx.add(100);
    fx.add(0);
    let s = block(run(&fx.opts(30, 60))).unwrap();
    assert_eq!(s.pruned_rows, 2);
    assert_eq!(fx.rows(), vec![5]);
    fx.verify_ok();
}

/// Probe D/D2: an existing file under the planned archive name is used only
/// if byte-identical; corrupt or planted files stop the pass with the rows kept.
#[test]
fn probe_d_and_d2_existing_files_are_never_trusted() {
    for planted in [b"not an archive".to_vec(), {
        let recs = vec![Record {
            id: 1,
            seq: 1,
            time: "2026-01-01T00:00:00Z".into(),
            event: "audit.chain".into(),
            user: String::new(),
            remote: String::new(),
            outcome: "ok".into(),
            detail: "planted".into(),
            data: None,
            prev: ZERO_HASH.into(),
            hash: String::new(),
        }];
        let mut recs = recs;
        recs[0].hash = recs[0].digest();
        let audit = archive::render_lines(&recs);
        archive::build(&audit, &archive::manifest_for(&recs, &audit).unwrap()).unwrap()
    }] {
        let fx = Fx::new("probed", &[100, 100, 0]);
        std::fs::create_dir(&fx.archive).unwrap();
        std::fs::set_permissions(&fx.archive, std::fs::Permissions::from_mode(0o700)).unwrap();
        let name = archive::archive_name(1, 2);
        std::fs::write(fx.archive.join(&name), &planted).unwrap();
        let e = block(run(&fx.opts(30, 60))).unwrap_err().to_string();
        assert!(e.contains("differs") || e.contains("do not match"), "{e}");
        assert_eq!(fx.rows(), vec![1, 2, 3]);
        assert_eq!(std::fs::read(fx.archive.join(&name)).unwrap(), planted);
        // Planted files under other names are refused too.
        std::fs::rename(
            fx.archive.join(&name),
            fx.archive.join(archive::archive_name(7, 9)),
        )
        .unwrap();
        assert!(block(run(&fx.opts(30, 60))).is_err());
        assert_eq!(fx.rows(), vec![1, 2, 3]);
    }
}

/// Probe E: a record whose content was changed (hash kept) is refused before
/// anything is archived or pruned.
#[test]
fn probe_e_content_tamper_blocks_prune() {
    let fx = Fx::new("probee", &[100, 100, 100, 0]);
    fx.exec("UPDATE audit_events SET user='forged' WHERE id=2");
    let e = block(run(&fx.opts(30, 60))).unwrap_err().to_string();
    assert!(e.contains("do not verify") && e.contains("modified"), "{e}");
    assert_eq!(fx.rows(), vec![1, 2, 3, 4]);
    assert!(fx.archives().is_empty());
}

/// Probe F and the expiry boundary: calendar months, kept at the cutoff,
/// expired just past it; shortening the window needs --confirm.
#[test]
fn probe_f_expiry_boundary_and_confirmation() {
    let mut fx = Fx::new("expiry", &[]);
    let cutoff = sub_months(fx.now, 12);
    fx.add_at(cutoff - 10);
    fx.add_at(cutoff); // newest archived record exactly at the cutoff
    fx.add(0);
    block(run(&fx.opts(30, 12))).unwrap();
    assert_eq!(fx.archives().len(), 1, "at the cutoff: kept");
    fx.now += 1; // one nanosecond later
    let s = block(run(&fx.opts(30, 12))).unwrap();
    assert_eq!(s.expired.len(), 1);
    assert!(fx.archives().is_empty());
    fx.verify_ok();

    let mut fx = Fx::new("shorten", &[100, 100, 0]);
    block(run(&fx.opts(30, 60))).unwrap();
    fx.add(0);
    let e = block(run(&fx.opts(30, 2))).unwrap_err().to_string();
    assert!(e.contains("--confirm"), "{e}");
    assert_eq!(fx.archives().len(), 1);
    let mut o = fx.opts(30, 2);
    o.dry_run = true;
    let s = block(run(&o)).unwrap();
    assert_eq!(s.expired.len(), 1, "dry run shows what would expire");
    let mut o = fx.opts(30, 2);
    o.confirm = true;
    let s = block(run(&o)).unwrap();
    assert_eq!(s.expired.len(), 1);
    fx.verify_ok();
}

/// Early archiving is driven by pages in use, stops and blocks itself when a
/// prune does not reduce them, and never drains the trail for non-audit bloat.
#[test]
fn early_archiving_uses_pages_and_blocks_on_reclaim_failure() {
    let ages = vec![0i128; 1500];
    let fx = Fx::new("early", &ages);
    let live = block(async {
        let mut d = hc_audit_retention::db::Db::open(&fx.db, true)
            .await
            .unwrap();
        d.page_stats().await.unwrap().live_bytes()
    });
    let mut o = fx.opts(30, 60);
    o.threshold_bytes = Some(live - 1);
    let s = block(run(&o)).unwrap();
    assert!(
        s.pruned_rows > 0 && s.pruned_rows < 1499,
        "{} {:?}",
        s.pruned_rows,
        s.notes
    );
    let l = Ledger::load(&fx.archive, false).unwrap();
    assert!(!l.early_blocked());
    fx.verify_ok();

    // Reclaim failure: the pages in use did not go down. Block, do not keep pruning.
    let fx = Fx::new("early-fail", &ages);
    let mut o = fx.opts(30, 60);
    o.threshold_bytes = Some(live - 1);
    o.live_after_override = Some(i64::MAX / 4);
    let s = block(run(&o)).unwrap();
    let first = s.pruned_rows;
    assert!(
        first > 0 && Ledger::load(&fx.archive, false).unwrap().early_blocked(),
        "{:?}",
        s.notes
    );
    let s = block(run(&o)).unwrap();
    assert_eq!(
        s.pruned_rows, 0,
        "blocked: no progressive pruning of younger rows"
    );
    let mut o2 = o.clone();
    o2.retry_early = true;
    o2.live_after_override = None;
    block(run(&o2)).unwrap();
    assert!(
        !Ledger::load(&fx.archive, false).unwrap().early_blocked(),
        "--retry-early-archive clears the block"
    );
    fx.verify_ok();

    // Non-audit bloat: the audit trail is far smaller than the excess.
    let fx = Fx::new("bloat", &[0, 0, 0, 0]);
    fx.exec("INSERT INTO settings VALUES ('blob', hex(randomblob(400000)))");
    let mut o = fx.opts(30, 60);
    o.threshold_bytes = Some(4096);
    let s = block(run(&o)).unwrap();
    assert_eq!(s.pruned_rows, 0);
    assert!(
        s.notes.iter().any(|n| n.contains("not draining")),
        "{:?}",
        s.notes
    );
}

#[test]
fn unknown_or_missing_schema_fails_safe() {
    for v in [Some(2), Some(3), Some(5), None] {
        let fx = Fx::with_schema("schema", &[100, 100, 0], v);
        let before = snapshot(&fx.root);
        assert!(block(run(&fx.opts(30, 60))).is_err(), "{v:?}");
        assert_eq!(
            snapshot(&fx.root).keys().collect::<Vec<_>>(),
            before.keys().collect::<Vec<_>>(),
            "{v:?}: no new files"
        );
        assert_eq!(fx.rows(), vec![1, 2, 3]);
    }
}

#[test]
fn verify_detects_archive_and_database_tampering() {
    let fx = Fx::new("verify", &[100, 100, 100, 0, 0, 0]);
    block(run(&fx.opts(30, 60))).unwrap();
    fx.verify_ok();
    let a = fx.archive.join(&fx.archives()[0]);
    let good = std::fs::read(&a).unwrap();
    let mut bad = good.clone();
    let n = bad.len() - 20;
    bad[n] ^= 1;
    std::fs::write(&a, &bad).unwrap();
    assert!(!fx.verify_problems().is_empty());
    std::fs::write(&a, &good).unwrap();
    fx.verify_ok();
    fx.exec("DELETE FROM audit_events WHERE id = 5");
    assert!(fx.verify_problems().iter().any(|p| p.contains("sequence")));
}

/// hc-api keeps its connection open and appends during and after the pass.
#[test]
fn concurrent_writer_keeps_the_chain() {
    let mut fx = Fx::new("concurrent", &[100, 100, 100, 0]);
    block(async {
        let mut other = conn(&fx.db).await;
        sqlx::query("SELECT 1").execute(&mut other).await.unwrap();
        run(&fx.opts(30, 60)).await.unwrap();
        other.close().await.unwrap();
    });
    fx.add(0);
    fx.verify_ok();
    assert_eq!(fx.rows(), vec![4, 5]);
}

#[test]
fn archive_directory_must_be_private() {
    let fx = Fx::new("perm", &[100, 100, 0]);
    std::fs::create_dir(&fx.archive).unwrap();
    std::fs::set_permissions(&fx.archive, std::fs::Permissions::from_mode(0o755)).unwrap();
    assert!(block(run(&fx.opts(30, 60))).is_err());
    std::fs::set_permissions(&fx.db, std::fs::Permissions::from_mode(0o644)).unwrap();
    std::fs::set_permissions(&fx.archive, std::fs::Permissions::from_mode(0o700)).unwrap();
    assert!(
        block(run(&fx.opts(30, 60))).is_err(),
        "group-readable database"
    );
    assert_eq!(fx.rows(), vec![1, 2, 3]);
}

/// RUSTSEC-2026-0459 mitigation: a WAL whose page size differs from the
/// database's is refused before SQLite opens the pair.
#[test]
fn mismatched_wal_page_size_is_refused() {
    let fx = Fx::new("wal", &[100, 100, 0]);
    let mut wal = vec![0u8; 32];
    wal[..4].copy_from_slice(&0x377f_0682u32.to_be_bytes());
    wal[8..12].copy_from_slice(&1024u32.to_be_bytes());
    let w = PathBuf::from(format!("{}-wal", fx.db.display()));
    std::fs::write(&w, &wal).unwrap();
    std::fs::set_permissions(&w, std::fs::Permissions::from_mode(0o600)).unwrap();
    let e = block(run(&fx.opts(30, 60))).unwrap_err().to_string();
    assert!(e.contains("page size"), "{e}");
    let mut o = fx.opts(30, 60);
    o.dry_run = true;
    assert!(block(run(&o)).is_err());
    std::fs::remove_file(&w).unwrap();
    assert_eq!(fx.rows(), vec![1, 2, 3]);
}
