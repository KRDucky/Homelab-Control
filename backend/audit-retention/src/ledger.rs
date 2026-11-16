// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

//! The retention ledger: an append-only, hash-chained JSON Lines file in the
//! Go audit format (`hlctl audit verify --file ledger.jsonl` verifies it).
//! Every archive creation, prune and expiry is recorded as an intent before
//! it happens and as done afterwards; each line is fsync'ed before the next
//! step, so a crash at any point leaves enough to finish or refuse safely.

use std::fs::OpenOptions;
use std::io::Write;
use std::os::unix::fs::OpenOptionsExt;
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};
use serde_json::{Map, Value};

use crate::fsx;
use crate::record::{verify_chain, Anchor, Record, ZERO_HASH};
use crate::timeutil::format_rfc3339;
use crate::Result;

pub const LEDGER_FILE: &str = "ledger.jsonl";
const MAX_LEDGER: u64 = 64 * 1024 * 1024;

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct ArchiveInfo {
    pub name: String,
    pub from_id: i64,
    pub to_id: i64,
    pub count: u64,
    pub first_seq: u64,
    pub first_prev: String,
    pub last_seq: u64,
    pub last_hash: String,
    pub oldest: String,
    pub newest: String,
    pub sha256: String,
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct PruneInfo {
    pub name: String,
    pub from_id: i64,
    pub to_id: i64,
    pub count: u64,
    pub first_hash: String,
    pub last_seq: u64,
    pub last_hash: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub live_bytes_before: Option<i64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub live_bytes_after: Option<i64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub early: Option<bool>,
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct ExpireInfo {
    pub name: String,
    pub sha256: String,
    pub newest: String,
    pub cutoff: String,
    pub archive_retention_months: u32,
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct SettingsInfo {
    pub active_retention_days: u32,
    pub archive_retention_months: u32,
    pub active_db_threshold_gib: u32,
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct EarlyInfo {
    pub live_bytes_before: i64,
    pub live_bytes_after: i64,
    pub expected_freed: i64,
}

/// A typed ledger entry.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Entry {
    Start,
    ArchiveCreated(ArchiveInfo),
    PruneIntent(PruneInfo),
    PruneDone(PruneInfo),
    ExpireIntent(ExpireInfo),
    ExpireDone(ExpireInfo),
    Settings(SettingsInfo),
    EarlyBlocked(EarlyInfo),
    EarlyCleared,
}

impl Entry {
    fn parts(&self) -> (&'static str, &'static str, Option<Value>) {
        let v = |x: &dyn ToValue| x.value();
        match self {
            Entry::Start => ("audit.chain", "ok", None),
            Entry::ArchiveCreated(i) => ("retention.archive", "created", Some(v(i))),
            Entry::PruneIntent(i) => ("retention.prune", "intent", Some(v(i))),
            Entry::PruneDone(i) => ("retention.prune", "done", Some(v(i))),
            Entry::ExpireIntent(i) => ("retention.expire", "intent", Some(v(i))),
            Entry::ExpireDone(i) => ("retention.expire", "done", Some(v(i))),
            Entry::Settings(i) => ("retention.settings", "applied", Some(v(i))),
            Entry::EarlyBlocked(i) => ("retention.early", "blocked", Some(v(i))),
            Entry::EarlyCleared => ("retention.early", "cleared", None),
        }
    }

    fn from_record(r: &Record) -> Result<Entry> {
        fn d<T: for<'de> Deserialize<'de>>(r: &Record) -> Result<T> {
            let raw = r
                .data
                .as_deref()
                .ok_or_else(|| format!("ledger seq {}: missing data", r.seq))?;
            Ok(serde_json::from_str(raw).map_err(|e| format!("ledger seq {}: {e}", r.seq))?)
        }
        Ok(match (r.event.as_str(), r.outcome.as_str()) {
            ("audit.chain", "ok") => Entry::Start,
            ("retention.archive", "created") => Entry::ArchiveCreated(d(r)?),
            ("retention.prune", "intent") => Entry::PruneIntent(d(r)?),
            ("retention.prune", "done") => Entry::PruneDone(d(r)?),
            ("retention.expire", "intent") => Entry::ExpireIntent(d(r)?),
            ("retention.expire", "done") => Entry::ExpireDone(d(r)?),
            ("retention.settings", "applied") => Entry::Settings(d(r)?),
            ("retention.early", "blocked") => Entry::EarlyBlocked(d(r)?),
            ("retention.early", "cleared") => Entry::EarlyCleared,
            (e, o) => return Err(format!("ledger seq {}: unknown entry {e}/{o}", r.seq).into()),
        })
    }
}

trait ToValue {
    fn value(&self) -> Value;
}
impl<T: Serialize> ToValue for T {
    fn value(&self) -> Value {
        serde_json::to_value(self).expect("serialize ledger data")
    }
}

/// Compact JSON with sorted keys, restricted to characters that Go's
/// `encoding/json` would not escape differently, so the stored bytes are
/// exactly what Go hashes.
fn compact_data(v: &Value) -> Result<String> {
    fn check(v: &Value) -> Result<()> {
        match v {
            Value::String(s) => {
                if s.chars()
                    .any(|c| !(' '..='~').contains(&c) || matches!(c, '<' | '>' | '&' | '"' | '\\'))
                {
                    return Err(format!(
                        "ledger value {s:?} contains a character that is not allowed"
                    )
                    .into());
                }
                Ok(())
            }
            Value::Object(m) => m.values().try_for_each(check),
            Value::Array(a) => a.iter().try_for_each(check),
            _ => Ok(()),
        }
    }
    check(v)?;
    let sorted: Map<String, Value> = match v {
        Value::Object(m) => m.iter().map(|(k, v)| (k.clone(), v.clone())).collect(),
        _ => return Err("ledger data must be an object".into()),
    };
    Ok(serde_json::to_string(&Value::Object(sorted))?)
}

pub struct Ledger {
    pub path: PathBuf,
    pub records: Vec<Record>,
    pub entries: Vec<Entry>,
    /// A torn (incomplete) last line was found; `load` with `repair` removed it.
    pub torn_tail: bool,
}

impl Ledger {
    /// Loads and verifies the ledger. A missing file is an empty ledger. A
    /// torn last line (crash during an append, before fsync returned and so
    /// before the step it announced) is removed when `repair` is set, and
    /// otherwise reported. Any other damage is an error.
    pub fn load(dir: &Path, repair: bool) -> Result<Ledger> {
        let path = dir.join(LEDGER_FILE);
        let mut l = Ledger {
            path: path.clone(),
            records: Vec::new(),
            entries: Vec::new(),
            torn_tail: false,
        };
        let bytes = match std::fs::symlink_metadata(&path) {
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(l),
            Err(e) => return Err(format!("{}: {e}", path.display()).into()),
            Ok(_) => fsx::read_file(&path, MAX_LEDGER)?,
        };
        let mut good = bytes.len();
        if !bytes.is_empty() && *bytes.last().unwrap() != b'\n' {
            good = bytes
                .iter()
                .rposition(|b| *b == b'\n')
                .map(|p| p + 1)
                .unwrap_or(0);
            l.torn_tail = true;
            if repair {
                let f = OpenOptions::new()
                    .write(true)
                    .custom_flags(libc::O_NOFOLLOW | libc::O_CLOEXEC)
                    .open(&path)?;
                f.set_len(good as u64)?;
                f.sync_all()?;
            }
        }
        let text = std::str::from_utf8(&bytes[..good])
            .map_err(|_| format!("{}: not UTF-8", path.display()))?;
        for (n, line) in text.lines().enumerate() {
            let r = Record::parse_line(line)
                .map_err(|e| format!("{} line {}: {e}", path.display(), n + 1))?;
            l.records.push(r);
        }
        let rep = verify_chain(l.records.iter(), None, "ledger");
        if !rep.ok() {
            return Err(format!(
                "{} does not verify: {}",
                path.display(),
                rep.problems.join("; ")
            )
            .into());
        }
        if rep.segments > 1 {
            return Err(format!("{}: unexpected second chain segment", path.display()).into());
        }
        for r in &l.records {
            l.entries.push(Entry::from_record(r)?);
        }
        Ok(l)
    }

    pub fn head(&self) -> Option<Anchor> {
        self.records.last().map(Record::anchor)
    }

    /// Appends one entry durably (write + fsync; fsync of the directory when
    /// the file is created). The first entry of a new ledger is preceded by
    /// the `audit.chain` start record.
    pub fn append(&mut self, e: Entry, now: i128) -> Result<()> {
        let created = self.records.is_empty();
        if created && e != Entry::Start {
            self.append_one(Entry::Start, now, true)?;
            return self.append_one(e, now, false);
        }
        self.append_one(e, now, created)
    }

    fn append_one(&mut self, e: Entry, now: i128, created: bool) -> Result<()> {
        let (event, outcome, data) = e.parts();
        let (seq, prev) = match self.records.last() {
            Some(h) => (h.seq + 1, h.hash.clone()),
            None => (1, ZERO_HASH.to_string()),
        };
        let mut r = Record {
            id: 0,
            seq,
            time: format_rfc3339(now),
            event: event.into(),
            user: String::new(),
            remote: String::new(),
            outcome: outcome.into(),
            detail: if seq == 1 {
                "retention ledger".into()
            } else {
                String::new()
            },
            data: data.as_ref().map(compact_data).transpose()?,
            prev,
            hash: String::new(),
        };
        r.hash = r.digest();
        let mut f = OpenOptions::new()
            .append(true)
            .create(true)
            .mode(0o600)
            .custom_flags(libc::O_NOFOLLOW | libc::O_CLOEXEC)
            .open(&self.path)
            .map_err(|e| format!("open {}: {e}", self.path.display()))?;
        let mut line = r.line();
        line.push('\n');
        f.write_all(line.as_bytes())?;
        f.sync_all()?;
        if created {
            fsx::fsync_dir(self.path.parent().unwrap_or(Path::new(".")))?;
        }
        self.records.push(r);
        self.entries.push(e);
        Ok(())
    }

    pub fn archives(&self) -> Vec<&ArchiveInfo> {
        self.entries
            .iter()
            .filter_map(|e| {
                if let Entry::ArchiveCreated(a) = e {
                    Some(a)
                } else {
                    None
                }
            })
            .collect()
    }

    pub fn archive(&self, name: &str) -> Option<&ArchiveInfo> {
        self.archives().into_iter().find(|a| a.name == name)
    }

    pub fn is_expired(&self, name: &str) -> bool {
        self.entries
            .iter()
            .any(|e| matches!(e, Entry::ExpireDone(x) if x.name == name))
    }

    pub fn is_pruned(&self, name: &str) -> bool {
        self.entries
            .iter()
            .any(|e| matches!(e, Entry::PruneDone(x) if x.name == name))
    }

    /// The last prune intent without a matching done entry.
    pub fn pending_prune(&self) -> Option<&PruneInfo> {
        let mut pending = None;
        for e in &self.entries {
            match e {
                Entry::PruneIntent(p) => pending = Some(p),
                Entry::PruneDone(p) if pending.map(|x| x.name == p.name).unwrap_or(false) => {
                    pending = None
                }
                _ => {}
            }
        }
        pending
    }

    pub fn pending_expire(&self) -> Option<&ExpireInfo> {
        let mut pending = None;
        for e in &self.entries {
            match e {
                Entry::ExpireIntent(x) => pending = Some(x),
                Entry::ExpireDone(x) if pending.map(|p| p.name == x.name).unwrap_or(false) => {
                    pending = None
                }
                _ => {}
            }
        }
        pending
    }

    /// The chain position the remaining database continues: the last record
    /// of the most recent completed prune.
    pub fn db_anchor(&self) -> Option<(Anchor, i64)> {
        self.entries.iter().rev().find_map(|e| match e {
            Entry::PruneDone(p) => Some((
                Anchor {
                    seq: p.last_seq,
                    hash: p.last_hash.clone(),
                },
                p.to_id,
            )),
            _ => None,
        })
    }

    pub fn last_settings(&self) -> Option<&SettingsInfo> {
        self.entries.iter().rev().find_map(|e| {
            if let Entry::Settings(s) = e {
                Some(s)
            } else {
                None
            }
        })
    }

    pub fn early_blocked(&self) -> bool {
        for e in self.entries.iter().rev() {
            match e {
                Entry::EarlyBlocked(_) => return true,
                Entry::EarlyCleared => return false,
                _ => {}
            }
        }
        false
    }
}
