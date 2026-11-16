// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

//! One retention pass: recover an interrupted pass, archive the oldest
//! records, prune exactly the archived rows, and expire old archives.
//!
//! Order for each archive (every ledger line is fsync'ed before the next step):
//!  1. build the deterministic archive from the database rows, verifying
//!     every Go digest and chain link first;
//!  2. write it durably (exclusive temp file, fsync, rename, fsync dir), or
//!     adopt an existing file only if it is byte-identical;
//!  3. re-read it from disk and verify it independently;
//!  4. ledger `retention.archive created`, then `retention.prune intent`;
//!  5. delete exactly those rows in one guarded transaction;
//!  6. ledger `retention.prune done`.
//!
//! Expiry is `intent` → unlink + fsync dir → `done`. A dry run reads only:
//! it takes no lock file, creates no directory, removes nothing and opens
//! the database read-only.

use std::collections::BTreeSet;
use std::path::{Path, PathBuf};

use crate::archive::{self, archive_name, is_archive_name, MAX_ARCHIVE_FILE};
use crate::config::Settings;
use crate::db::Db;
use crate::fsx;
use crate::ledger::{
    ArchiveInfo, EarlyInfo, Entry, ExpireInfo, Ledger, PruneInfo, SettingsInfo, LEDGER_FILE,
};
use crate::record::{sha256_hex, verify_chain, Anchor, Record};
use crate::timeutil::{format_rfc3339, parse_rfc3339, sub_months, NANOS_PER_DAY};
use crate::Result;

pub const DEFAULT_CHUNK_ROWS: i64 = 100_000;

#[derive(Clone, Debug)]
pub struct Paths {
    pub state_db: PathBuf,
    pub archive_dir: PathBuf,
}

/// Test-only crash injection: the pass stops with an error right after the
/// named step, leaving files, ledger and database as a crash would.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Fault {
    AfterArchiveFile,
    AfterArchiveRecorded,
    AfterPruneIntent,
    AfterDelete,
    AfterExpireIntent,
    AfterExpireUnlink,
}

#[derive(Clone, Debug)]
pub struct Options {
    pub paths: Paths,
    pub settings: Settings,
    pub dry_run: bool,
    /// Required to apply a shorter `archive_retention_months` than last applied.
    pub confirm: bool,
    /// Lift a recorded early-archiving block.
    pub retry_early: bool,
    pub now: i128,
    pub chunk_rows: i64,
    #[doc(hidden)]
    pub fault: Option<Fault>,
    #[doc(hidden)]
    pub threshold_bytes: Option<i64>,
    /// Test-only: pretend the in-use size after a prune is this value.
    #[doc(hidden)]
    pub live_after_override: Option<i64>,
}

impl Options {
    pub fn new(paths: Paths, settings: Settings, now: i128) -> Options {
        Options {
            paths,
            settings,
            dry_run: false,
            confirm: false,
            retry_early: false,
            now,
            chunk_rows: DEFAULT_CHUNK_ROWS,
            fault: None,
            threshold_bytes: None,
            live_after_override: None,
        }
    }
}

#[derive(Debug, Default)]
pub struct Summary {
    pub dry_run: bool,
    pub archived_rows: u64,
    pub pruned_rows: u64,
    pub archives: Vec<String>,
    pub expired: Vec<String>,
    pub live_bytes: i64,
    pub threshold_bytes: i64,
    pub notes: Vec<String>,
    pub ledger_path: Option<PathBuf>,
    pub ledger_head: Option<Anchor>,
}

impl Summary {
    fn note(&mut self, s: impl Into<String>) {
        self.notes.push(s.into());
    }
}

fn fault(opts: &Options, f: Fault) -> Result<()> {
    if opts.fault == Some(f) {
        return Err(format!("injected fault after {f:?}").into());
    }
    Ok(())
}

/// The chain position before an archive's first record, or `None` when the
/// archive starts a segment.
pub fn anchor_before(first_seq: u64, first_prev: &str) -> Option<Anchor> {
    if first_seq == 1 && first_prev == crate::record::ZERO_HASH {
        None
    } else {
        Some(Anchor {
            seq: first_seq.saturating_sub(1),
            hash: first_prev.to_string(),
        })
    }
}

fn row_estimate(r: &Record) -> i64 {
    32 + (r.time.len()
        + r.event.len()
        + r.user.len()
        + r.remote.len()
        + r.outcome.len()
        + r.detail.len()
        + r.prev.len()
        + r.hash.len()) as i64
}

/// Reads an archive from disk and checks it against its ledger record.
fn read_recorded(dir: &Path, a: &ArchiveInfo) -> Result<Vec<u8>> {
    let bytes = fsx::read_file(&dir.join(&a.name), MAX_ARCHIVE_FILE)?;
    if sha256_hex(&bytes) != a.sha256 {
        return Err(format!(
            "{}: file differs from the ledger record (sha256); refusing",
            a.name
        )
        .into());
    }
    let p = archive::parse(&bytes, anchor_before(a.first_seq, &a.first_prev).as_ref())?;
    if p.manifest.from_id != a.from_id
        || p.manifest.to_id != a.to_id
        || p.manifest.count != a.count
        || p.manifest.last_hash != a.last_hash
    {
        return Err(format!("{}: manifest differs from the ledger record", a.name).into());
    }
    Ok(bytes)
}

pub async fn run(opts: &Options) -> Result<Summary> {
    let mut s = Summary {
        dry_run: opts.dry_run,
        ..Default::default()
    };
    opts.settings.validate()?;
    let dry = opts.dry_run;
    let dir = opts.paths.archive_dir.as_path();
    fsx::check_state_db(&opts.paths.state_db)?;
    // Refuse an unknown schema before touching anything else (read-only).
    {
        let mut ro = Db::open(&opts.paths.state_db, true).await?;
        let checked = ro.check_schema().await;
        ro.close().await?;
        checked?;
    }
    let mut db = Db::open(&opts.paths.state_db, dry).await?;
    db.check_schema().await?;

    let mut dir_exists = std::fs::symlink_metadata(dir).is_ok();
    if dir_exists {
        fsx::check_private_dir(dir)?;
    } else if dry {
        s.note(format!("would create archive directory {}", dir.display()));
    } else {
        fsx::create_private_dir(dir)?;
        dir_exists = true;
    }
    let _lock = if dir_exists {
        Some(fsx::lock_dir(dir)?)
    } else {
        None
    };

    if dir_exists {
        let tmps = fsx::temp_files(dir)?;
        if !tmps.is_empty() {
            if dry {
                s.note(format!(
                    "would remove {} leftover temporary file(s)",
                    tmps.len()
                ));
            } else {
                for t in &tmps {
                    fsx::remove_durable(dir, t)?;
                }
                s.note(format!("removed {} leftover temporary file(s)", tmps.len()));
            }
        }
    }

    let mut ledger = if dir_exists {
        Ledger::load(dir, !dry)?
    } else {
        Ledger {
            path: dir.join(LEDGER_FILE),
            records: Vec::new(),
            entries: Vec::new(),
            torn_tail: false,
        }
    };
    if ledger.torn_tail {
        s.note(if dry {
            "ledger ends with an interrupted (torn) line; a real run removes it"
        } else {
            "removed an interrupted (torn) last ledger line"
        });
    }
    s.ledger_path = Some(ledger.path.clone());

    // Settings: shortening the total window deletes archives sooner; it
    // needs --confirm (after a --dry-run shows what would go).
    let cur = SettingsInfo {
        active_retention_days: opts.settings.active_retention_days,
        archive_retention_months: opts.settings.archive_retention_months,
        active_db_threshold_gib: opts.settings.active_db_threshold_gib,
    };
    if let Some(prev) = ledger.last_settings() {
        if cur.archive_retention_months < prev.archive_retention_months && !opts.confirm && !dry {
            return Err(format!(
                "archive_retention_months was shortened from {} to {}: archives may be deleted sooner; run --dry-run to see what would be expired, then repeat with --confirm",
                prev.archive_retention_months, cur.archive_retention_months
            )
            .into());
        }
    }
    if ledger.last_settings() != Some(&cur) {
        if dry {
            s.note("would record the current settings in the ledger");
        } else {
            ledger.append(Entry::Settings(cur.clone()), opts.now)?;
        }
    }
    if opts.retry_early && ledger.early_blocked() {
        if dry {
            s.note("would clear the early-archiving block");
        } else {
            ledger.append(Entry::EarlyCleared, opts.now)?;
        }
    }

    // Files on disk versus the ledger.
    let mut unrecorded: BTreeSet<String> = BTreeSet::new();
    if dir_exists {
        let pending_expire = ledger.pending_expire().map(|x| x.name.clone());
        let mut present = BTreeSet::new();
        for e in std::fs::read_dir(dir)? {
            let name = e?.file_name().to_string_lossy().into_owned();
            if is_archive_name(&name) {
                present.insert(name);
            }
        }
        for a in ledger.archives() {
            if !ledger.is_expired(&a.name)
                && !present.contains(&a.name)
                && pending_expire.as_deref() != Some(&a.name)
            {
                return Err(format!(
                    "{}: recorded in the ledger but missing, without a recorded expiry; refusing",
                    a.name
                )
                .into());
            }
        }
        for n in present {
            if ledger.archive(&n).is_none() {
                unrecorded.insert(n);
            } else if ledger.is_expired(&n) {
                return Err(format!(
                    "{n}: present although the ledger records it as expired; refusing"
                )
                .into());
            }
        }
    }

    recover(&mut db, &mut ledger, dir, opts, &mut s).await?;

    // An unrecorded archive file is acceptable only as the leftover of an
    // interrupted pass: a single file starting at the oldest remaining row,
    // which is adopted below only if byte-identical. Anything else stops the
    // pass before any change.
    if !unrecorded.is_empty() {
        let (_, min_id, _) = db.extent().await?;
        let ok = unrecorded.len() == 1
            && unrecorded
                .iter()
                .next()
                .and_then(|n| n[6..26].parse::<i64>().ok())
                == Some(min_id);
        if !ok {
            return Err(format!(
                "unrecorded archive file(s) {} are not the leftover of an interrupted pass; refusing (nothing changed)",
                unrecorded.iter().cloned().collect::<Vec<_>>().join(", ")
            )
            .into());
        }
    }

    archive_and_prune(&mut db, &mut ledger, dir, opts, &mut s, &mut unrecorded).await?;
    if !unrecorded.is_empty() {
        return Err(format!(
            "unrecorded archive file(s) {} do not match the database; refusing (they may be planted or left by an interrupted run with different settings)",
            unrecorded.iter().cloned().collect::<Vec<_>>().join(", ")
        )
        .into());
    }

    expire(&mut ledger, dir, opts, &mut s)?;

    s.ledger_head = ledger.head();
    db.close().await?;
    Ok(s)
}

/// Finishes an interrupted expiry or prune recorded in the ledger.
async fn recover(
    db: &mut Db,
    ledger: &mut Ledger,
    dir: &Path,
    opts: &Options,
    s: &mut Summary,
) -> Result<()> {
    if let Some(x) = ledger.pending_expire().cloned() {
        let path = dir.join(&x.name);
        if opts.dry_run {
            s.note(format!("would finish the interrupted expiry of {}", x.name));
        } else {
            if std::fs::symlink_metadata(&path).is_ok() {
                let bytes = fsx::read_file(&path, MAX_ARCHIVE_FILE)?;
                if sha256_hex(&bytes) != x.sha256 {
                    return Err(format!(
                        "{}: differs from the ledger; refusing to delete it",
                        x.name
                    )
                    .into());
                }
                fsx::remove_durable(dir, &path)?;
            }
            ledger.append(Entry::ExpireDone(x.clone()), opts.now)?;
            s.expired.push(x.name.clone());
            s.note(format!("finished the interrupted expiry of {}", x.name));
        }
    }
    if let Some(p) = ledger.pending_prune().cloned() {
        let a = ledger
            .archive(&p.name)
            .cloned()
            .ok_or_else(|| format!("prune intent for {} without an archive record", p.name))?;
        read_recorded(dir, &a)?;
        if opts.dry_run {
            s.note(format!(
                "would finish the interrupted prune of rows {}..{} ({})",
                p.from_id, p.to_id, p.name
            ));
        } else {
            let before = db.page_stats().await?.live_bytes();
            let n = db
                .guarded_delete(p.from_id, p.to_id, p.count, &p.first_hash, &p.last_hash)
                .await?;
            db.reclaim(64).await?;
            let after = db.page_stats().await?.live_bytes();
            let mut done = p.clone();
            done.live_bytes_before = Some(before);
            done.live_bytes_after = Some(after);
            ledger.append(Entry::PruneDone(done), opts.now)?;
            s.pruned_rows += n;
            s.note(format!(
                "finished the interrupted prune of {} ({} rows deleted now)",
                p.name, n
            ));
        }
    }
    Ok(())
}

async fn archive_and_prune(
    db: &mut Db,
    ledger: &mut Ledger,
    dir: &Path,
    opts: &Options,
    s: &mut Summary,
    unrecorded: &mut BTreeSet<String>,
) -> Result<()> {
    let (count, min_id, max_id) = db.extent().await?;
    let stats = db.page_stats().await?;
    s.live_bytes = stats.live_bytes();
    s.threshold_bytes = opts
        .threshold_bytes
        .unwrap_or(opts.settings.active_db_threshold_gib as i64 * (1 << 30));
    if count <= 1 {
        s.note("nothing to archive (the newest audit record is always kept)");
        return Ok(());
    }
    // Chain position the database must continue.
    let mut anchor: Option<Anchor> = match ledger.db_anchor() {
        Some((a, prev_to)) => {
            if min_id <= prev_to {
                return Err(format!("audit_events still has rows up to id {prev_to} that the ledger records as pruned; refusing").into());
            }
            Some(a)
        }
        None => None,
    };
    let last_candidate = max_id - 1; // never the newest record

    // Records older than the live window form a contiguous oldest prefix.
    let live_cutoff = opts.now - opts.settings.active_retention_days as i128 * NANOS_PER_DAY;
    let mut age_to = min_id - 1;
    let mut cursor = min_id - 1;
    'age: loop {
        let rows = db.fetch(cursor, last_candidate, 5000).await?;
        if rows.is_empty() {
            break;
        }
        for r in &rows {
            cursor = r.id;
            let t = parse_rfc3339(&r.time).map_err(|e| format!("audit_events id {}: {e}", r.id))?;
            if t < live_cutoff {
                age_to = r.id;
            } else {
                break 'age;
            }
        }
    }

    // Early archiving when the pages in use exceed the threshold.
    let mut early_to = age_to;
    if s.live_bytes > s.threshold_bytes {
        if ledger.early_blocked() {
            s.note("the database is over the size threshold, but early archiving is blocked: an earlier early prune did not reduce the pages in use. Investigate, then run with --retry-early-archive");
        } else {
            let need = s.live_bytes - s.threshold_bytes / 10 * 9;
            let (mut acc, mut cur) = (0i64, age_to);
            let mut reached = false;
            loop {
                let sizes = db.row_sizes(cur, last_candidate, 5000).await?;
                if sizes.is_empty() {
                    break;
                }
                for (id, sz) in &sizes {
                    cur = *id;
                    acc += sz;
                    if acc >= need {
                        early_to = *id;
                        reached = true;
                        break;
                    }
                }
                if reached {
                    break;
                }
            }
            if reached {
                s.note(format!("pages in use ({} bytes) exceed the threshold ({} bytes): archiving rows up to id {early_to} early", s.live_bytes, s.threshold_bytes));
            } else {
                s.note(format!(
                    "pages in use ({} bytes) exceed the threshold ({} bytes), but archiving every audit record would not bring the database under it; not draining the audit trail",
                    s.live_bytes, s.threshold_bytes
                ));
            }
        }
    }
    let to_id = age_to.max(early_to);
    if to_id < min_id {
        s.note("no audit records are due for archiving");
        return Ok(());
    }

    let mut after = min_id - 1;
    while after < to_id {
        // An unrecorded archive from an interrupted run is adopted only if it
        // starts here and is byte-identical to the rebuilt archive.
        let adopt = unrecorded.iter().find_map(|n| {
            let from: i64 = n[6..26].parse().ok()?;
            let to: i64 = n[27..47].parse().ok()?;
            (from == after + 1 && to <= to_id).then_some((n.clone(), to))
        });
        if adopt.is_none() && !unrecorded.is_empty() {
            return Err(format!(
                "unrecorded archive file(s) {} cannot be adopted for the rows now due; refusing (nothing pruned in this step)",
                unrecorded.iter().cloned().collect::<Vec<_>>().join(", ")
            )
            .into());
        }
        let upto = adopt.as_ref().map(|(_, to)| *to).unwrap_or(to_id);
        let limit = if adopt.is_some() {
            i64::MAX
        } else {
            opts.chunk_rows
        };
        let records = db.fetch(after, upto, limit).await?;
        let Some(last) = records.last() else { break };
        let (from_id, chunk_to) = (records[0].id, last.id);
        let first_anchor = if records[0].is_segment_start() {
            None
        } else {
            anchor.clone()
        };
        let rep = verify_chain(records.iter(), first_anchor.as_ref(), "audit_events");
        if !rep.ok() {
            return Err(format!(
                "audit records to be archived do not verify; nothing archived or pruned: {}",
                rep.problems.join("; ")
            )
            .into());
        }
        let early = chunk_to > age_to;
        let audit = archive::render_lines(&records);
        let m = archive::manifest_for(&records, &audit)?;
        let bytes = archive::build(&audit, &m)?;
        let name = archive_name(from_id, chunk_to);
        if let Some((n, _)) = &adopt {
            if *n != name {
                return Err(format!(
                    "{n}: unrecorded archive does not match the database rows; refusing"
                )
                .into());
            }
        }

        let path = dir.join(&name);
        let exists = std::fs::symlink_metadata(&path).is_ok();
        if exists {
            let on_disk = fsx::read_file(&path, MAX_ARCHIVE_FILE)?;
            if on_disk != bytes {
                return Err(format!("{name}: an existing file differs from the archive rebuilt from the database; refusing (nothing pruned)").into());
            }
        }
        unrecorded.remove(&name);

        if opts.dry_run {
            s.note(format!(
                "would {} {} row(s) id {from_id}..{chunk_to} as {name}{} and prune them",
                if exists {
                    "adopt the identical existing archive for"
                } else {
                    "archive"
                },
                records.len(),
                if early { " (early)" } else { "" }
            ));
            s.archived_rows += records.len() as u64;
            anchor = Some(last.anchor());
            after = chunk_to;
            continue;
        }

        if exists {
            s.note(format!(
                "adopted {name} left by an interrupted run (byte-identical)"
            ));
        } else {
            fsx::write_new_durable(dir, &name, &bytes)?;
        }
        fault(opts, Fault::AfterArchiveFile)?;

        // Independent check of what is actually on disk.
        let on_disk = fsx::read_file(&path, MAX_ARCHIVE_FILE)?;
        if on_disk != bytes {
            return Err(format!(
                "{name}: re-read archive differs from what was written; nothing pruned"
            )
            .into());
        }
        let parsed = archive::parse(&on_disk, first_anchor.as_ref())?;
        if parsed.records.len() != records.len()
            || parsed
                .records
                .iter()
                .zip(&records)
                .any(|(a, b)| a.hash != b.hash || a.seq != b.seq)
        {
            return Err(format!(
                "{name}: re-read archive does not hold the database rows; nothing pruned"
            )
            .into());
        }
        let info = ArchiveInfo {
            name: name.clone(),
            from_id,
            to_id: chunk_to,
            count: records.len() as u64,
            first_seq: m.first_seq,
            first_prev: m.first_prev.clone(),
            last_seq: m.last_seq,
            last_hash: m.last_hash.clone(),
            oldest: m.oldest.clone(),
            newest: m.newest.clone(),
            sha256: sha256_hex(&on_disk),
        };
        match ledger.archive(&name) {
            None => ledger.append(Entry::ArchiveCreated(info.clone()), opts.now)?,
            Some(rec) if *rec == info => {}
            Some(_) => {
                return Err(
                    format!("{name}: ledger record differs from the archive; refusing").into(),
                )
            }
        }
        fault(opts, Fault::AfterArchiveRecorded)?;

        let before = db.page_stats().await?.live_bytes();
        let mut p = PruneInfo {
            name: name.clone(),
            from_id,
            to_id: chunk_to,
            count: records.len() as u64,
            first_hash: records[0].hash.clone(),
            last_seq: last.seq,
            last_hash: last.hash.clone(),
            live_bytes_before: None,
            live_bytes_after: None,
            early: early.then_some(true),
        };
        ledger.append(Entry::PruneIntent(p.clone()), opts.now)?;
        fault(opts, Fault::AfterPruneIntent)?;
        let n = db
            .guarded_delete(from_id, chunk_to, p.count, &p.first_hash, &p.last_hash)
            .await?;
        if n != p.count {
            return Err(format!(
                "{name}: archived rows were already gone from the database; refusing"
            )
            .into());
        }
        fault(opts, Fault::AfterDelete)?;
        db.reclaim(64).await?;
        let after_bytes = match opts.live_after_override {
            Some(v) => v,
            None => db.page_stats().await?.live_bytes(),
        };
        p.live_bytes_before = Some(before);
        p.live_bytes_after = Some(after_bytes);
        ledger.append(Entry::PruneDone(p), opts.now)?;
        s.archived_rows += n;
        s.pruned_rows += n;
        s.archives.push(name.clone());
        anchor = Some(last.anchor());
        after = chunk_to;

        if early {
            let expected: i64 = records
                .iter()
                .filter(|r| r.id > age_to)
                .map(row_estimate)
                .sum();
            if before - after_bytes < expected / 2 {
                ledger.append(
                    Entry::EarlyBlocked(EarlyInfo {
                        live_bytes_before: before,
                        live_bytes_after: after_bytes,
                        expected_freed: expected,
                    }),
                    opts.now,
                )?;
                s.note(format!(
                    "early archiving freed {} bytes of pages in use, expected about {expected}; stopped and blocked further early archiving (run --retry-early-archive after investigating)",
                    before - after_bytes
                ));
                break;
            }
        }
    }
    Ok(())
}

fn expire(ledger: &mut Ledger, dir: &Path, opts: &Options, s: &mut Summary) -> Result<()> {
    let live_cutoff = opts.now - opts.settings.active_retention_days as i128 * NANOS_PER_DAY;
    let cutoff = sub_months(opts.now, opts.settings.archive_retention_months).min(live_cutoff);
    let mut list: Vec<ArchiveInfo> = ledger
        .archives()
        .into_iter()
        .filter(|a| !ledger.is_expired(&a.name))
        .cloned()
        .collect();
    list.sort_by_key(|a| a.from_id);
    for a in list {
        if !ledger.is_pruned(&a.name) {
            break; // never expire an archive whose rows are still being pruned
        }
        let newest = parse_rfc3339(&a.newest)?;
        if newest >= cutoff {
            break; // contiguous oldest prefix only
        }
        let x = ExpireInfo {
            name: a.name.clone(),
            sha256: a.sha256.clone(),
            newest: a.newest.clone(),
            cutoff: format_rfc3339(cutoff),
            archive_retention_months: opts.settings.archive_retention_months,
        };
        if opts.dry_run {
            s.note(format!(
                "would expire {} (newest record {}, cutoff {})",
                a.name, a.newest, x.cutoff
            ));
            s.expired.push(a.name.clone());
            continue;
        }
        let path = dir.join(&a.name);
        let bytes = fsx::read_file(&path, MAX_ARCHIVE_FILE)?;
        if sha256_hex(&bytes) != a.sha256 {
            return Err(format!(
                "{}: differs from the ledger record; refusing to delete it",
                a.name
            )
            .into());
        }
        ledger.append(Entry::ExpireIntent(x.clone()), opts.now)?;
        fault(opts, Fault::AfterExpireIntent)?;
        fsx::remove_durable(dir, &path)?;
        fault(opts, Fault::AfterExpireUnlink)?;
        ledger.append(Entry::ExpireDone(x), opts.now)?;
        s.expired.push(a.name.clone());
    }
    Ok(())
}
