// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

//! Read-only verification across the ledger, every archive and the live
//! database: one continuous chain with every Go digest checked. Expired
//! archives are accounted for by their ledger records.

use std::collections::BTreeSet;

use crate::archive::{self, is_archive_name, MAX_ARCHIVE_FILE};
use crate::db::Db;
use crate::fsx;
use crate::ledger::Ledger;
use crate::record::{sha256_hex, verify_chain, Anchor};
use crate::retention::{anchor_before, Paths};
use crate::Result;

#[derive(Debug, Default)]
pub struct Report {
    pub ledger_records: u64,
    pub archives_verified: u64,
    pub archives_expired: u64,
    pub archived_records: u64,
    pub db_records: u64,
    pub problems: Vec<String>,
    pub warnings: Vec<String>,
}

impl Report {
    pub fn ok(&self) -> bool {
        self.problems.is_empty()
    }
}

fn links(prev: &Option<Anchor>, first_seq: u64, first_prev: &str) -> bool {
    match (prev, anchor_before(first_seq, first_prev)) {
        (_, None) => true, // a new segment starts
        (None, Some(_)) => false,
        (Some(p), Some(a)) => *p == a,
    }
}

pub async fn verify(paths: &Paths) -> Result<Report> {
    let mut rep = Report::default();
    fsx::check_state_db(&paths.state_db)?;
    let dir = paths.archive_dir.as_path();
    let dir_exists = std::fs::symlink_metadata(dir).is_ok();
    let ledger = if dir_exists {
        Some(Ledger::load(dir, false)?)
    } else {
        None
    };
    let mut chain: Option<Anchor> = None;
    let mut pending_to: Option<i64> = None;
    if let Some(l) = &ledger {
        rep.ledger_records = l.records.len() as u64;
        if l.torn_tail {
            rep.warnings
                .push("ledger ends with an interrupted line (the next run removes it)".into());
        }
        if l.pending_prune().is_some() || l.pending_expire().is_some() {
            rep.warnings
                .push("an interrupted retention pass is pending; the next run completes it".into());
        }
        pending_to = l.pending_prune().map(|p| p.to_id);
        let mut recorded = BTreeSet::new();
        let mut list: Vec<_> = l.archives().into_iter().cloned().collect();
        list.sort_by_key(|a| a.from_id);
        let mut last_to = 0i64;
        for a in &list {
            recorded.insert(a.name.clone());
            if a.from_id <= last_to {
                rep.problems
                    .push(format!("{}: overlaps the previous archive", a.name));
            }
            last_to = a.to_id;
            if !links(&chain, a.first_seq, &a.first_prev) {
                rep.problems.push(format!(
                    "{}: does not continue the previous archive's chain",
                    a.name
                ));
            }
            let path = dir.join(&a.name);
            if l.is_expired(&a.name) {
                rep.archives_expired += 1;
                if std::fs::symlink_metadata(&path).is_ok() {
                    rep.problems
                        .push(format!("{}: recorded as expired but still present", a.name));
                }
            } else {
                match fsx::read_file(&path, MAX_ARCHIVE_FILE) {
                    Err(e) => rep.problems.push(format!(
                        "{}: {e} (missing without a recorded expiry)",
                        a.name
                    )),
                    Ok(bytes) if sha256_hex(&bytes) != a.sha256 => rep.problems.push(format!(
                        "{}: differs from the ledger record (sha256)",
                        a.name
                    )),
                    Ok(bytes) => match archive::parse(
                        &bytes,
                        anchor_before(a.first_seq, &a.first_prev).as_ref(),
                    ) {
                        Err(e) => rep.problems.push(format!("{}: {e}", a.name)),
                        Ok(p) => {
                            rep.archives_verified += 1;
                            rep.archived_records += p.records.len() as u64;
                            if p.manifest.last_hash != a.last_hash || p.manifest.to_id != a.to_id {
                                rep.problems.push(format!(
                                    "{}: manifest differs from the ledger record",
                                    a.name
                                ));
                            }
                        }
                    },
                }
            }
            chain = Some(Anchor {
                seq: a.last_seq,
                hash: a.last_hash.clone(),
            });
        }
        for e in std::fs::read_dir(dir)? {
            let n = e?.file_name().to_string_lossy().into_owned();
            if is_archive_name(&n) && !recorded.contains(&n) {
                rep.problems
                    .push(format!("{n}: archive file not recorded in the ledger"));
            }
        }
    }

    let mut db = Db::open(&paths.state_db, true).await?;
    db.check_schema().await?;
    let (_, min_id, max_id) = db.extent().await?;
    // While a prune is pending, the database may still begin with the rows of
    // the last archive; then it continues the archive before that one.
    let db_anchor = match (&ledger, pending_to) {
        (Some(l), Some(to)) if min_id <= to => {
            let mut list: Vec<_> = l
                .archives()
                .into_iter()
                .filter(|a| a.to_id < min_id)
                .cloned()
                .collect();
            list.sort_by_key(|a| a.to_id);
            list.last().map(|a| Anchor {
                seq: a.last_seq,
                hash: a.last_hash.clone(),
            })
        }
        _ => chain.clone(),
    };
    let mut head = db_anchor;
    let mut cursor = min_id - 1;
    loop {
        let rows = db.fetch(cursor, max_id, 10_000).await?;
        if rows.is_empty() {
            break;
        }
        let first_anchor = if rows[0].is_segment_start() {
            None
        } else {
            head.clone()
        };
        let r = verify_chain(rows.iter(), first_anchor.as_ref(), "audit_events");
        rep.problems.extend(r.problems);
        rep.warnings.extend(r.warnings);
        rep.db_records += r.records;
        head = r.head;
        cursor = rows.last().unwrap().id;
    }
    db.close().await?;
    Ok(rep)
}
