// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

//! Audit records and the Go-compatible record digest.
//!
//! `backend/internal/audit` hashes a record as SHA-256 over Go's
//! `json.Marshal` of the `Event` struct with `hash` cleared. This module
//! reproduces that encoding byte for byte (field order, `omitempty`, and
//! `encoding/json` string escaping with HTML escaping), so archives are
//! verified with the same per-record digest the Go verifier uses before any
//! row is pruned. Golden vectors produced by the Go code
//! (`backend/internal/audit/testdata/digest-vectors.json`) pin the encoding.

use serde::Deserialize;
use serde_json::value::RawValue;
use sha2::{Digest, Sha256};

use crate::Result;

/// The `prev` value of the first record of a chain segment.
pub const ZERO_HASH: &str = "0000000000000000000000000000000000000000000000000000000000000000";

/// One audit record. `id` is the `audit_events` row id (0 for records read
/// from an archive or the ledger). `data` is the raw compact JSON of Go's
/// `Data` field (never present in the API trail).
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Record {
    pub id: i64,
    pub seq: u64,
    pub time: String,
    pub event: String,
    pub user: String,
    pub remote: String,
    pub outcome: String,
    pub detail: String,
    pub data: Option<String>,
    pub prev: String,
    pub hash: String,
}

/// A chain position (sequence and hash of a record).
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Anchor {
    pub seq: u64,
    pub hash: String,
}

impl Record {
    /// Go's `json.Marshal(Event)`, with or without the `hash` field.
    pub fn encode(&self, with_hash: bool) -> String {
        let mut out = String::with_capacity(256);
        out.push('{');
        let mut first = true;
        let mut field = |out: &mut String, name: &str| {
            if !first {
                out.push(',');
            }
            first = false;
            out.push('"');
            out.push_str(name);
            out.push_str("\":");
        };
        if self.seq != 0 {
            field(&mut out, "seq");
            out.push_str(&self.seq.to_string());
        }
        field(&mut out, "time");
        go_quote(&mut out, &self.time);
        field(&mut out, "event");
        go_quote(&mut out, &self.event);
        if !self.user.is_empty() {
            field(&mut out, "user");
            go_quote(&mut out, &self.user);
        }
        if !self.remote.is_empty() {
            field(&mut out, "remote");
            go_quote(&mut out, &self.remote);
        }
        field(&mut out, "outcome");
        go_quote(&mut out, &self.outcome);
        if !self.detail.is_empty() {
            field(&mut out, "detail");
            go_quote(&mut out, &self.detail);
        }
        if let Some(d) = &self.data {
            if !d.is_empty() {
                field(&mut out, "data");
                go_compact(&mut out, d);
            }
        }
        if !self.prev.is_empty() {
            field(&mut out, "prev");
            go_quote(&mut out, &self.prev);
        }
        if with_hash && !self.hash.is_empty() {
            field(&mut out, "hash");
            go_quote(&mut out, &self.hash);
        }
        out.push('}');
        out
    }

    /// The Go `digest()` of this record.
    pub fn digest(&self) -> String {
        hex(&Sha256::digest(self.encode(false).as_bytes()))
    }

    /// One JSON Lines export line (no trailing newline), identical to
    /// `hlctl audit export` for the same record.
    pub fn line(&self) -> String {
        self.encode(true)
    }

    pub fn is_segment_start(&self) -> bool {
        self.event == "audit.chain" && self.seq == 1 && self.prev == ZERO_HASH
    }

    pub fn anchor(&self) -> Anchor {
        Anchor {
            seq: self.seq,
            hash: self.hash.clone(),
        }
    }

    /// Parses one export or ledger line.
    pub fn parse_line(line: &str) -> Result<Record> {
        #[derive(Deserialize)]
        #[serde(deny_unknown_fields)]
        struct In<'a> {
            #[serde(default)]
            seq: u64,
            time: String,
            event: String,
            #[serde(default)]
            user: String,
            #[serde(default)]
            remote: String,
            outcome: String,
            #[serde(default)]
            detail: String,
            #[serde(borrow, default)]
            data: Option<&'a RawValue>,
            #[serde(default)]
            prev: String,
            #[serde(default)]
            hash: String,
        }
        let r: In = serde_json::from_str(line)?;
        Ok(Record {
            id: 0,
            seq: r.seq,
            time: r.time,
            event: r.event,
            user: r.user,
            remote: r.remote,
            outcome: r.outcome,
            detail: r.detail,
            data: r.data.map(|d| d.get().to_string()),
            prev: r.prev,
            hash: r.hash,
        })
    }
}

/// Appends `s` as a JSON string exactly as Go's `encoding/json` does with
/// HTML escaping enabled (the `json.Marshal` default).
pub fn go_quote(out: &mut String, s: &str) {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    out.push('"');
    for c in s.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\u{8}' => out.push_str("\\b"),
            '\u{c}' => out.push_str("\\f"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            '<' | '>' | '&' | '\u{0}'..='\u{1f}' => {
                let b = c as u32 as u8;
                out.push_str("\\u00");
                out.push(HEX[(b >> 4) as usize] as char);
                out.push(HEX[(b & 0xf) as usize] as char);
            }
            '\u{2028}' => out.push_str("\\u2028"),
            '\u{2029}' => out.push_str("\\u2029"),
            _ => out.push(c),
        }
    }
    out.push('"');
}

/// Go's `json.Compact` with HTML escaping, as `json.Marshal` applies to a
/// `json.RawMessage`: whitespace outside strings is removed and `<`, `>`,
/// `&`, U+2028 and U+2029 inside strings are escaped. Key order is kept.
pub fn go_compact(out: &mut String, raw: &str) {
    let (mut in_str, mut esc) = (false, false);
    for c in raw.chars() {
        if in_str {
            if esc {
                esc = false;
                out.push(c);
                continue;
            }
            match c {
                '\\' => {
                    esc = true;
                    out.push(c);
                }
                '"' => {
                    in_str = false;
                    out.push(c);
                }
                '<' => out.push_str("\\u003c"),
                '>' => out.push_str("\\u003e"),
                '&' => out.push_str("\\u0026"),
                '\u{2028}' => out.push_str("\\u2028"),
                '\u{2029}' => out.push_str("\\u2029"),
                _ => out.push(c),
            }
        } else {
            match c {
                ' ' | '\t' | '\n' | '\r' => {}
                '"' => {
                    in_str = true;
                    out.push(c);
                }
                _ => out.push(c),
            }
        }
    }
}

pub fn hex(bytes: &[u8]) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let mut s = String::with_capacity(bytes.len() * 2);
    for b in bytes {
        s.push(HEX[(b >> 4) as usize] as char);
        s.push(HEX[(b & 0xf) as usize] as char);
    }
    s
}

pub fn sha256_hex(bytes: &[u8]) -> String {
    hex(&Sha256::digest(bytes))
}

/// Result of a chain verification.
#[derive(Debug, Default)]
pub struct ChainReport {
    pub records: u64,
    pub segments: u64,
    pub head: Option<Anchor>,
    pub problems: Vec<String>,
    pub warnings: Vec<String>,
}

impl ChainReport {
    pub fn ok(&self) -> bool {
        self.problems.is_empty()
    }
}

/// Verifies content digests and chain links, the same rules as Go's
/// `audit.Verify`/`VerifyAnchored`: every record's hash must equal its
/// digest; the first record must continue `anchor` (or, without an anchor,
/// start a segment); later records continue the previous one or start a new
/// segment (reported as a warning, as the Go verifier does).
pub fn verify_chain<'a, I>(records: I, anchor: Option<&Anchor>, label: &str) -> ChainReport
where
    I: IntoIterator<Item = &'a Record>,
{
    let mut rep = ChainReport::default();
    let mut head: Option<Anchor> = anchor.cloned();
    for r in records {
        let at = if r.id > 0 {
            format!("{label} id {} (seq {})", r.id, r.seq)
        } else {
            format!("{label} seq {}", r.seq)
        };
        if r.seq == 0 {
            rep.problems.push(format!("{at}: unchained record"));
            continue;
        }
        if r.hash.is_empty() || r.digest() != r.hash {
            rep.problems.push(format!(
                "{at}: record hash does not match its content (modified)"
            ));
        }
        if r.is_segment_start() {
            if head.is_some() {
                rep.warnings
                    .push(format!("{at}: new chain segment started"));
            }
            rep.segments += 1;
        } else {
            match &head {
                None => rep.problems.push(format!(
                    "{at}: chain does not start with an audit.chain record and no anchor was given"
                )),
                Some(h) => {
                    if r.seq != h.seq + 1 {
                        rep.problems.push(format!(
                            "{at}: sequence follows {} (records missing, inserted or reordered)",
                            h.seq
                        ));
                    }
                    if r.prev != h.hash {
                        rep.problems
                            .push(format!("{at}: previous-hash link broken"));
                    }
                }
            }
        }
        head = Some(r.anchor());
        rep.records += 1;
    }
    rep.head = head;
    rep
}

#[cfg(test)]
mod tests {
    use super::*;

    #[derive(Deserialize)]
    struct Vector {
        name: String,
        seq: u64,
        time: String,
        event: String,
        user: String,
        remote: String,
        outcome: String,
        detail: String,
        #[serde(default)]
        data: Option<Box<RawValue>>,
        prev: String,
        hash: String,
        line: String,
    }

    /// The Go-generated golden vectors (`go test ./internal/audit -run TestDigestVectors`).
    #[test]
    fn matches_go_digest_and_export_vectors() {
        let path = concat!(
            env!("CARGO_MANIFEST_DIR"),
            "/../internal/audit/testdata/digest-vectors.json"
        );
        let text = std::fs::read_to_string(path).expect("golden vectors");
        let vs: Vec<Vector> = serde_json::from_str(&text).unwrap();
        assert!(vs.len() >= 10);
        for v in vs {
            let r = Record {
                id: 0,
                seq: v.seq,
                time: v.time,
                event: v.event,
                user: v.user,
                remote: v.remote,
                outcome: v.outcome,
                detail: v.detail,
                data: v.data.map(|d| d.get().to_string()),
                prev: v.prev,
                hash: v.hash.clone(),
            };
            assert_eq!(r.digest(), v.hash, "digest: {}", v.name);
            assert_eq!(r.line(), v.line, "export line: {}", v.name);
            let parsed = Record::parse_line(&v.line).unwrap();
            assert_eq!(parsed.digest(), v.hash, "parse: {}", v.name);
            assert_eq!(parsed.line(), v.line, "re-encode: {}", v.name);
        }
    }

    #[test]
    fn detects_modified_and_relinked_records() {
        let mut a = Record {
            id: 1,
            seq: 1,
            time: "2026-01-01T00:00:00Z".into(),
            event: "audit.chain".into(),
            user: String::new(),
            remote: String::new(),
            outcome: "ok".into(),
            detail: String::new(),
            data: None,
            prev: ZERO_HASH.into(),
            hash: String::new(),
        };
        a.hash = a.digest();
        let mut b = Record {
            id: 2,
            seq: 2,
            event: "x.read".into(),
            prev: a.hash.clone(),
            ..a.clone()
        };
        b.hash = b.digest();
        assert!(verify_chain([&a, &b], None, "t").ok());
        let mut forged = b.clone();
        forged.user = "forged".into();
        assert!(!verify_chain([&a, &forged], None, "t").ok());
        assert!(
            !verify_chain([&b], None, "t").ok(),
            "mid-chain start without anchor"
        );
        assert!(verify_chain([&b], Some(&a.anchor()), "t").ok());
    }
}
