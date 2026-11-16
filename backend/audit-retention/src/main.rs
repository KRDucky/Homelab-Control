// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

//! `hc-audit-retention`: archive, verify and prune the Homelab Control API
//! audit trail, and expire old archives.

use std::os::unix::fs::MetadataExt;
use std::path::PathBuf;
use std::process::ExitCode;

use hc_audit_retention::config;
use hc_audit_retention::retention::{self, Options, Paths};
use hc_audit_retention::timeutil::now_nanos;
use hc_audit_retention::verify;

const USAGE: &str = "\
usage: hc-audit-retention run    [--dry-run] [--confirm] [--retry-early-archive] [PATHS]
       hc-audit-retention verify [PATHS]

  run      archive records past the live window (or the oldest records when the
           database's pages in use exceed the threshold), verify them, prune
           exactly the archived rows, and expire archives past the total window.
           --dry-run reads only and reports what would happen.
           --confirm is required once after archive_retention_months is shortened.
  verify   read-only check of the ledger, every archive and the live database
           as one hash chain (every record digest checked).

PATHS:
  --state-db PATH     default /var/lib/homelab-control/state.db
  --archive-dir PATH  default /var/lib/homelab-control/audit-archive
  --config PATH       default /etc/homelab-control/audit-retention.conf (root-owned)

Archived records are not shown on the Audit page or by `hlctl audit export`;
they are in the archive directory and are checked by `verify`.
";

fn main() -> ExitCode {
    match real_main() {
        Ok(code) => code,
        Err(e) => {
            eprintln!("hc-audit-retention: {e}");
            ExitCode::FAILURE
        }
    }
}

fn real_main() -> hc_audit_retention::Result<ExitCode> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let Some(cmd) = args.first().cloned() else {
        eprint!("{USAGE}");
        return Ok(ExitCode::from(2));
    };
    let mut paths = Paths {
        state_db: "/var/lib/homelab-control/state.db".into(),
        archive_dir: "/var/lib/homelab-control/audit-archive".into(),
    };
    let mut config_path = PathBuf::from("/etc/homelab-control/audit-retention.conf");
    let (mut dry, mut confirm, mut retry) = (false, false, false);
    let mut i = 1;
    while i < args.len() {
        let a = args[i].as_str();
        let mut value = || -> hc_audit_retention::Result<PathBuf> {
            i += 1;
            Ok(PathBuf::from(
                args.get(i).ok_or_else(|| format!("{a} needs a value"))?,
            ))
        };
        match a {
            "--state-db" => paths.state_db = value()?,
            "--archive-dir" => paths.archive_dir = value()?,
            "--config" => config_path = value()?,
            "--dry-run" if cmd == "run" => dry = true,
            "--confirm" if cmd == "run" => confirm = true,
            "--retry-early-archive" if cmd == "run" => retry = true,
            "-h" | "--help" => {
                print!("{USAGE}");
                return Ok(ExitCode::SUCCESS);
            }
            other => return Err(format!("unknown argument `{other}`\n\n{USAGE}").into()),
        }
        i += 1;
    }
    let rt = tokio::runtime::Builder::new_current_thread()
        .enable_all()
        .build()?;
    match cmd.as_str() {
        "run" => {
            let euid = unsafe { libc::geteuid() };
            if !dry {
                if euid == 0 {
                    return Err("refusing to run as root: run as the state database owner (systemctl start homelab-control-audit-retention.service, or runuser -u homelab-control --)".into());
                }
                let owner = std::fs::symlink_metadata(&paths.state_db)
                    .map_err(|e| format!("{}: {e}", paths.state_db.display()))?
                    .uid();
                if owner != euid {
                    return Err(format!(
                        "{} is owned by uid {owner}; run as that user",
                        paths.state_db.display()
                    )
                    .into());
                }
            }
            let (settings, found) = config::load(&config_path, 0)?;
            let mut o = Options::new(paths, settings, now_nanos());
            o.dry_run = dry;
            o.confirm = confirm;
            o.retry_early = retry;
            let s = rt.block_on(retention::run(&o))?;
            if !found {
                println!(
                    "note: {} not found; using the defaults (the owner maxima)",
                    config_path.display()
                );
            }
            println!("dry_run: {}", s.dry_run);
            println!(
                "live_bytes: {} (threshold {})",
                s.live_bytes, s.threshold_bytes
            );
            println!("archived_rows: {}", s.archived_rows);
            println!("pruned_rows: {}", s.pruned_rows);
            for a in &s.archives {
                println!("archive: {a}");
            }
            for e in &s.expired {
                println!(
                    "{}: {e}",
                    if s.dry_run { "would expire" } else { "expired" }
                );
            }
            for n in &s.notes {
                println!("note: {n}");
            }
            // Same shape as the Go loggers' checkpoints: journald keeps the
            // ledger head outside the service user's reach.
            if let (Some(p), Some(h)) = (&s.ledger_path, &s.ledger_head) {
                if !s.dry_run {
                    println!(
                        "audit checkpoint log={} seq={} hash={}",
                        p.display(),
                        h.seq,
                        h.hash
                    );
                }
            }
            Ok(ExitCode::SUCCESS)
        }
        "verify" => {
            let r = rt.block_on(verify::verify(&paths))?;
            println!(
                "ledger records {}, archives verified {}, archives expired {}, archived records {}, live records {}",
                r.ledger_records, r.archives_verified, r.archives_expired, r.archived_records, r.db_records
            );
            println!(
                "note: archived records are not shown on the Audit page or by `hlctl audit export`"
            );
            for w in &r.warnings {
                println!("warning: {w}");
            }
            for p in &r.problems {
                println!("PROBLEM: {p}");
            }
            Ok(if r.ok() {
                ExitCode::SUCCESS
            } else {
                ExitCode::FAILURE
            })
        }
        _ => {
            eprint!("{USAGE}");
            Ok(ExitCode::from(2))
        }
    }
}
