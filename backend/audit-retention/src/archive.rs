// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

//! Deterministic `tar.gz` archives: exactly two regular members,
//! `manifest.json` then `audit.jsonl`, fixed owner/mode/mtime and a gzip
//! header without a timestamp, so rebuilding the same range gives the same
//! bytes. `audit.jsonl` is the Go export format, line for line.

use std::io::Read;

use serde::{Deserialize, Serialize};

use crate::record::{sha256_hex, verify_chain, Anchor, Record};
use crate::timeutil::parse_rfc3339;
use crate::Result;

pub const MANIFEST_MEMBER: &str = "manifest.json";
pub const AUDIT_MEMBER: &str = "audit.jsonl";
pub const FORMAT: u32 = 1;
pub const MAX_MANIFEST: u64 = 64 * 1024;
pub const MAX_AUDIT: u64 = 256 * 1024 * 1024;
pub const MAX_ARCHIVE_FILE: u64 = MAX_AUDIT + MAX_MANIFEST + 1024 * 1024;

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Manifest {
    pub format: u32,
    pub from_id: i64,
    pub to_id: i64,
    pub count: u64,
    pub first_seq: u64,
    pub first_prev: String,
    pub last_seq: u64,
    pub last_hash: String,
    /// Earliest and latest event time in the archive (by value, not order).
    pub oldest: String,
    pub newest: String,
    pub audit_sha256: String,
}

pub fn archive_name(from_id: i64, to_id: i64) -> String {
    format!("audit-{from_id:020}-{to_id:020}.tar.gz")
}

pub fn is_archive_name(name: &str) -> bool {
    let b = name.as_bytes();
    name.len() == 6 + 20 + 1 + 20 + 7
        && name.starts_with("audit-")
        && name.ends_with(".tar.gz")
        && b[26] == b'-'
        && b[6..26].iter().all(u8::is_ascii_digit)
        && b[27..47].iter().all(u8::is_ascii_digit)
}

/// Builds the manifest for `records` (non-empty, oldest first).
pub fn manifest_for(records: &[Record], audit: &[u8]) -> Result<Manifest> {
    let first = records.first().ok_or("empty archive")?;
    let last = records.last().unwrap();
    let mut oldest = (parse_rfc3339(&first.time)?, first.time.clone());
    let mut newest = oldest.clone();
    for r in records {
        let t = parse_rfc3339(&r.time).map_err(|e| format!("id {}: {e}", r.id))?;
        if t < oldest.0 {
            oldest = (t, r.time.clone());
        }
        if t > newest.0 {
            newest = (t, r.time.clone());
        }
    }
    Ok(Manifest {
        format: FORMAT,
        from_id: first.id,
        to_id: last.id,
        count: records.len() as u64,
        first_seq: first.seq,
        first_prev: first.prev.clone(),
        last_seq: last.seq,
        last_hash: last.hash.clone(),
        oldest: oldest.1,
        newest: newest.1,
        audit_sha256: sha256_hex(audit),
    })
}

pub fn render_lines(records: &[Record]) -> Vec<u8> {
    let mut out = Vec::new();
    for r in records {
        out.extend_from_slice(r.line().as_bytes());
        out.push(b'\n');
    }
    out
}

fn member(tar: &mut tar::Builder<impl std::io::Write>, name: &str, data: &[u8]) -> Result<()> {
    let mut h = tar::Header::new_ustar();
    h.set_path(name)?;
    h.set_size(data.len() as u64);
    h.set_mode(0o400);
    h.set_uid(0);
    h.set_gid(0);
    h.set_mtime(0);
    h.set_entry_type(tar::EntryType::Regular);
    h.set_cksum();
    tar.append(&h, data)?;
    Ok(())
}

pub fn build(audit: &[u8], m: &Manifest) -> Result<Vec<u8>> {
    let mut manifest = serde_json::to_vec_pretty(m)?;
    manifest.push(b'\n');
    let gz = flate2::GzBuilder::new()
        .mtime(0)
        .operating_system(255)
        .write(Vec::new(), flate2::Compression::new(6));
    let mut tar = tar::Builder::new(gz);
    tar.mode(tar::HeaderMode::Deterministic);
    member(&mut tar, MANIFEST_MEMBER, &manifest)?;
    member(&mut tar, AUDIT_MEMBER, audit)?;
    let gz = tar.into_inner()?;
    Ok(gz.finish()?)
}

/// A parsed and fully checked archive.
pub struct Parsed {
    pub manifest: Manifest,
    pub records: Vec<Record>,
}

/// Reads an archive: exactly the two expected regular members in order,
/// bounded sizes, then checks the manifest against the records and verifies
/// every record digest and chain link (`anchor` is the record before the
/// first one, or `None` when the archive starts a segment).
pub fn parse(bytes: &[u8], anchor: Option<&Anchor>) -> Result<Parsed> {
    let gz = flate2::read::GzDecoder::new(bytes);
    let mut ar = tar::Archive::new(gz.take(MAX_ARCHIVE_FILE));
    let mut members: Vec<(String, Vec<u8>)> = Vec::new();
    for entry in ar.entries()? {
        let entry = entry?;
        let kind = entry.header().entry_type();
        let path = entry.path()?.to_string_lossy().into_owned();
        let expect = match members.len() {
            0 => (MANIFEST_MEMBER, MAX_MANIFEST),
            1 => (AUDIT_MEMBER, MAX_AUDIT),
            _ => return Err(format!("unexpected extra archive member `{path}`").into()),
        };
        if path != expect.0 {
            return Err(
                format!("archive member `{path}` where `{}` was expected", expect.0).into(),
            );
        }
        if !kind.is_file() {
            return Err(format!("archive member `{path}` is not a regular file").into());
        }
        if entry.header().size()? > expect.1 {
            return Err(format!("archive member `{path}` exceeds {} bytes", expect.1).into());
        }
        let mut data = Vec::new();
        entry.take(expect.1 + 1).read_to_end(&mut data)?;
        if data.len() as u64 > expect.1 {
            return Err(format!("archive member `{path}` exceeds {} bytes", expect.1).into());
        }
        members.push((path, data));
    }
    if members.len() != 2 {
        return Err("archive must contain manifest.json and audit.jsonl".into());
    }
    let manifest: Manifest = serde_json::from_slice(&members[0].1)?;
    let audit = &members[1].1;
    if manifest.format != FORMAT {
        return Err(format!("unsupported archive format {}", manifest.format).into());
    }
    if sha256_hex(audit) != manifest.audit_sha256 {
        return Err("audit.jsonl does not match the manifest's audit_sha256".into());
    }
    let text = std::str::from_utf8(audit)?;
    if !text.is_empty() && !text.ends_with('\n') {
        return Err("audit.jsonl does not end with a newline".into());
    }
    let mut records = Vec::new();
    for (n, line) in text.lines().enumerate() {
        let r = Record::parse_line(line).map_err(|e| format!("audit.jsonl line {}: {e}", n + 1))?;
        if r.data.is_some() {
            return Err(format!(
                "audit.jsonl line {}: the API trail has no data field",
                n + 1
            )
            .into());
        }
        records.push(r);
    }
    let rep = verify_chain(records.iter(), anchor, "archive");
    if !rep.ok() {
        return Err(format!("archive chain: {}", rep.problems.join("; ")).into());
    }
    let mut ids = records.clone();
    if let Some(f) = ids.first_mut() {
        f.id = manifest.from_id;
    }
    if let Some(l) = ids.last_mut() {
        l.id = manifest.to_id;
    }
    let want = manifest_for(&ids, audit)?;
    if want != manifest {
        return Err("manifest does not describe the archived records".into());
    }
    Ok(Parsed { manifest, records })
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::testutil::chain;

    #[test]
    fn deterministic_and_round_trips() {
        let recs = chain(1, 5, 100);
        let audit = render_lines(&recs);
        let m = manifest_for(&recs, &audit).unwrap();
        let a = build(&audit, &m).unwrap();
        assert_eq!(a, build(&audit, &m).unwrap());
        let p = parse(&a, None).unwrap();
        assert_eq!(p.manifest, m);
        assert_eq!(p.records.len(), 5);
        assert!(is_archive_name(&archive_name(1, 5)) && !is_archive_name("audit-1-5.tar.gz"));
    }

    #[test]
    fn rejects_bad_members_and_content() {
        let recs = chain(1, 3, 100);
        let audit = render_lines(&recs);
        let m = manifest_for(&recs, &audit).unwrap();
        let tarball = |members: &[(&str, &[u8], tar::EntryType)]| {
            let gz = flate2::GzBuilder::new().write(Vec::new(), flate2::Compression::new(6));
            let mut t = tar::Builder::new(gz);
            for (name, data, kind) in members {
                let mut h = tar::Header::new_ustar();
                // Write the name field directly: set_path refuses `..`.
                h.as_mut_bytes()[..name.len()].copy_from_slice(name.as_bytes());
                h.set_size(data.len() as u64);
                h.set_entry_type(*kind);
                h.set_mode(0o400);
                h.set_cksum();
                t.append(&h, *data).unwrap();
            }
            t.into_inner().unwrap().finish().unwrap()
        };
        let mj = serde_json::to_vec_pretty(&m).unwrap();
        let reg = tar::EntryType::Regular;
        assert!(parse(
            &tarball(&[(MANIFEST_MEMBER, &mj, reg), (AUDIT_MEMBER, &audit, reg)]),
            None
        )
        .is_ok());
        assert!(
            parse(
                &tarball(&[(AUDIT_MEMBER, &audit, reg), (MANIFEST_MEMBER, &mj, reg)]),
                None
            )
            .is_err(),
            "order"
        );
        assert!(
            parse(
                &tarball(&[
                    (MANIFEST_MEMBER, &mj, reg),
                    (AUDIT_MEMBER, &audit, reg),
                    ("x", b"1", reg)
                ]),
                None
            )
            .is_err(),
            "extra"
        );
        assert!(
            parse(
                &tarball(&[
                    (MANIFEST_MEMBER, &mj, reg),
                    (AUDIT_MEMBER, b"", tar::EntryType::Symlink)
                ]),
                None
            )
            .is_err(),
            "symlink"
        );
        assert!(
            parse(
                &tarball(&[("../manifest.json", &mj, reg), (AUDIT_MEMBER, &audit, reg)]),
                None
            )
            .is_err(),
            "traversal"
        );
        let mut tampered = audit.clone();
        let pos = tampered.windows(6).position(|w| w == b"x.read").unwrap();
        tampered[pos] = b'y';
        let mut m2 = m.clone();
        m2.audit_sha256 = sha256_hex(&tampered);
        let mj2 = serde_json::to_vec_pretty(&m2).unwrap();
        assert!(
            parse(
                &tarball(&[(MANIFEST_MEMBER, &mj2, reg), (AUDIT_MEMBER, &tampered, reg)]),
                None
            )
            .is_err(),
            "content digest"
        );
        assert!(parse(b"not gzip", None).is_err());
    }
}
