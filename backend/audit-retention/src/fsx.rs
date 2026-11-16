// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

//! Filesystem helpers: no-follow opens, exclusive private creates, fsync of
//! files and directories, and the directory lock.

use std::fs::{File, OpenOptions};
use std::io::Write;
use std::os::unix::fs::{MetadataExt, OpenOptionsExt};
use std::path::Path;

use crate::Result;

fn euid() -> u32 {
    unsafe { libc::geteuid() }
}

/// The state database must be a regular file (no symlink), mode 0600 or
/// stricter. Its sidecars (-wal, -shm, -journal), if present, likewise.
pub fn check_state_db(path: &Path) -> Result<std::fs::Metadata> {
    let m = std::fs::symlink_metadata(path).map_err(|e| format!("{}: {e}", path.display()))?;
    if !m.file_type().is_file() {
        return Err(format!(
            "{}: not a regular file (symlinks are refused)",
            path.display()
        )
        .into());
    }
    if m.mode() & 0o077 != 0 {
        return Err(format!(
            "{}: mode {:04o} is group/world-accessible; expected 0600",
            path.display(),
            m.mode() & 0o7777
        )
        .into());
    }
    check_wal_page_size(path)?;
    for side in ["-wal", "-shm", "-journal"] {
        let p = std::path::PathBuf::from(format!("{}{side}", path.display()));
        match std::fs::symlink_metadata(&p) {
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => {}
            Err(e) => return Err(format!("{}: {e}", p.display()).into()),
            Ok(sm) if !sm.file_type().is_file() => {
                return Err(format!("{}: not a regular file", p.display()).into())
            }
            Ok(_) => {}
        }
    }
    Ok(m)
}

/// Refuses a `-wal` file whose page size differs from the database's: the
/// precondition of CVE-2026-119375 (RUSTSEC-2026-0459, uninitialised memory
/// returned by SQLite < 3.54.0), which the SQLite bundled through
/// sqlx/libsqlite3-sys 0.30 still has. hc-api never produces such a pair.
pub fn check_wal_page_size(db: &Path) -> Result<()> {
    use std::io::Read;
    let wal = std::path::PathBuf::from(format!("{}-wal", db.display()));
    let mut wh = [0u8; 32];
    let n = match open_read(&wal) {
        Err(_) => return Ok(()), // no WAL
        Ok(mut f) => f.read(&mut wh)?,
    };
    if n < 32 {
        return Ok(()); // empty or reset WAL: no frames to read
    }
    let mut dh = [0u8; 100];
    open_read(db)?
        .read_exact(&mut dh)
        .map_err(|e| format!("{}: cannot read the database header: {e}", db.display()))?;
    if &dh[..16] != b"SQLite format 3\0" {
        return Err(format!("{}: not an SQLite 3 database", db.display()).into());
    }
    let page = |v: u32| if v == 1 { 65536 } else { v };
    let db_page = page(u16::from_be_bytes([dh[16], dh[17]]) as u32);
    let wal_page = u32::from_be_bytes([wh[8], wh[9], wh[10], wh[11]]);
    let magic = u32::from_be_bytes([wh[0], wh[1], wh[2], wh[3]]);
    if magic != 0x377f_0682 && magic != 0x377f_0683 {
        return Err(format!("{}: not a SQLite WAL file; refusing", wal.display()).into());
    }
    if wal_page != db_page {
        return Err(format!("{}: WAL page size {wal_page} differs from the database page size {db_page}; refusing to open (CVE-2026-119375 precondition)", wal.display()).into());
    }
    Ok(())
}

/// The archive directory must be a real directory (no symlink), owned by the
/// current user, mode 0700 or stricter.
pub fn check_private_dir(dir: &Path) -> Result<()> {
    let m = std::fs::symlink_metadata(dir).map_err(|e| format!("{}: {e}", dir.display()))?;
    if !m.file_type().is_dir() {
        return Err(format!("{}: not a directory (symlinks are refused)", dir.display()).into());
    }
    if m.uid() != euid() {
        return Err(format!(
            "{}: owned by uid {}, not the current user {}",
            dir.display(),
            m.uid(),
            euid()
        )
        .into());
    }
    if m.mode() & 0o077 != 0 {
        return Err(format!(
            "{}: mode {:04o} is group/world-accessible; expected 0700",
            dir.display(),
            m.mode() & 0o7777
        )
        .into());
    }
    Ok(())
}

/// Creates `dir` with mode 0700 (its parent must exist) and fsyncs the parent.
pub fn create_private_dir(dir: &Path) -> Result<()> {
    use std::os::unix::fs::DirBuilderExt;
    std::fs::DirBuilder::new()
        .mode(0o700)
        .create(dir)
        .map_err(|e| format!("create {}: {e}", dir.display()))?;
    if let Some(parent) = dir.parent() {
        fsync_dir(parent)?;
    }
    check_private_dir(dir)
}

pub fn fsync_dir(dir: &Path) -> Result<()> {
    File::open(dir)
        .and_then(|d| d.sync_all())
        .map_err(|e| format!("fsync {}: {e}", dir.display()).into())
}

/// Exclusive lock on the archive directory (flock on the directory itself,
/// so taking it creates no file). Held for the life of the returned handle.
pub fn lock_dir(dir: &Path) -> Result<File> {
    let f = OpenOptions::new()
        .read(true)
        .custom_flags(libc::O_DIRECTORY | libc::O_NOFOLLOW | libc::O_CLOEXEC)
        .open(dir)?;
    match f.try_lock() {
        Ok(()) => Ok(f),
        Err(std::fs::TryLockError::WouldBlock) => {
            Err(format!("{}: another retention run holds the lock", dir.display()).into())
        }
        Err(std::fs::TryLockError::Error(e)) => Err(format!("lock {}: {e}", dir.display()).into()),
    }
}

/// Opens an existing regular file for reading without following symlinks.
pub fn open_read(path: &Path) -> Result<File> {
    let f = OpenOptions::new()
        .read(true)
        .custom_flags(libc::O_NOFOLLOW | libc::O_CLOEXEC)
        .open(path)
        .map_err(|e| format!("open {}: {e}", path.display()))?;
    if !f.metadata()?.file_type().is_file() {
        return Err(format!("{}: not a regular file", path.display()).into());
    }
    Ok(f)
}

pub fn read_file(path: &Path, max: u64) -> Result<Vec<u8>> {
    use std::io::Read;
    let f = open_read(path)?;
    let mut buf = Vec::new();
    f.take(max + 1).read_to_end(&mut buf)?;
    if buf.len() as u64 > max {
        return Err(format!("{}: larger than {max} bytes", path.display()).into());
    }
    Ok(buf)
}

/// Writes `data` to `dir/name` durably: exclusive private temp file, fsync,
/// rename into place, fsync the directory. Fails if `name` already exists.
pub fn write_new_durable(dir: &Path, name: &str, data: &[u8]) -> Result<()> {
    let tmp = dir.join(format!(".tmp-{name}-{}", std::process::id()));
    let final_path = dir.join(name);
    if std::fs::symlink_metadata(&final_path).is_ok() {
        return Err(format!("{}: already exists", final_path.display()).into());
    }
    {
        let mut f = OpenOptions::new()
            .write(true)
            .create_new(true)
            .mode(0o600)
            .custom_flags(libc::O_NOFOLLOW | libc::O_CLOEXEC)
            .open(&tmp)
            .map_err(|e| format!("create {}: {e}", tmp.display()))?;
        f.write_all(data)?;
        f.sync_all()?;
    }
    // link() + unlink() would refuse an existing target atomically; rename
    // is acceptable because the lock is held and the target was checked.
    if let Err(e) = std::fs::rename(&tmp, &final_path) {
        let _ = std::fs::remove_file(&tmp);
        return Err(format!("rename {} -> {}: {e}", tmp.display(), final_path.display()).into());
    }
    fsync_dir(dir)
}

/// Removes leftover `.tmp-*` files from an interrupted run (never in a dry run).
pub fn temp_files(dir: &Path) -> Result<Vec<std::path::PathBuf>> {
    let mut out = Vec::new();
    for e in std::fs::read_dir(dir)? {
        let e = e?;
        if e.file_name().to_string_lossy().starts_with(".tmp-") {
            out.push(e.path());
        }
    }
    out.sort();
    Ok(out)
}

pub fn remove_durable(dir: &Path, path: &Path) -> Result<()> {
    std::fs::remove_file(path).map_err(|e| format!("remove {}: {e}", path.display()))?;
    fsync_dir(dir)
}
