# HealthDashboard

System-wide Health & Diagnostics (“Fix common problems”): summary, findings by severity or module, modules checked, scan history.

**Status: Proposed (HCIS).** New in the HCIS specification; a reference design, not in the app.

Source: none in the repository.

## When to use

Covers CPU, memory and swap/OOM, disks and SMART/NVMe, ZFS, Btrfs, mdadm, LVM2, mergerfs, SnapRAID, systemd, Docker, packages, network/DNS, ECC, thermals, backups, UPS/NUT and security configuration.

## Rules

- Critical, Warning, Notice, Healthy and Unknown are distinct states.
- Acknowledged findings are hidden by default, never deleted.

Props: `HealthDashboardProps` in `reference/index.d.ts`. Preview data is fictional and labelled “Example data”.
