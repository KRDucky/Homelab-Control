// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

//! Audit retention for the Homelab Control API audit trail (`audit_events`
//! in the Go-owned `state.db`). Records older than the live window, or the
//! oldest records when the pages in use exceed the size threshold, are
//! archived to deterministic `tar.gz` files, verified with the Go record
//! digest, and only then pruned. Archives older than the total window are
//! expired. Every step is recorded in a hash-chained ledger first, so an
//! interrupted pass is completed or refused, never guessed. See
//! `docs/security/AUDIT-LOG.md` ("Retention") and ADR 0006 amendment 4.

pub mod archive;
pub mod config;
pub mod db;
pub mod fsx;
pub mod ledger;
pub mod record;
pub mod retention;
pub mod timeutil;
pub mod verify;

pub type Result<T> = std::result::Result<T, Box<dyn std::error::Error + Send + Sync>>;

#[cfg(test)]
pub(crate) mod testutil {
    use crate::record::{Record, ZERO_HASH};
    use crate::timeutil::{format_rfc3339, now_nanos, NANOS_PER_DAY};
    use std::sync::atomic::{AtomicU64, Ordering};

    pub fn tmp(tag: &str) -> std::path::PathBuf {
        static N: AtomicU64 = AtomicU64::new(0);
        let p = std::env::temp_dir().join(format!(
            "hc-ret-{tag}-{}-{}",
            std::process::id(),
            N.fetch_add(1, Ordering::Relaxed)
        ));
        let _ = std::fs::remove_dir_all(&p);
        p
    }

    /// A correctly chained run of `n` records with real Go digests, `age_days` old.
    pub fn chain(start_seq: u64, n: u64, age_days: i128) -> Vec<Record> {
        let mut prev = ZERO_HASH.to_string();
        let t = format_rfc3339(now_nanos() - age_days * NANOS_PER_DAY);
        (0..n)
            .map(|i| {
                let seq = start_seq + i;
                let mut r = Record {
                    id: seq as i64,
                    seq,
                    time: t.clone(),
                    event: if seq == 1 {
                        "audit.chain".into()
                    } else {
                        "x.read".into()
                    },
                    user: "admin".into(),
                    remote: "192.0.2.1".into(),
                    outcome: "ok".into(),
                    detail: format!("n={seq}"),
                    data: None,
                    prev: prev.clone(),
                    hash: String::new(),
                };
                r.hash = r.digest();
                prev = r.hash.clone();
                r
            })
            .collect()
    }
}
