// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

//! Retention settings. The owner policy fixes the maxima (365 days live,
//! 60 calendar months total); administrators may choose shorter windows in
//! the root-owned settings file, never longer ones.

use std::collections::BTreeSet;
use std::os::unix::fs::MetadataExt;
use std::path::Path;

use crate::Result;

pub const MAX_ACTIVE_RETENTION_DAYS: u32 = 365;
pub const MAX_ARCHIVE_RETENTION_MONTHS: u32 = 60;
pub const MAX_ACTIVE_DB_THRESHOLD_GIB: u32 = 1024;

/// Engineering defaults: the owner maxima, so a missing settings file never
/// deletes anything sooner than the owner policy allows.
pub const DEFAULT_ACTIVE_RETENTION_DAYS: u32 = MAX_ACTIVE_RETENTION_DAYS;
pub const DEFAULT_ARCHIVE_RETENTION_MONTHS: u32 = MAX_ARCHIVE_RETENTION_MONTHS;
pub const DEFAULT_ACTIVE_DB_THRESHOLD_GIB: u32 = 5;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Settings {
    /// Live window in `state.db`, days (1..=365).
    pub active_retention_days: u32,
    /// Total retention from event time, calendar months (1..=60); archives
    /// whose newest record is older are deleted.
    pub archive_retention_months: u32,
    /// Live-database size (pages in use) that triggers early archiving.
    pub active_db_threshold_gib: u32,
}

impl Default for Settings {
    fn default() -> Self {
        Settings {
            active_retention_days: DEFAULT_ACTIVE_RETENTION_DAYS,
            archive_retention_months: DEFAULT_ARCHIVE_RETENTION_MONTHS,
            active_db_threshold_gib: DEFAULT_ACTIVE_DB_THRESHOLD_GIB,
        }
    }
}

impl Settings {
    pub fn validate(&self) -> Result<()> {
        if !(1..=MAX_ACTIVE_RETENTION_DAYS).contains(&self.active_retention_days) {
            return Err(format!(
                "active_retention_days {} is outside 1..={MAX_ACTIVE_RETENTION_DAYS}",
                self.active_retention_days
            )
            .into());
        }
        if !(1..=MAX_ARCHIVE_RETENTION_MONTHS).contains(&self.archive_retention_months) {
            return Err(format!(
                "archive_retention_months {} is outside 1..={MAX_ARCHIVE_RETENTION_MONTHS}",
                self.archive_retention_months
            )
            .into());
        }
        if !(1..=MAX_ACTIVE_DB_THRESHOLD_GIB).contains(&self.active_db_threshold_gib) {
            return Err(format!(
                "active_db_threshold_gib {} is outside 1..={MAX_ACTIVE_DB_THRESHOLD_GIB}",
                self.active_db_threshold_gib
            )
            .into());
        }
        // The total window must cover the live window (a month has at most 31 days).
        if self.archive_retention_months as u64 * 31 < self.active_retention_days as u64 {
            return Err(format!(
                "archive_retention_months {} is shorter than active_retention_days {}: the total window must include the live window",
                self.archive_retention_months, self.active_retention_days
            )
            .into());
        }
        Ok(())
    }
}

/// Loads the settings file. A missing file means the defaults. The file must
/// be a regular file (no symlink), owned by `owner_uid` (root in
/// production), and not group- or world-writable. Unknown, duplicate or
/// malformed keys are errors: a typo must not silently fall back.
pub fn load(path: &Path, owner_uid: u32) -> Result<(Settings, bool)> {
    let meta = match std::fs::symlink_metadata(path) {
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
            let s = Settings::default();
            s.validate()?;
            return Ok((s, false));
        }
        Err(e) => return Err(format!("{}: {e}", path.display()).into()),
        Ok(m) => m,
    };
    if !meta.file_type().is_file() {
        return Err(format!(
            "{}: not a regular file (symlinks are refused)",
            path.display()
        )
        .into());
    }
    if meta.uid() != owner_uid {
        return Err(format!(
            "{}: owned by uid {}, expected {owner_uid}",
            path.display(),
            meta.uid()
        )
        .into());
    }
    if meta.mode() & 0o022 != 0 {
        return Err(format!(
            "{}: group- or world-writable (mode {:04o})",
            path.display(),
            meta.mode() & 0o7777
        )
        .into());
    }
    if meta.len() > 64 * 1024 {
        return Err(format!("{}: larger than 64 KiB", path.display()).into());
    }
    let text =
        std::fs::read_to_string(path).map_err(|e| format!("read {}: {e}", path.display()))?;
    let mut s = Settings::default();
    let mut seen = BTreeSet::new();
    for (n, raw) in text.lines().enumerate() {
        let line = raw.trim();
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        let at = format!("{}:{}", path.display(), n + 1);
        let (k, v) = line
            .split_once('=')
            .ok_or_else(|| format!("{at}: expected `key = value`"))?;
        let (k, v) = (k.trim(), v.trim());
        if !seen.insert(k.to_string()) {
            return Err(format!("{at}: duplicate setting `{k}`").into());
        }
        let n: u32 = if !v.is_empty() && v.bytes().all(|b| b.is_ascii_digit()) && v.len() <= 6 {
            v.parse()?
        } else {
            return Err(format!("{at}: `{k}` needs a positive whole number").into());
        };
        match k {
            "active_retention_days" => s.active_retention_days = n,
            "archive_retention_months" => s.archive_retention_months = n,
            "active_db_threshold_gib" => s.active_db_threshold_gib = n,
            _ => return Err(format!("{at}: unknown setting `{k}`").into()),
        }
    }
    s.validate()?;
    Ok((s, true))
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::os::unix::fs::PermissionsExt;

    fn uid() -> u32 {
        unsafe { libc::geteuid() }
    }

    fn write(dir: &Path, text: &str, mode: u32) -> std::path::PathBuf {
        std::fs::create_dir_all(dir).unwrap();
        let p = dir.join("audit-retention.conf");
        std::fs::write(&p, text).unwrap();
        std::fs::set_permissions(&p, std::fs::Permissions::from_mode(mode)).unwrap();
        p
    }

    #[test]
    fn bounds_and_defaults() {
        assert!(Settings::default().validate().is_ok());
        let ok = |a, m| {
            Settings {
                active_retention_days: a,
                archive_retention_months: m,
                active_db_threshold_gib: 5,
            }
            .validate()
            .is_ok()
        };
        assert!(ok(365, 60) && ok(1, 1) && ok(90, 24) && ok(365, 12) && ok(31, 1));
        assert!(
            !ok(366, 60) && !ok(0, 60) && !ok(365, 61) && !ok(365, 0) && !ok(32, 1) && !ok(365, 11)
        );
    }

    #[test]
    fn file_rules() {
        let dir = crate::testutil::tmp("cfg");
        let p = write(
            &dir,
            "# c\nactive_retention_days = 90\narchive_retention_months=24\n",
            0o640,
        );
        let (s, found) = load(&p, uid()).unwrap();
        assert!(
            found
                && s.active_retention_days == 90
                && s.archive_retention_months == 24
                && s.active_db_threshold_gib == 5
        );
        assert!(load(&dir.join("absent.conf"), uid()).unwrap().0 == Settings::default());
        for bad in [
            "active_retention_dayz = 90\n",
            "active_retention_days = 90\nactive_retention_days = 80\n",
            "active_retention_days = -1\n",
            "active_retention_days = 9.5\n",
            "active_retention_days\n",
            "active_retention_days = 400\n",
            "archive_retention_months = 61\n",
        ] {
            let p = write(&dir, bad, 0o640);
            assert!(load(&p, uid()).is_err(), "{bad}");
        }
        let p = write(&dir, "active_retention_days = 90\n", 0o664);
        assert!(load(&p, uid()).is_err(), "group-writable");
        let p = write(&dir, "active_retention_days = 90\n", 0o640);
        assert!(load(&p, uid() + 1).is_err(), "wrong owner");
        let link = dir.join("link.conf");
        std::os::unix::fs::symlink(&p, &link).unwrap();
        assert!(load(&link, uid()).is_err(), "symlink");
        let _ = std::fs::remove_dir_all(&dir);
    }
}
