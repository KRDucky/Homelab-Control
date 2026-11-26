# M5 storage tools checkpoint (2026-11-25; not M5 acceptance)

The read-only storage helper is active and runs the standard tools: `lsblk`, `smartctl` (SATA/SAS SMART;
NVMe SMART needs CAP_SYS_ADMIN, not granted) and `pvs`/`lvs --readonly`. The native LVM reader
(`internal/lvmmeta`) and blkid fd-pinning are removed. VM-validated on Ubuntu 26.04, Debian 13 and Ubuntu
22.04 (27/27 each): `docs/evidence/m5/STORAGE-TOOLS.md`.

# Independent project; Webmin removed (2026-11-25)

Owner: Homelab Control is not a fork of Webmin (ADR 0016). `compat/webmin`, the upstream sync, verify and
licence-inventory scripts, the `upstream-perl-tests` CI job and the Legacy Webmin feature are removed. The
Git history is rewritten so no commit contains `compat/`. Commit hashes cited below are from before the
rewrite and are kept as text.

# Production service management (2026-11-21)

Owner: M2 service management is production. `main` before this branch is `08f0e4aa` (PR #136). New accounts get `services.read`, `services.operate`, `services.configure`, `services.logs`, `services.files` and `services.systemctl`. Existing `users.json` is not rewritten. `audit.read` stays off. `services.systemctl` is the closed systemctl set (mask, unmask, drop-in edit, daemon-reload, host power, socket and timer verbs, and the other templates in that operation). It is not a shell. Issue #90 and GitHub milestone 3 were closed on 2026-11-21. E2 was not run and is not a gate. M3 and M4 are not started. The notes below are history.

# Review candidate — not acceptance (2026-11-20)

`main` is `a00318b5475bceb504f31d10ba327f2abe1ecbdd` (PR #133 squash, merged 2026-11-20T17:49:57Z by KRDucky). Its parent `b71325d7` is the PR #132 squash. The following verdict still names `79a10715` as the tip; that sentence is now historical. Product behavior is `b71325d7` plus the documentation in PRs #130 through #133. GitHub checks on `b71325d7` finished 21/21 success. Checks on `a00318b5` were not recorded here as 21/21. CI is not owner acceptance.

This note is the review pull request requested on 2026-11-18. Merging it does not accept M2, does not close GitHub milestone 3, does not close issue #90, and does not activate `services.operate` or `services.configure`. Stage B is on main through PR #127 and is not accepted here. ADR 0006 amendment 4 is Accepted. Other proposed ADRs stay Proposed. The Ubuntu 26.04.1 hostname and timezone proof and the Debian 13.7 package proof are already recorded below. The AppArmor profile `hc-storage-probe` was not rechecked on Ubuntu. No new profile was added. Fedora, Arch, and SELinux do not block this release.

# Current verdict — M2 remains open (2026-11-20)

`main` is `79a10715` (PR #131 squash), whose parent `c85154e0` is the PR #130 squash. GitHub checks
for `79a10715` finished 21/21 success on 2026-11-20, including check, browser-e2e, upstream-perl-tests,
systemd-ubuntu, systemd-ubuntu-2604, upgrade-ubuntu, arch-package, release-dry-run build/arch/verify,
and install-matrix jobs for Debian stable/oldstable, Ubuntu 22.04/24.04/26.04, Fedora latest, Arch,
openSUSE Leap, and openSUSE Tumbleweed. CI is not owner acceptance.

The older Phase B and file-manager statements below are historical. File-manager read, write, download,
upload, and content are on `main` through PR #128. Stage B is on `main` through PR #127 and is not
accepted here. The B20 harness fix is on `main` through PR #125; the owner closed the disposable-guest
rerun on 2026-11-20 without running it. The Ubuntu 26.04.1 hostname/timezone proof in PR #130 is
sufficient for that check. Fedora, Arch, and SELinux do not block this release. M2 (GitHub milestone 3)
and issue #90 remain open. This note does not record production activation.

Debian 13 package proof (2026-11-20): reused disposable guest `hc-m24-apparmor-debian13-20261117`
ran Debian GNU/Linux 13 (trixie), version 13.7. It installed `homelab-control 0.0.0+git.b71325d7`,
built in the guest from `b71325d7ce6ed5773199842a25e09fd50e61cdf2`; DEB SHA-256:
`581774cf4d6c0783744ce863739a084c2cac91da08cf6a6620e2de7ee77f8048`. No PR #119 package was used.
The first install stopped at a conffile prompt for the existing
`/etc/apparmor.d/usr.lib.homelab-control.hc-storage-probe` profile from an earlier test on this guest.
The repeated install used `--force-confnew` and succeeded; no AppArmor profile was added. After install,
`homelab-control` was active and `GET /healthz` on `127.0.0.1:9180` returned 200. The guest shut off
with ACPI, autostart stayed disabled, and the disk was kept. GitHub checks on `b71325d7` finished
21/21 success; CI is not owner acceptance. Evidence:
`/mnt/tank/claude-vms/artifacts/m2-debian13-b71325d7-20261120/RESULT.txt`. This proof is not M2
acceptance or production activation; issue #90 and milestone 3 remain open.

# Current status — M2-3 A3–A5 evidence (2026-11-19); M2 remains open

PR #118 was merged as `c54fe5cc`. PR #119 is on `main` as `91d79d30`. PR #120, the corrected Fedora
A1/A2 note, is `ccf9ea5b`. None of those merges is owner acceptance of M2 or M2-3.

On 2026-11-19, host job `job_081e973ecda011c468a4d9482ca0f644` ran A3, A4, and A5 on the merged PR #119
release-candidate package. Fedora 44 (SELinux Enforcing), Ubuntu 26.04.1, and Ubuntu 22.04.5 each
recorded A3 7/0 and A4 6/0. A5 was 19 pass / 1 fail on each guest: browser step B20 expected
`[403, 200]` and observed `[403, 403]` because the harness fetch sends no CSRF token. Phase B was
not run. The same job re-ran A1 74/0 and A2 16/0 on that package first; the Fedora guest's earlier
A1/A2 had used the `build.zip` RPM. All three guests were left shut off with autostart disabled.
M2-3 and M2 remain **open**. Sanitized evidence is at
[`m2-3/kvm-pr119-91d79d30-a3a5-2026-11-19/README.md`](evidence/m2/m2-3/kvm-pr119-91d79d30-a3a5-2026-11-19/README.md).
The 2026-11-18 A1/A2 record remains at
[`m2-3/kvm-pr119-2026-11-18-a1-a2/README.md`](evidence/m2/m2-3/kvm-pr119-2026-11-18-a1-a2/README.md).
M2-4 is read-only inventory; its helper remains disabled by
default. See [the browser evidence](m2/evidence/browser-2026-11-18/README.md).

The historical reconciliation below remains a record of the 2026-11-07 main snapshot; it is not the
current M2 verdict.

Reconciled against `origin/main` at `20ddc2b7` (PR #54 squash `20ddc2b7` on PR #53 `9fc9d8af` on
`b290f9dc`, 2026-11-07). Git first-parent history is the
source for merge state. "Merged" means a first-parent commit on `main` (a merge commit, or a squash after
PR #28); PR #27 is the one earlier exception, described below. Dates are UTC, from those commit timestamps.
"CI n/n" is the number of GitHub Actions check runs that succeeded out of those run on the PR's
head commit, read from GitHub on 2026-10-25 for PRs #5–#24 (it is not a statement about later commits). For
PRs #25–#28 only each workflow run's overall result is recorded, because the public run pages do not show
individual job conclusions. CI counts already printed for those PRs were not re-read for this STATUS
update. This file does not close M2, accept an ADR, or record production or KVM acceptance.
The working-skeleton close bar (ask_ui `m2-close-m3-d8`) is docs and tests: isolated Playwright+axe for
layout (on `main` as PRs #48 and #52), `/files`, and host-settings preview. Those scripts are opt-in,
labelled-fixture only, and not in CI. PR #53 (`9fc9d8af`, 2026-11-07) folded the remaining files
Origin GET + host-settings Playwright checks, putting the working-skeleton close evidence on `main` (not
accepted). M3 Theme/Plugin SDK is authorized for isolated development after that (D-8; keep
`docs/project/04-ROADMAP.md`); its first checkpoint is on `main` as PR #54 (`20ddc2b7`). This is not M2
close, not production, not KVM acceptance, and not ADR acceptance.

## Audit retention (owner-accepted amendment; implementation and deployment evidence remain scoped separately)

`hc-audit-retention` (Rust, SQLx) archives API audit records past the live window (≤ 365 days) or above the
size threshold into verified `tar.gz` archives, prunes exactly the archived rows, and expires archives past
the total window (≤ 60 months); daily timer, `homelab-control` user. Design: `docs/security/AUDIT-LOG.md`
("Retention"). Open: RUSTSEC-2026-0459 in the SQLite bundled by sqlx (owner-approved exception until
2027-01-31, WAL page-size mitigation); systemd, distribution and KVM runs not yet done; no UI for archived records.
This amendment does not add retention for the broker log or operation journal; both remain out of scope.

## M11 read-only networking — first checkpoint (2026-11-24; not M11 acceptance)

Branch `feat/m11-network-readonly` from `origin/main` `0f17d108`; code tested at `0bc2a917`. The owner
asked to start M11 (port-use tracker, LACP/bonding detect, firewall WebUI for the active backend). This
checkpoint is the read-only slice only:

- **Ports:** `GET /api/v1/ports` with `networking.ports.read`, via broker op `network.ports.list` (root-read,
  no arguments). It reads PID 1's socket tables. Owners are labelled `process`, `systemd-socket` (observed)
  or `account` (inferred). `CAP_SYS_PTRACE` is not added.
- **Bonds:** `GET /api/v1/bonds` with `networking.bonds.read`, read unprivileged in `hc-api` from sysfs. The
  renderer (systemd-networkd, NetworkManager, ifupdown) is observed from runtime state files or reported
  `unknown`.
- **Firewall:** `GET /api/v1/firewall` with `networking.firewall.read`, via broker op
  `network.firewall.config`. One fixed `systemctl show` picks the active backend (firewalld, ufw, nftables,
  iptables), and only that backend's saved configuration is parsed. The live ruleset is not read.

ADR 0008 Amendment 9 is Proposed. The scopes are not defaults and are not implied by any other scope.
There are no write controls. Graphify review (two passes, findings fixed) and disposable-VM end-to-end
runs on Ubuntu 26.04.1 and Debian 13 (installed package, API 47/47 and UI 15/15 on each) are recorded
in [`evidence/m11/README.md`](evidence/m11/README.md). Firewall, bond and port-binding writes need their
own owner authorization and an ADR 0011 review. Not a merge, not production.

## M3 plugin SDK — first checkpoint on main (development note; not M3 acceptance, not M2 close)

The plugin half of the M3 Theme/Plugin SDK landed on `main` as PR #54 (squash `20ddc2b7`, on PR #53
`9fc9d8af`, on `b290f9dc`). It ships the declarative read-only sample plugin: `schemas/plugin/v1.json`
(MIT), `sdk/plugin/` PDK v1 validator, the labelled sample fixture (now imported from Extensions as `extensions/plugins/sample-readonly/`), a compiled-in
`overview.sample-card` contribution with browser-local load/unload (new `hc.plugins.v1` key; no route,
scope, `state.db` row or broker op), and a Proposed ADR 0013 (plugin isolation / declarative
contributions). No executable payload. This slice folds the closeout nits (component-element render,
Appearance checkbox event subscription, docs filename) and an opt-in Playwright+axe fixture
(`tests/e2e/plugin_browser.py`, reusing `TestStorageBrowserFixture`). The checkpoint never reads a
user-supplied manifest. This is a working note only — not M3 acceptance, not a merge, and it does not
claim ADR 0010 server persistence is done.

## M4-1b Software Center — read-only checkpoint (development note; not M4 acceptance)

Read-only Software Center on a throwaway branch from `origin/main` `fea1df0e`: a `modules.json` module
(ID `packages`, path `/software`, scope `packages.read`) with four audited GET routes
(`/packages/search`, `/installed`, `/updates`, `/sources`), four unprivileged broker read ops
(`packages.*`, ADR 0008 Amendment 5 Proposed: closed argv, `LC_ALL=C`, `/dev/null` stdin, cache-only,
validated query), and a read-only four-tab UI with no Install/Remove/Update/Add-repo control. A missing
`modules.json` enables every module (default on); an unknown ID invalidates the file. `packages.read`
is in `KnownScopes`, never default, implied by nothing; `packages.manage` stays reserved for ADR 0014.
No root template, no plan/apply, no write, no metadata refresh, no repository mutation. Not M4
acceptance, not a merge, not production; ADR 0008 Amendment 5 remains Proposed.

## M7 first checkpoint: OpenZFS (read-only; not M7 acceptance)

One read-only page at `/storage/zfs` (module id `zfs`, scope `zfs.read`, never default and implied
by nothing, including `storage.read`) probes the required `zfs` and `zpool` binaries (unprivileged
`os.Stat` of well-known `/usr/sbin` then `/usr/bin` paths, no `os/exec`), optional `smartctl`
(`smartmontools`) and `nvme` (`nvme-cli`) on known families, and kernel ZFS from bounded
`/proc/filesystems` (never modprobe, never a device node). Debian/Ubuntu names `zfsutils-linux`;
Fedora, SUSE, and Arch name no package (OpenZFS is not in those default archives; Homelab Control
does not add a third-party repository). Observed `fstype=zfs` mounts come from bounded
`/proc/self/mountinfo` (dataset source and mountpoint only; datasets are not opened;
parse limits match mergerfs at 4 MiB and 8192 records; zfs rows cap at 256 with
`mountsTruncated` and the fixed text “too many mounts to list” rather than failing the
inventory). A mountinfo line whose type is not `zfs` is skipped even when the rest of
that record is malformed. A line over 64 KiB is skipped and marks the list truncated.
A line with no filesystem type, or a `zfs` line with a bad mountpoint or source, still
fails the inventory. A missing
required package offers Software Center search (`/software?q=zfsutils-linux`) when the packages
module is enabled and the session has `packages.read`. No POST install, `packages.manage`, pool
create, import, export, destroy, or `zpool`/`zfs`/`smartctl`/`nvme` execution exists. Optional
tools do not degrade capability and no disk is opened. This is a checkpoint, not M7
acceptance, not a merge, not production. Never touch production pool `tank`.

**Update 2026-11-25 (M5–M9 review, "use the standard tools"):** the page now shows pools from the tools
themselves. The read-only storage helper runs `zpool list -H -p`, `zpool status -p` and `zfs list -H -p`
(fixed templates, request scope `zfs`) with `/dev/zfs` as its one explicit device node; the broker op
`zfs.status` (scope `zfs.read`) returns pools (health, capacity, fragmentation, status/action/scan/errors
text, vdev tree with read/write/checksum counters) and datasets/volumes, and `GET /zfs` adds them as
`status`. The text report is parsed rather than `zpool status -j`, which needs OpenZFS 2.3. Without kernel
ZFS or with the helper stopped the page keeps the inventory and states why pools are missing. Still no
pool create, import, export, scrub or property write. ADR 0008 Amendment 13; evidence
`docs/evidence/m7/ZFS-TOOLS.md`.

## M7 first checkpoint: LVM2 (read-only; not M7 acceptance)

One read-only page at `/storage/lvm` (module id `lvm`, scope `lvm.read`, never default and implied
by nothing, including `storage.read`) probes the required `lvm` binary (unprivileged `os.Stat` of
`/usr/sbin/lvm` then `/usr/bin/lvm`, no `os/exec`) and optional `thin_check`. Package `lvm2` is named
for Debian, Fedora, SUSE, and Arch. Optional thin tools: Debian and Arch `thin-provisioning-tools`,
Fedora `device-mapper-persistent-data`; SUSE names no optional package. An unknown family names no
package. Kernel device-mapper is a bounded read of `/proc/devices` (last field exactly
`device-mapper`; never modprobe; listed is not ready to create a PV). Observed devices are
`dm-<digits>` under sysfs whose `dm/uuid` starts with `LVM-` (the uuid is not returned). The mapper
name comes from `dm/name` when it is valid UTF-8, at most 128 bytes, and contains no `/`, `..`, or
control character; otherwise the mapper is empty and the device is still listed. `dm-N` names are
sorted numerically before the keep cap. The directory read stops at 8192 entries and the returned
list stops at 256, with `devicesTruncated` and the fixed page text “too many devices to list”.
Mapper names ending in `_tmeta`, `_tdata`, `_cmeta`, `_cdata`, `_corig`, `_mlog`, `_pmspare`,
or `_vorigin`, numbered `_mimage_N` / `_rimage_N` / `_rmeta_N`, and device-mapper snapshot
components whose final unescaped component is `real` or `cow` are omitted. A user LV named `real`
(`vg-real`) or `foo-cow` (`vg-foo--cow`) stays listed. `thin_check` keeps a package name only when
a raw `dm/name` ends in `_tmeta` or `_tdata`, including a name that is not a safe public label;
an empty or unreadable name does not count. Otherwise the package is empty and the item stays,
matched by id. A cancelled walk stops about every 64 devices and sets `devicesTruncated`.
An unlisted device-mapper
target, including an unloaded `dm_mod`, stays the fixed text “not currently listed” (degraded, not
a module-load instruction). An unreadable device inventory is `present=false` with a fixed reason
and an empty list. No `pvs`/`vgs`/`lvs` execution and no `pvcreate`/`vgcreate`/`lvcreate`.
A missing `lvm2` package offers Software Center search (`/software?q=lvm2`) when the packages module
is enabled and the session has `packages.read`. This is a checkpoint, not M7 acceptance, not a merge,
not production. Never touch production pool `tank`.

**Update 2026-11-25 (M5–M9 review, "use the standard tools"):** the page now shows what LVM reports instead of
reconstructing LVs from sysfs `dm-*` names (that walk is removed). The read-only storage helper runs `pvs`,
`lvs` and `vgs --readonly --nolocking` (request scope `lvm`); broker op `lvm.status` (scope `lvm.read`);
`GET /lvm` adds `status` with volume groups (size, free, counts, missing PVs), logical volumes (segment type,
health, sync) and physical volumes, decoded by the Storage page's parsers. Still no create, change or remove.
ADR 0008 Amendment 16; evidence `docs/evidence/m7/LVM-TOOLS.md`.

## M7 first checkpoint: Btrfs (read-only; not M7 acceptance)

One read-only page at `/storage/btrfs` (module id `btrfs`, scope `btrfs.read`, never default and
implied by nothing, including `storage.read`) probes the required `btrfs` binary (unprivileged
`os.Stat` of `/usr/bin/btrfs` then `/usr/sbin/btrfs`, no `os/exec`) and optional `smartctl`
(`smartmontools` on known families). Debian, Fedora, and Arch name `btrfs-progs`. SUSE names
`btrfsprogs`. An unknown family names no package. Kernel Btrfs is a bounded read of
`/proc/filesystems` (last field exactly `btrfs`; never modprobe; listed is not ready to format).
Observed `fstype=btrfs` mounts come from bounded `/proc/self/mountinfo` (mountpoint, a `/dev` source,
and `subvol` / `subvolid` options only). A source that is not an absolute `/dev` path, or an unsafe
`subvol`, is skipped. A bad mountpoint still fails the inventory. Multi-device membership is not
joined from one mount source. Parse limits match the other inventory pages at 4 MiB and 8192
records; btrfs rows cap at 256 with `mountsTruncated` and the fixed text “too many mounts to list”.
No `btrfs` execution, no format, balance, scrub, replace, or device open. RAID5/6 is not offered.
A missing `btrfs-progs` (or SUSE `btrfsprogs`) package offers Software Center search when the
packages module is enabled and the session has `packages.read`. `smartctl` is offered only after a
btrfs mount is observed, and missing it does not degrade capability. This is a checkpoint, not M7
acceptance, not a merge, not production. Issue #88 stays open. Never touch production pool `tank`.

**Update 2026-11-25 (M5–M9 review, "use the standard tools"):** the page now shows each mounted filesystem as
btrfs-progs reports it. The read-only storage helper runs `btrfs filesystem usage -b`, `device stats`,
`scrub status` and `subvolume list` (request scope `btrfs`) on one mountpoint per filesystem that it picks from
its own mount table; the broker op `btrfs.status` (scope `btrfs.read`) returns allocation by profile and
device, device error counters, scrub status and subvolumes, and `GET /btrfs` adds them as `status`. Mounts
under `/home`, `/root` and `/run/user` are hidden from the helper and named on the page as not inspected.
The unused optional smartctl offer was removed (SMART is on the Storage page). Still no format, balance,
scrub start or replace. ADR 0008 Amendment 14; evidence `docs/evidence/m7/BTRFS-TOOLS.md`.

## M7 first checkpoint: ext4 (read-only; not M7 acceptance)

One read-only page at `/storage/ext4` (module id `ext4`, scope `ext4.read`, never default and
implied by nothing, including `storage.read`) probes the required `dumpe2fs` binary (unprivileged
`os.Stat` of `/usr/sbin/dumpe2fs` then `/usr/bin/dumpe2fs`, no `os/exec`). Package `e2fsprogs` is named for Debian, Fedora, SUSE, and Arch.
Kernel ext4 is a bounded read of `/proc/filesystems` (last field exactly `ext4`; never
modprobe; listed is not ready to format). Observed `fstype=ext4` mounts come from bounded
`/proc/self/mountinfo` (mountpoint and a `/dev` source only). A source that is not an absolute
`/dev` path, or a mountpoint with a `..` component, is skipped or refused. A record missing its
options field still fails the inventory instead of panicking. Parse limits match the other
inventory pages at 4 MiB and 8192 records; ext4 rows cap at 256 with `mountsTruncated` and the
fixed text “too many mounts to list”. No `dumpe2fs` execution, no format, grow, or repair, and no
device open. A missing package offers Software Center search when the packages module is enabled
and the session has `packages.read`. This is a checkpoint, not M7 acceptance, not a merge, not
production. Never touch production pool `tank`.

**Update 2026-11-26 (M5–M9 review, "use the standard tools"):** the page now shows each mounted ext4
superblock as `tune2fs -l` reports it: state, recorded errors (count, first and last error), mount counts and
check times, block and inode counts, features. The read-only storage helper runs `tune2fs -l
/dev/block/<major>:<minor>` per mounted device (request scope `ext4`); broker op `ext4.status` (scope
`ext4.read`). The helper unit now reads `block-device-mapper` so ext4 on LVM (the Ubuntu default) is covered;
loop devices are skipped with a reason. Mounts only under /home, /root or /run/user are named as not
inspected. ADR 0008 Amendment 17; evidence `docs/evidence/m7/FS-TOOLS.md`.

## Software RAID (mdadm) first checkpoint (read-only; not acceptance)

One read-only page at `/storage/mdadm` (module id `mdadm`, scope `mdadm.read`, never default and
implied by nothing, including `storage.read`; API `GET /api/v1/mdadm`) probes the required `mdadm`
binary (unprivileged `os.Stat` of `/usr/sbin/mdadm` then `/usr/bin/mdadm`, no `os/exec`). Debian,
Fedora, SUSE, and Arch name the package `mdadm`; an unknown family names no package. The inventory is
bounded `/proc/mdstat` only (1 MiB file and 64 KiB line caps, read through `os.Root`): per array the
name, RAID level as printed (empty when none is printed), state as printed (`active` / `inactive`,
optionally `(read-only)` / `(auto-read-only)`), and the number of component entries listed on the array
line (null when none). The "Personalities :" line of the same file is shown. A missing file, an
unreadable or oversized file, or any malformed array line fails closed: no arrays and fixed reason
text. Rows cap at 256 with `arraysTruncated`. No component or array device is opened, no superblock is
parsed, and no degraded verdict is derived beyond what `/proc/mdstat` prints. No create, assemble,
add, remove, grow, or reshape. A missing `mdadm` offers Software Center search when the package name
is known, the packages module is enabled and the session has `packages.read` (no POST install). This
is a checkpoint, not acceptance, not a merge, not production.

**Update 2026-11-25 (M5–M9 review, "use the standard tools"):** the page now shows each array as
`mdadm --detail` describes it: mdadm's state wording (clean, degraded, recovering), active, failed and spare
counts, rebuild/resync progress, layout, UUID and member roles. The read-only storage helper runs it (request
scope `md`) for each array in `/proc/mdstat`; broker op `mdadm.status` (scope `mdadm.read`); `GET /mdadm`
adds it as `status`. The raw `/proc/mdstat` table remains for when the report is unavailable. Still no create,
assemble, add, remove, fail or grow. ADR 0008 Amendment 15; evidence `docs/evidence/m7/MDADM-TOOLS.md`.

## M7 first checkpoint: f2fs (read-only; not M7 acceptance)

One read-only page at `/storage/f2fs` (module id `f2fs`, scope `f2fs.read`, never default and
implied by nothing, including `storage.read`) probes the required `dump.f2fs` binary (unprivileged
`os.Stat` of `/usr/sbin/dump.f2fs` then `/usr/bin/dump.f2fs`, no `os/exec`). Package Debian `f2fs-tools` is named only where verified; Fedora, Suse, Arch name no package.
Kernel f2fs is a bounded read of `/proc/filesystems` (last field exactly `f2fs`; never
modprobe; listed is not ready to format). Observed `fstype=f2fs` mounts come from bounded
`/proc/self/mountinfo` (mountpoint and a `/dev` source only). A source that is not an absolute
`/dev` path, or a mountpoint with a `..` component, is skipped or refused. A record missing its
options field still fails the inventory instead of panicking. Parse limits match the other
inventory pages at 4 MiB and 8192 records; f2fs rows cap at 256 with `mountsTruncated` and the
fixed text “too many mounts to list”. No `dump.f2fs` execution, no format, grow, or repair, and no
device open. A missing package offers Software Center search when the packages module is enabled
and the session has `packages.read`. This is a checkpoint, not M7 acceptance, not a merge, not
production. Never touch production pool `tank`.

**Update 2026-11-26 (M5–M9 review):** f2fs-tools has no read-only report for a mounted filesystem, so hc-api
now reads the kernel's documented counters from `/sys/fs/f2fs/<disk>/` unprivileged (free, dirty and
overprovision segments, reserved blocks, lifetime writes, encoding, features), matched to mounts by device
number. No broker op. Evidence `docs/evidence/m7/FS-TOOLS.md`.


## M7 first checkpoint: XFS (read-only; not M7 acceptance)

One read-only page at `/storage/xfs` (module id `xfs`, scope `xfs.read`, never default and
implied by nothing, including `storage.read`) probes the required `xfs_info` binary (unprivileged
`os.Stat` of `/usr/sbin/xfs_info` then `/usr/bin/xfs_info`, no `os/exec`). Package `xfsprogs` is named for Debian, Fedora, SUSE, and Arch.
Kernel XFS is a bounded read of `/proc/filesystems` (last field exactly `xfs`; never
modprobe; listed is not ready to format). Observed `fstype=xfs` mounts come from bounded
`/proc/self/mountinfo` (mountpoint and a `/dev` source only). A record that cannot be listed
safely (no filesystem type, a malformed xfs record such as one missing its options field, a
mountpoint with a `..` component or over 256 bytes, or a source that is not a safe `/dev` path) is skipped without
panicking, and the other mounts are still listed. Parse limits match the other inventory pages at
4 MiB and 8192 records; xfs rows cap at 256. A cap, a limit or a skipped record sets
`mountsTruncated`, and the page shows the fixed text “not every mount could be listed”. The XFS
capability follows the `xfs` module only, so disabling Storage does not hide it (mergerfs, zfs, lvm,
btrfs, ext4, mdadm and f2fs are gated the same way). No `xfs_info` execution, no format, grow, or repair, and no
device open. A missing package offers Software Center search when the packages module is enabled
and the session has `packages.read`. This is a checkpoint, not M7 acceptance, not a merge, not
production. Never touch production pool `tank`.

**Update 2026-11-26 (M5–M9 review):** the page now shows each mounted filesystem's `xfs_info` geometry
(size, block size, allocation groups, inode and sector size, stripe, log, realtime, on-disk features). The
helper runs xfs_info's own command, `xfs_spaceman -p xfs_info -c info <mountpoint>` (request scope `xfs`);
broker op `xfs.status` (scope `xfs.read`). Free space stays on the Overview page. ADR 0008 Amendment 17;
evidence `docs/evidence/m7/FS-TOOLS.md`.

## M8 first checkpoint: MergerFS + optional SnapRAID (not M8 acceptance)

One read-only page at `/storage/mergerfs` (module id `mergerfs`, scope `mergerfs.read`, never default
and implied by nothing) probes the required `mergerfs` and `fusermount3` (package `fuse3`) binaries
and optional `snapraid`, `smartctl`, `dumpe2fs`, `xfs_info`, and `btrfs` (unprivileged `os.Stat` of
well-known paths, no `os/exec`, no `packages.installed`, never those binaries run, never a disk
opened or formatted), plus kernel FUSE from bounded
`/proc/filesystems` (never `/dev/fuse`, never modprobe) and kernel release from bounded
`/proc/sys/kernel/osrelease` (visible to `mergerfs.read` without `system.read`; never `uname`, never
`mergerfs --version`; unread is not degraded), plus a read-only `/etc/fuse.conf` probe for an
uncommented `user_allow_other` line (symlink not followed, 4 KiB cap, missing file is not degraded,
file is not edited). FUSE3 (`fusermount3`, package `fuse3`) is the
supported baseline; FUSE2 `fusermount` is
not probed. A missing `fuse` line is “not currently listed” (the module may load on first mount),
not proof that mounts cannot work, and a listed line is not proof that a mount will succeed.
It then shows observed `fuse.mergerfs` pools parsed from bounded `/proc/self/mountinfo`, plus a
read-only import of `/etc/fstab` and `/etc/systemd/system/*.mount` (O_NOFOLLOW, size and entry
caps, 64 branches per entry; includes, drop-ins, generator units and symlinks never followed;
listed paths never opened; `mounted` means a fuse.mergerfs mount exists at that mountpoint),
and, when a `snapraid.conf` is readable, an optional SnapRAID section (`data`/`parity`/`content`
paths only). A missing required package hides empty-pool pages and offers a Software Center search
link (`/software?q=…`, when the packages module is enabled and the session has `packages.read`);
observed pools stay listed. No POST install or `packages.manage` exists.
SnapRAID is never implied by a mergerfs pool. The inventory reports read-only honesty
warnings when a SnapRAID parity path sits on a mergerfs branch or the fused mount, or when a
pool lists the same branch twice; data disks as branches are expected and are not warnings.
Matching is path-based (`=RW`/`=NC`/`=RO` suffixes stripped); a missing warning is not proof.
Content on a data branch is expected. Content on the fused mount or on a parity path is a warning,
as is fewer than two content files. A root-owned pool (`user_id=0`) whose parsed options omit
`allow_other` is a warning (`fuse-allow-other`), recorded after that pool's duplicate and parity
findings. Nil options and non-root mounts stay silent. `user_allow_other` in `/etc/fuse.conf` only
matters when a non-root user mounts with `allow_other`. This page does not remount. Content files are not
read, and a missing mount is not inferred.
Writes between SnapRAID syncs are not protected. A separate read, `GET /api/v1/mergerfs/snapraid/status`, asks the opt-in root helper to run only `snapraid status` (no client path and no `--conf`; SnapRAID locks the array and reads its root-only content file for status, so the read-only broker cannot run it, ADR 0008 Amendment 18). It shows the per-disk table and SnapRAID's summary sentences, including whether a sync is in progress, how old the oldest scrubbed block is, and when another SnapRAID command holds the array lock. It does not return the scrub graph or file paths, and does not run sync, diff, fix, scrub, or smart. The report describes the latest sync; later writes are not counted.
The warning list is capped (64 total, 8 per pool) with `warningsTruncated` when the cap or
collect deadline hits. Each root-owned live pool branch (`user_id=0` in mount super-options)
is measured with `fstatfs` of the directory under the collect root (`os.OpenRoot`, directory,
1.5s budget, 8 workers, 8 parked goroutines, 64 per pool / 64 total). `/proc`, `/sys`, `/dev`,
the fused mount (any observed pool mountpoint), fstab/systemd paths, and listed SnapRAID
paths are not opened as such; a data disk that is also a branch is measured as a branch.
A last-component symlink is unread. Capacity timeout degrades usage and does not 503 the
inventory. `availBytes` is unprivileged free (`bavail`). A missing measurement is not zero
capacity. Zero-byte measured filesystems still include the numbers.
The `snapraid.conf` reader follows SnapRAID's own grammar (cmdline/state.c): a path is the rest of the
line (spaces allowed), the deprecated `disk` keyword is a data disk, and `1-parity`, `q-parity`,
`r-parity` and `z-parity` are parity levels.
Saving the array definition refuses a `snapraid.conf` that Homelab Control did not write (no managed
first line), so a hand-written array's disk names, excludes and settings are never replaced.
Pool mount, array definition (fstab plus snapraid.conf), sync, scrub, and filtered recovery
(`snapraid fix -m -f` only) are plan/apply broker ops. `mergerfs.configure` and `mergerfs.maintain`
are not default grants and do not imply `mergerfs.read` or each other. The broker dials an opt-in
helper socket. The package ships `/etc/homelab-control/mergerfs-apply.enabled`. The socket has no
`[Install]` section and the package does not enable it at boot. Sync and scrub refuse a missing disk,
removed files, or an array lock that another SnapRAID command holds. Sync, scrub and fix run to completion
(ADR 0008 Amendment 19). A run that lasts more than 5 s is reported as started and continues in the
helper's unit, with SnapRAID's output and exit status in the journal. The diff gate reads the start and
end of `snapraid diff`, so a long list of changed files does not block sync. There is no `--force`, no `-e`, and no unfiltered fix. Disks are not formatted.
`storagecap.Advance` enters `execute` after a passing confirm, then `progress` and `postcheck`.
It still does not format a device itself. `ext4.format.plan` / `ext4.format.apply` (scope
`ext4.format`, not implied by `ext4.read`) call `mkfs.ext4 -q --` on one by-id path after that
gate. The unused `homelab-control-storage-helper.service` stays `ExecStart=/bin/false`.
Debian and Ubuntu install a drop-in that sets `AppArmorProfile=hc-mergerfs-apply`. The shared unit does not set it. The first release is Debian and Ubuntu only. The profile is enforce mode, allows
`mount` and the helper's three binaries, and denies other network families. It is not the probe
profile. There is no SELinux policy for this helper. It does not use a private mount or network namespace
(`PrivateMounts=no`, `PrivateNetwork=no`, `IPAddressDeny=any`). `@system-service` excludes `@mount`,
so the unit adds `@mount`. mergerfs is started in its own systemd scope so the oneshot does not stop it.
One disposable Ubuntu 24.04 guest exercised that helper socket: the pool mount stayed visible, the
array definition was written, sync completed, a later sync was refused, `fix -m -f` restored one file,
scrub completed, and a sync after a data directory was removed was refused. In that guest the refusal
after deleting the only file on a data disk was `missing-disk` (SnapRAID's all-files warning says the
disk may not be mounted). The refusal after the directory was removed was `diff-unreadable`. Both
refusals stopped the sync. Evidence run `src-20261121T224150Z-dfa0f3`.
A later disposable Ubuntu 24.04 guest exercised the same actions through hc-api and the broker:
login, step-up, and POST plan/apply for the pool mount, the array definition, sync, a refused sync
after a file was removed, `fix -m -f` of that file, scrub, and a refused sync after a data directory
was removed. Outcomes were succeeded, succeeded, succeeded, failed, succeeded, succeeded, failed.
The mount plan argv was the mergerfs command. That guest created the marker itself.
The UI was not part of this guest. Evidence run `src-20261121T225047Z-8c8798`.
That plan/apply checkpoint is on main.
A later disposable Ubuntu 24.04 guest loaded `hc-mergerfs-apply` in enforce mode and
ran the same helper under that profile. Mount, persist, sync, the removed-file sync
refusal, `fix -m -f`, scrub, and the hidden-directory sync refusal all completed, and
the journal had no AppArmor denial for the helper. The guest created the enable marker
itself; it did not install the package. The UI was not in this guest. `storagecap.Advance`
was not part of this guest. Evidence run `src-20261122T022248Z-d6dead`.
Not production-approved. Not M8 acceptance.

## P0 storage shared (development; not production, not ADR 0014)

`backend/internal/storagecap` is the shared P0 probe, install-plan preview, discovery gate,
identity/extra-confirm types, and SHARED-02 operation phases (`Advance`). Probe is still
`os.Stat` of well-known paths only. Install plans set `executes: false` (Software Center
`/software?q=` only). The MergerFS install offer opens a review dialog (`executes: false`); it
never POSTs a package. `ext4.format.apply` calls the closed `mkfs.ext4 -q -- <by-id>` template
after `Advance` reaches execute. No other filesystem format template exists.
`Advance` confirm still requires observed block-device preflight bound to the planned
device identity (each target once, including the same rdev under two keys or in
`PlannedRdev`,
block device, canonical major:minor rdev with major ≥ 1 matching the plan, not
mounted including partitions of the disk, no holders). The ext4 format op supplies
that preflight and is the storage apply that runs only after execute is reached.
An unused closed storage helper unit is packaged
(G3 placeholder: no `[Install]`, `PrivateDevices=yes`, no `DeviceAllow`, empty
caps, `ExecStart=/bin/false`, `RefuseManualStart=yes`). The main broker unit is
unchanged (`PrivateDevices=yes`). This is not owner O-1. A broker that cannot see
block devices refuses before exec. Preflight inside `storagecap` is still
caller-supplied; that package does not stat devices. A live format is a disposable
VM disk, not the MediaMouse host. `storageinfo.OverlapClaims` flags two unrelated stacks on one block node
(layered LVM/md/crypt plus a matching filesystem is one stack). Collect still does not read
`/dev/disk/by-id` (`PrivateDevices=yes`). Default is read-only. No storage mutation, no host
package install. MergerFS tooling JSON is unchanged (types aliased). Not P0 complete, not M8,
not production. Ext4 format has broker unit tests with a fake child. A live
`mkfs.ext4` in a guest has not been run for this caller. Do not format the MediaMouse host.

## M6 partition planner (first checkpoint; not acceptance)

Partition nodes in the storage graph include `start_bytes` when sysfs `start` is readable
(512-byte sectors). A missing or unreadable start stays unset, including offset zero only
when the kernel reported zero. `POST /api/v1/storage/plan/dry-run` (scope `storage.read`,
CSRF, audit detail is the slice count) checks overlap, 1 MiB alignment, bounds, and the
filesystem allowlist ext4, xfs, f2fs, btrfs, exfat, swap, none, or unknown.
`unknown` is an unobserved type and is not reported as unformatted. The report always has
`executes: false`, and every step has `executable: false`. The call does not collect
devices, call the broker, partition, format, or mount. The Storage page draws a
proportional bar from observed partition starts and can check a layout. The page
still does not partition. `POST /api/v1/storage/partition/plan` and
`POST /api/v1/storage/partition/apply` (scope `storage.partition`, not a default
and not implied by `storage.read` or `ext4.format`) write one GPT label with
`sfdisk --wipe=always --wipe-partitions=always` and a generated script. The
target is one whole by-id disk. Slices are not formatted. The apply locks the
same device number as `ext4.format`. A non-zero or killed `sfdisk` is uncertain
and is not retried. The main broker still cannot see host disks
(`PrivateDevices=yes`). The only live format path remains `ext4.format` on a
whole by-id disk. Not M6 acceptance.

Two virtual-disk checks have run, and they are partial. On 2026-11-23 the
disposable guest `m6-partplan-c1e73d69b` (Ubuntu 26.04.1, kernel
`7.0.0-43-generic`) had a blank 1 GiB virtio disk. `sfdisk` wrote a GPT label
only on that disk: starts 2048 and 32768 of 512-byte sectors, 16384 sectors
each, Linux filesystem GUID `0FC63DAF-8483-4772-8E79-3D69D8477DE4`. A static
probe built from `c1e73d69` with Go 1.26.11 and `CGO_ENABLED=0` ran
`storageinfo.Collect` and `partplan.DryRun` in the guest. Observed
`start_bytes` were 1048576 and 16777216, and both sizes were 8388608. The
observed layout posted as filesystem `unknown` was ok, with `executes: false`,
two unknown steps, and gap steps. Forcing both slices onto the first start was
not ok, with `executes: false` and the error `slice 2 overlaps another slice`.
No step was executable. Nothing was formatted. Evidence is on the VM host at
`/mnt/tank/claude-vms/artifacts/m6-partition-plan-c1e73d69/`.

The same probe passed on Debian 13.8 (`VERSION_ID=13`) in guest
`m6-partplan-c1e73d69-deb13efi`. The generic cloud image looped in BIOS GRUB,
so this guest used the already installed UEFI firmware. The blank virtio disk
showed the same starts, sizes, unknown steps, and overlap rejection. A BIOS
domain from that attempt was left shut off and was not undefined. This was
not an installed package and not the Web UI.

## M2-3: expanded service management (development authorized 2026-11-04; implementation not accepted)

**Current update (2026-11-18):** the owner authorized KVM Phase A and Phase B and opening an M2 PR. The
historical CI artifact for `40801659` is still available, but current code changes to routing/CSRF,
module registration and service-write support make it stale for this candidate. No VM has run yet.
Use the exact CI package built from the M2 PR before testing. Merge and production activation remain
separate decisions.

The owner authorized M2-3 development and isolated testing on 2026-11-04; owner acceptance and production
activation remain separate owner decisions (`docs/m2/OWNER-DECISIONS.md`, owner items O-1…O-5). The working
skeleton is on `main` as PR #36 (`a548f52f`, 2026-11-06). This is not M2-3 acceptance, not production, not
KVM acceptance, and not M2 close.

- **On `main` (PR #36, `a548f52f`):** boot-time enable/disable of `.service` units (scope `services.configure`,
  never granted by default; `--no-reload`, never `--now`; disruptive with step-up; ADR 0011 plan/apply with
  separate tokens, locks, root revalidation from disk, journal and independent read-back; both actions refused
  while unit files changed after systemd's last load; disable refused when it would stop a protected unit from
  starting at boot); read-only unit detail, redacted unit-file contents (`services.files`), timers and sockets;
  a minimal compiled-in module registry (root-owned `/etc/homelab-control/modules.json`). Findings R1–R26:
  `docs/m2/M2-3-DESIGN-REVIEW.md` §7–§11. ADR 0008 Amendment 3 and the ADR 0011 M2-3 extension remain
  **Proposed**. Owner decision 2026-11-05: the safety objective of P-7 is approved (not its implementation).
- **Evidence** (`docs/m2/M2-3-ACCEPTANCE.md`, `docs/evidence/m2/m2-3/review-d9946a4a/`): disposable Docker
  containers (not VMs; SELinux disabled) with Ubuntu 26.04.1, 24.04.5, 22.04.5 and Fedora 44: smoke/M1 71/0,
  M2-2 with expiry waits 89/0, M2-3 with expiry waits 129–130/0, broker kill 20/0 each, browser 18/18 on
  Ubuntu 26.04 and Fedora 44. `make check` exit 0. No VM, SELinux-enforcing, Debian, openSUSE or Arch evidence;
  B-2 (netplan's runtime unit configuration refused every enable/disable on the GitHub runner VM) addressed in
  product commit `00fb7f2b` with the owner-approved approach P-8 option 3 (implementation for review); evidence
  `docs/evidence/m2/m2-3/review-00fb7f2b/`. Final safety review (2026-11-05): enable trusted the current
  running state; reproduced with real container reboots and corrected in `f8d3af2e` and `eb2fc2d4` (evidence
  `docs/evidence/m2/m2-3/review-eb2fc2d4/`). Review-branch final candidate `c0f620de`: `services.files` (P-2,
  implementation owner-authorized), unit search path and drop-in precedence fail closed (`review-c0f620de/`).
  This STATUS update does not re-run that evidence against `a548f52f`. Proposals P-1, P-3…P-6 and P-9 await
  owner approval (not decisions); P-7 objective and P-8 approach approved, implementations for review;
  D-10 is resolved by P-1; KVM acceptance not authorized.
- **Still not authorized:** mask/unmask, unit editing or creation, daemon-reload, and other privileged writes
  outside the working skeletons listed below. M3 product, acceptance, and production are not granted;
  isolated Theme/Plugin SDK development is authorized after skeleton closeout (ask_ui `m2-close-m3-d8`).
  M2-4, M2-5 and M2-6 development and isolated testing were authorized on 2026-11-05
  (`docs/m2/OWNER-DECISIONS.md`, "Continuation through M2-6"); their working skeletons are on `main`
  (below). Owner acceptance, production activation, and M2 close remain pending.

## M2-4: read-only storage inventory (local closeout candidate; scoped acceptance recorded)

`docs/m2/M2-4-ACCEPTANCE.md` records acceptance for the scoped read-only inventory and explicit
on-demand probe on the local candidate. Includes native LVM metadata parsing, broker/API/graph/UI
integration, fixture differentials, and Fedora 44 SELinux plus Debian 13 AppArmor guest evidence.
Read-only means it never configures or writes storage; the explicit probe can wake disks. The helper
remains disabled by default. This does not authorize host activation or close M2.

## M2-5: manual drive-layout Stage A (owner-accepted 2026-11-20; M2 remains open)

Manual/unverified assignments, import/export, and versioned changes are implemented. hc-ui reports
18 focused checks and 204 total Vitest checks passing, plus typecheck/lint/format; these results were
not rerun in the integration environment. Synthetic Playwright+axe evidence: 18 checks pass,
desktop/mobile axe zero violations, no mobile overflow.
The owner-supplied `KRDucky/hbaviewer-cockpit` source is MIT-licensed. Its bay-layout concept informs
Stage A; no upstream code or privileged Cockpit/Python runtime was copied. Credits and license
compatibility are recorded in `OWNER-DECISIONS.md` and `MODULE-ARCHITECTURE.md`.
Static review found and repaired the medium identity disclosure (write responses omit assignment
identity without `storage.read`), plus three lows: export now omits identity and assignment metadata,
uses a dedicated 8 MiB import cap, and accepts only the custom drive drag type while not busy. Backend
follow-ups make non-searchable directories report permission errors and preserve deep paths in audit
data. Implementers report focused regression coverage and full Go/UI checks passing on isolated
branches; integrated-candidate checks remain outstanding.
See `docs/m2/M2-5-STAGE-A-ACCEPTANCE.md` and
`docs/m2/evidence/browser-2026-11-18/`.

Stage A is manual chassis/bay layout in app state (no host writes, no new broker op). D-12 source
identification and concept reconciliation are recorded. Contract: [`docs/m2/M2-5-STAGE-A-CONTRACT.md`](m2/M2-5-STAGE-A-CONTRACT.md). The
slice entered `main` through PR #40 (`1f885993`, 2026-11-06), whose squash includes the M2-5 handoff
commit labelled `#39`. Stage A was owner-accepted for M5 on 2026-11-20. Stage B is on main
through PR #127 and is not accepted by this record. M2 remains open; this is not production or KVM acceptance.

## OIDC login (working skeleton on main, not accepted)

PR #38 (`7a121dbd`, 2026-11-06) on `main`. One configured OpenID Connect provider can sign in to an
existing local account; scopes stay on the local account. Notes: [`docs/security/OIDC-SSO.md`](security/OIDC-SSO.md)
(not tested against a real provider; not accepted).

## M2-6: hostname and timezone apply (local closeout candidate; not owner-accepted)

On 2026-11-20, the owner confirmed that the Ubuntu 26.04.1 hostname/timezone proof is sufficient for
this check. It does not require Ubuntu 22.04 or Debian; Fedora and Arch remain out of this release.
The evidence and exact read-back values are in the
[`M2-6 host settings working note`](m2/M2-6-HOST-SETTINGS-SKELETON.md). This does not accept M2-6
or M2 and does not record production activation.

The synthetic Playwright fixture has 33 passing checks, zero desktop/mobile axe violations, and no
mobile overflow. DeepSeek added a confirmation-cancel regression test (reported in 205 total Vitest
checks) and repaired two storage-module test expectations; focused Go, full frontend, vet, typecheck
and lint checks were reported passing in its isolated branch, not rerun here. Its apply-capable
account opens the confirmation UI; one dry-run plan reaches only a fail-closed fake broker, with zero
apply requests. No host settings changed. Distro/KVM coverage, documentation and owner acceptance
remain open.

Owner-authorized collaborative development of hostname (static and pretty) and timezone apply. On `main`:
read-only skeleton PR #40 (`1f885993`, 2026-11-06); apply backend PR #41 (`4c3c30a7`, 2026-11-07);
apply UI PR #42 (`90453f0a`, 2026-11-07). ADR 0011 `POST /host/settings/plan` and `/apply` (scope
`host.settings.apply`, never default) with closed `hostnamectl`/`timedatectl` templates. Preview stays
non-executing and never posts plan or apply. ADR 0008 Amendment 4 and the ADR 0011 M2-6 extension remain
**Proposed**. Isolated fixtures only; no production or retained-host apply. The opt-in Playwright+axe
fixture (`TestHostSettingsBrowserFixture`, `tests/e2e/host_settings_browser.py`) is documented in
`docs/m2/M2-6-HOST-SETTINGS-SKELETON.md` and exercises preview plus confirmation against a fail-closed
fake broker. It never runs `hostnamectl` or `timedatectl`. Not in CI. This is not M2-6 owner acceptance,
not production, not KVM acceptance, and not M2 close. Working notes:
[`docs/m2/M2-6-HOST-SETTINGS-SKELETON.md`](m2/M2-6-HOST-SETTINGS-SKELETON.md).

## File manager: read-only listing (local closeout candidate; not accepted)

The navigation/details UI implementer reports 47 synthetic fixture checks, zero desktop/mobile axe
violations and no mobile overflow; evidence is preserved in `docs/m2/evidence/browser-2026-11-18/files-ui/`.
The integration host cannot rerun these checks because Node/npm/Go are unavailable. Static review's
filename spoofing, table-detail collision, unreadable-directory and audit-path findings have focused
repairs; invalid UTF-8/backslash navigation is still being resolved on the backend. Implementer test
reports are not yet rerun on the integrated candidate. ADR 0012 and owner acceptance remain open; no
write operations are in scope.

Owner `ask_ui` selected the file manager after squash-merging PR #42. Isolated **read-only** listing and
stat of entries under allow-listed confined roots (`files.read`, never default). `hc-api` reopens each
jail with `openat`+`O_NOFOLLOW`; there is no broker file operation. No content download, editor, or
writes. On `main`: backend PR #43 (`5da0ac90`, 2026-11-07); read-only UI PR #45 (`519e1c52`, 2026-11-07,
`frontend/app/src/pages/Files.tsx`, route `/files`); docs nit PR #46 (`8f8d918e`, 2026-11-07); opt-in
Playwright script PR #50 (`8bdc574a`) and Go fixture PR #49 (`0e7abb59`). Identifier-bearing GETs require
the session CSRF token; a present Origin still has to match `originAllowed` (same-origin GET fetch omits
Origin). ADR 0012 is **Proposed**. New folder, Permissions, and Download stay hidden. Opt-in Playwright+axe
(`TestFilesBrowserFixture`, `tests/e2e/files_browser.py`) is a labelled synthetic jail only; not live host
browse; not in CI. This is a working skeleton, not M2 close, not M2-6 acceptance, not production, and not
KVM acceptance. Working notes:
[`docs/m2/FILE-MANAGER-SKELETON.md`](m2/FILE-MANAGER-SKELETON.md).

## M2-2: controlled systemd service management (authorized 2026-10-23; merged 2026-10-26, PR #28, merge commit `a3447efa`)

The owner authorized M2-2 and approved D-2 (broker-issued plan → confirm → execute), D-3 (password
step-up), D-4 (`root-exec` runner class) and D-5 (built-in protected-unit policy), option A each
(`docs/m2/OWNER-DECISIONS.md`). It was delivered as two stacked PRs: M2-2a, the framework (PR #27, one
commit, `457eddf1`), and M2-2b, operations, API and UI (PR #28). GitHub reports #27 as merged (page fetched
2026-10-27: "KRDucky merged commit 457eddf into main", Oct 26); its commit entered `main` through PR #28, and
there is no separate merge commit for it.

Commits named in this section: `970341e6` is the local and container evidence commit; `987ba011` is the PR #28
head; `a3447efa` is the merge commit, whose tree is identical to `987ba011`'s. `987ba011` differs from
`970341e6` by four frontend/test files (from merging PR #26's changes into the branch) and documentation.

- **What exists on `main`:** start, stop and restart of loaded `.service` units from the Services
  page, scope `services.operate` (never granted by default); broker plan tokens (HMAC-SHA256, 120 s,
  single use, bound to op, action, canonical unit, actor, session and observed state); per-unit locks;
  revalidation; a hash-chained, fsync'ed operation journal with interrupted-operation recovery; step-up
  re-authentication for disruptive operations (5 min, per session); a fixed protected-unit policy
  applied to aliases, unit files, template bases and, transitively, dependencies; outcomes verified by
  reading the unit state back. Design: ADR 0011 and ADR 0008 Amendment 2. Results:
  `docs/m2/M2-2-ACCEPTANCE.md`. User guide: `docs/user/SERVICE-MANAGEMENT.md`.
- **Still not authorized by M2-2 itself (2026-10-26):** later milestones, enable/disable, mask/unmask,
  unit-file editing, daemon-reload, other unit types, host shutdown or reboot, arbitrary commands, storage,
  Docker, packages, users and groups, third-party modules. Later development authorizations and working
  skeletons on `main` are in the M2-3 through file-manager sections above; they are not M2-2 scope.
- **Acceptance evidence, by source** (details and exact gaps: `docs/m2/M2-2-ACCEPTANCE.md`,
  `docs/evidence/m2/`):
  - **Local and disposable containers (no VM), on `970341e6`:** `make check`; Go tests as non-root;
    Vitest 63; Playwright on Chromium and WebKit; M2-2 real-systemd scenario 86/86 on Ubuntu 26.04.1
    (systemd 259.9) and on Fedora 43 (systemd 258.11), including the token and step-up expiry waits. Not
    re-run on `987ba011` or `a3447efa`.
  - **GitHub CI:** the workflow concluded success overall on ci #83 (PR #27 head `457eddf1`), ci #84
    (`987ba011`) and ci #85 (`main`, `a3447efa`, run 40292176133); each reports 12 notices and no warnings or
    errors. The workflow is configured to run the M2-2 service-operations scenario in short mode on Ubuntu
    24.04 (runner) and 26.04 (container) and the service-operations dialog test on Chromium, Firefox and
    WebKit. **Individual job conclusions and logs were not seen**, so execution of those particular checks in
    CI is not verified. The expiry waits (`HC_M2_LONG`) are not configured in CI, and CI has no non-Debian
    real-systemd job; that evidence was then container-only; later KVM runs include both expiry waits.
  - **Later KVM closeout:** [combined-main evidence](evidence/m2/vm-final-main-d812995e/README.md): Ubuntu/Fedora/Arch extended scenario 89/0, 89/0, 88/0; smoke/M1 67/0 and browser B1–B15 15/15 each; E4 1.6/1.5. E2/E4 closed by owner 2026-10-29.
  - **Binding checklist (`docs/m2/M2-SECURITY-ACCEPTANCE.md`: every item in §1–§3 must pass before merge):**
    PR #28 was merged while E2 had no evidence and E4's required VM evidence was missing (only a container
    check existed). No evidence was linked in the PR description; other locations (comments, linked discussions)
    were not inspected, and no review was submitted on the PR page (review elsewhere is not verified). No contemporaneous owner
    exception to that pre-merge condition was recorded. By the owner's decision of 2026-10-27 (message
    "HomeControl — Documentation Reconciliation Decisions", 00:38 America/Chicago, after the merge; quoted in
    `docs/m2/M2-2-ACCEPTANCE.md`), **E2 was deferred, not waived and not passed at that time**, and is required before
    `services.operate` is enabled in a real deployment. That decision names E2 only; it does not make any
    criterion pass.
- **Latest validated CI on `main`:** run 40666815912 at `d812995e`: 21/21 successful job conclusions. Corrections #31 (Setsid/diagnostics), #32 (process-group test) and #30 (H3 cleanup/Webmin fixture) are merged. The original CI discussion above concerns older runs.

## UI foundation continuation (2026-10-25, merged in PR #24)

The owner authorized completing the remaining frontend and adding a user Theme Development Kit. PR #24 implements shared responsive tables/states, navigation-only palette, browser-local theme import/export and Theme Studio, and the MIT TDK. [Implementation handoff](ui/11-IMPLEMENTED-UI-AND-TDK.md) records the supported contract. This is frontend authorization; no host-operation permission or server settings store is added.

**Final design review (2026-10-26):** design accepted; seven defects (DT-1 to DT-7) fixed in PR #25, with follow-up fixes in PR #26 (both merged 2026-10-26). See [docs/ui/12](ui/12-FINAL-DESIGN-REVIEW-AND-HANDOFF.md) for what was and was not verified (Chromium locally; no real broker data, screen reader or Windows High Contrast device).

## Merged work
- PR #5 (licence compliance, Webmin independence, UI plan): merged 2026-10-19 as `bc7205e3`; 20/20 checks
  on `main`.
- PR #6 (release signing deferred, phased releases): merged 2026-10-19 as `8b0f017e`; 20/20 checks on
  `main` (run 39210847731).
- PR #7 (CI modernization: node24 actions pinned by SHA, manual runs, `govulncheck` v1.9.0): merged
  2026-10-20 as `259cf5bb`; 20/20 checks on `main` (run 39375492889).
- PR #8 (Phase 1 release workflow): merged 2026-10-21 as `296ebd0b`.
- PR #9 (Go 1.26.10 for GO-2026-6809): merged 2026-10-21 as `ec34b21e`; main CI run 39560364523 20/20.
- PR #10 (release closeout; v0.1.0-dev.1 published and verified; LA-13 closed for Phase 1): merged
  2026-10-21 as `31f39778`; main CI run 39570975685 20/20.
- PR #12 (M2 PR-1b: read-only service logs, hash-chained audit): merged 2026-10-21 as `04358bbf`; CI 20/20
  on head `2a5e699a`.
- PR #13 (M2 PR-1a: SQLite state, persistent sessions, audit trail, Audit and Sessions pages): merged
  2026-10-22 as `c012f299`; CI 21/21 on head `2dd78997`.
- PR #14 (HCIS design system; documents, a `REUSE.toml` licence annotation and a `CLAUDE.md` pointer, no
  application code): merged 2026-10-22 as `880309a4`; CI 21/21 on head `911cf736`.
- PR #15 (HCIS step 1, colour tokens): merged 2026-10-22 as `505f3709`; CI 21/21 on head `030f02bc`.
- PR #16 (HCIS font licensing policy; documentation only): merged 2026-10-23 as `870a69c5`; CI 21/21 on
  head `bec10e68`.
- PR #17 (HCIS step 2, bundled Inter and JetBrains Mono): merged 2026-10-23 as `e5ec4165`; CI 21/21 on
  head `738217a5`.
- PR #18 (README presents Homelab Control as an independent product; documentation only): merged
  2026-10-23 as `4bbef83a`; CI 21/21 on head `23e23073`.
- PR #19 (HCIS step 3, responsive shell and appearance preferences): merged 2026-10-23 as `16650cb6`; CI
  21/21 on head `8b71139a`.
- PR #20 (`NOTICE.md` lists `modernc.org/sqlite`; documentation only): merged 2026-10-26 as `f1b919bc`; CI
  21/21 on head `d76ad33d`.
- PR #21 (this file reconciled with M2 PR-1 and HCIS steps 1-3; documentation only): merged 2026-10-26 as
  `1553908b`; CI 21/21 on head `fc0e39f8`.
- PR #22 (CI results and PRs #20-#21 recorded here; documentation only): merged 2026-10-26 as `531dedb6`; CI
  21/21 on head `36cfe527`.
- PR #23 (CI job count and status; documentation only): merged 2026-10-26 as `5a444a9a`.
- PR #24 (HCIS frontend foundation and Theme Development Kit): merged 2026-10-26 as `f6af207c`; the merge
  commit records CI 21/21 on head `71b93cad`, with DCO sign-off waived by the owner for that PR.
- PR #25 (final HCIS design review fixes; documentation reconciliation): merged 2026-10-26 as `ccab63ce`.
  The CI run on that merge commit (ci #80) failed; its cause was not determined (the run page shows no
  annotation). The next merge to `main` (PR #26) concluded success (ci #82).
- PR #26 (HCIS review follow-up: disclosure targets and signed-out title): merged 2026-10-26 as
  `4194396b`; main ci #82 concluded success.
- PR #27 (M2-2a, write-authorization framework): GitHub reports it merged (page fetched 2026-10-27:
  "KRDucky merged commit 457eddf into main", Oct 26). Its single commit `457eddf1` entered `main` through
  PR #28; there is no separate merge commit for it.
- PR #28 (M2-2b, service start/stop/restart, API and UI): merged 2026-10-26 as `a3447efa` (tree identical
  to the PR head `987ba011`). No evidence is linked in its description (other locations not verified) and no
  review was submitted on its page (review elsewhere not verified). Main ci #85 (run 40292176133) concluded
  success overall; see the M2-2 section above for what that does and does not establish.
- PR #36 (M2-3 expanded service management working skeleton): squash-merged 2026-11-06 as `a548f52f`.
  Working skeleton on `main`; not owner acceptance, not production, not KVM acceptance.
- PR #37 (M2-4 read-only storage inventory working skeleton): squash-merged 2026-11-06 as `058f4725`.
- PR #38 (OIDC login working skeleton): squash-merged 2026-11-06 as `7a121dbd`.
- PR #40 (M2-6 read-only host-settings skeleton; squash includes the M2-5 layout handoff labelled `#39`):
  squash-merged 2026-11-06 as `1f885993`. PR #39 has no separate first-parent commit on `main`.
- PR #41 (M2-6 host-settings apply backend working skeleton): squash-merged 2026-11-07 as `4c3c30a7`.
- PR #42 (M2-6 host-settings apply UI working skeleton): squash-merged 2026-11-07 as `90453f0a`.
- PR #43 (file-manager read-only confined listing working skeleton): squash-merged 2026-11-07 as
  `5da0ac90`.
- PR #45 (file-manager read-only UI, `/files`, `Files.tsx`): squash-merged 2026-11-07 as `519e1c52`.
- PR #46 (docs: FileBrowser port is on `/files`; ADR 0012 still Proposed): squash-merged 2026-11-07 as
  `8f8d918e`.
- PR #47 (STATUS first-parent reconcile and D-8 Theme/Plugin SDK): squash-merged 2026-11-07 as
  `9ffc69ba`.
- PR #50 (opt-in `/files` Playwright + axe script): squash-merged 2026-11-07 as `8bdc574a`.
- PR #48 (layout browser axe and mobile checks): squash-merged 2026-11-07 as `94874da5`.
- PR #49 (opt-in files Go browser fixture): squash-merged 2026-11-07 as `0e7abb59`.
- PR #51 (owner 2026-11-07 product-scope catalog fold): squash-merged 2026-11-07 as `4c183eab`.
- PR #52 (layout selected-bay contrast: inherit signal-ink on filled-button muted/tag): squash-merged
  2026-11-07 as `b290f9dc`.
- PR #53 (working-skeleton close: files Origin GET + host-settings Playwright, not acceptance):
  squash-merged 2026-11-07 as `9fc9d8af`.
- PR #54 (M3 read-only sample plugin SDK first checkpoint, not acceptance): squash-merged 2026-11-07 as
  `20ddc2b7`.

## M2 PR-1 (authorized 2026-10-21; D-1, D-6, D-7 approved): merged, read-only
- PR #11 (planning) merged 2026-10-21 as `6e0f6218`. Its last commit (the D-1/D-6/D-7 record and the D-12
  correction) missed the merge by a minute and was carried in PR-1b.
- **PR-1b (PR #12, merged 2026-10-21 as `04358bbf`):**
  - read-only `services.unit.logs` broker op (ADR 0008 Amendment 1), API
    `GET /api/v1/services/units/{unit}/logs`, scope `services.logs` (not granted by default);
  - logs panel on Services;
  - hash-chained broker and API audit logs with journald checkpoints and `hlctl audit verify`
    (tamper-evident only; `docs/security/AUDIT-LOG.md`);
  - fail-closed audit for broker results and log reads; `hlctl user scopes`;
  - a 60-second fuzz run of the `services.unit.logs` argument decoder on every CI run.
- **PR-1b CI:** run on `2a5e699a`, 20/20 green, including real systemd on 24.04 and 26.04.
- **PR-1a (PR #13, merged 2026-10-22 as `c012f299`; developed stacked on PR-1b):**
  - **State database:** `/var/lib/homelab-control/state.db`, using `modernc.org/sqlite` v1.60.1
    (ADR 0006 amendment 1; reviewed allow-list `backend/go-deps-allowlist.json`; `hc-broker` stays
    stdlib-only).
  - **Migrations:** transactional, with a backup before migrating and downgrade refusal.
  - **Persistent sessions (D-6):** hashes only, with own-session list and revoke.
  - **API audit:** hash-chained in the database; `hlctl audit verify --db` and `hlctl audit export --db`.
  - **New scope `audit.read`:** not granted by default (D-7).
  - **UI:** new Audit and Sessions pages.
  - **Release:** the source archive carries the Go module zips (`vendor-sources/go/download`) and builds
    offline from them.
  - **CI:** new `upgrade-ubuntu` job (v0.1.0-dev.1 → build).
- **Not in a release:** v0.1.0-dev.1 (tag → `ec34b21e`) predates M2 PR-1, M2-2 and the HCIS steps. No release has
  been published since.
- **Known limits:**
  - login throttling is still in memory;
  - no audit retention or rotation;
  - no admin management of other users' sessions.
- **Authorization at the time of PR-1:** no privileged host writes. Since 2026-10-23 M2-2 (service
  start/stop/restart, plan/confirm/execute, `root-exec`) is authorized; see the M2-2 section above.
  PAM/TOTP, storage configuration, Docker/VM management and Webmin migration remain unauthorized.
  D-8…D-12 were open at that date; D-8 was decided 2026-11-07 (`docs/m2/OWNER-DECISIONS.md`).

## HCIS UI migration (merged steps 1–3; each step is a separate, approved PR)
- **Design documents:** the HCIS design system (PR #14) and the font licensing policy (PR #16) changed no
  application code. Planned and Proposed components in `docs/ui/hcis/` are designs, not shipped features.
- **Step 1, colour tokens (PR #15):** generated HCIS colour tokens for Midnight, Daybreak, Graphite and
  OLED; the 12 M1 colour names alias them.
- **Step 2, typography (PR #17):** Inter and JetBrains Mono (OFL-1.1) are bundled and served from
  `hc-api`'s own origin, replacing Archivo. No font CDN.
- **Step 3, shell and appearance (PR #19):** responsive shell (sidebar, icon rail, drawer); appearance menu
  for theme, density, interface font, text size, surfaces and motion; scrolling tables as labelled,
  focusable regions. Preferences are stored per browser (`localStorage`) until the server settings store
  (`docs/ui/10-PHASED-PLAN.md`, phase U5).
- **Implemented in PR #24:** shared DataTable with responsive cards, status/live states, navigation-only command palette, browser-local `.hltheme` import/export and Theme Studio, and TDK. See the UI handoff above.
- **Still future:** server settings store, resource search, workspaces, dashboard widgets, jobs/notifications and module views requiring backend support.


## CI (`.github/workflows/ci.yml`)
- **Triggers:** push to `main`, every pull request, and a **manual run** (Actions tab → *ci* →
  *Run workflow*), which runs the same jobs with no inputs.
- **Coverage:**
  - lint, Go and Vitest tests;
  - security gate: govulncheck **v1.9.0**, npm audit, reuse, notices/DEP-5/SBOM drift, Webmin inventory, secret scan, shellcheck;
  - reproducible build;
  - Debian/Ubuntu `.deb` payload check with lintian. Fedora, openSUSE and Arch are not in this release;
  - install matrix on Debian stable/oldstable and Ubuntu 22.04/24.04/26.04;
  - real systemd on Ubuntu 24.04 and 26.04, configured to run the M2-2 service-operations scenario in
    short mode (the token and step-up expiry waits need `HC_M2_LONG=1`, which CI does not set);
  - browser e2e plus the theme CSP probe (Chromium, Firefox, WebKit), and the M2-2 service-operations
    dialog test configured for the same engines;
  - upstream Webmin Perl tests;
  - upgrade test from the published v0.1.0-dev.1 package (`upgrade-ubuntu`, PR #13) and 60-second fuzz
    runs of the log-argument (`FuzzLogsArgs`) and write-operation argument (`FuzzWriteArgs`) fuzzers;
  - release dry run through the same reusable workflow as the release (`release-build.yml`, exact
    toolchain pins): two runners, reproducibility comparison, fail-closed candidate verification,
    negative tests, release notes. The candidate is a Debian/Ubuntu `.deb` plus source, binaries, SBOM and notices.
- **Job count:** 21 check runs on every PR head from #13 to #22, all green. PR #12 had 20; the difference is
  the `upgrade-ubuntu` job added by #13 (compared by check-run name). Not re-counted for #25–#28.
- **Actions:** official `actions/*` releases on the node24 runtime, pinned by commit SHA, read-only token,
  no persisted git credentials, no dependency caches.

## License compliance and product independence
- **Release readiness: Phase 1 complete (2026-10-21).** LA-13 closed for Phase 1. First unsigned development
  prerelease **v0.1.0-dev.1** (tag → `ec34b21e`, release run 39562079197) published by the owner and verified
  after publication, including an independent rebuild outside GitHub (binaries and `.deb` byte-identical).
  Signing (Phase 3) and attestations (Phase 2) remain deferred; no stable releases or package repositories.
  Procedure: `docs/release/RELEASING.md`.
- Release **dry run** in CI: complete corresponding-source archive; binaries and the Debian/Ubuntu `.deb`
  built only from it; archive build = Git build; two independent runs compared file by file. The first
  release does not build or publish Fedora, openSUSE or Arch packages. Earlier RPM and Arch
  nondeterminism was found and fixed while those packages were still in the candidate. Not tested with
  other toolchain versions or distributions.
- **Webmin independence:** distributed Homelab Control artifacts (packages, binaries, release source
  archives) contain no Webmin code, enforced in CI. `compat/webmin/` stays an unchanged
  development/reference subtree until separately authorized migration work (phase 1 is approved for its
  own PR). There is no open choice about bundling Webmin.
- Licences: core **GPL-3.0-or-later**; **MIT** for SDK, schemas (incl. `schemas/openapi/`), theme
  definitions and design tokens; SPDX headers, `REUSE.toml`, `reuse lint` in CI; DEP-5 `copyright` in the
  `.deb`. `compat/webmin/` has a generated **per-file licence inventory** (not relicensed; 835 third-party
  files with no stated licence marked unverified) and is excluded from packages and source archives.
- Approved policies: `CONTRIBUTING.md` (DCO, AI disclosure; history grandfathered), "The Homelab Control
  Authors". Follow-ups: CI sign-off check, `SECURITY.md`, Webmin phase 1. (Archivo removal, LA-06, was superseded by HCIS step 2: bundled Inter and JetBrains Mono.)
- Browser-local theme import/export, Studio and TDK are implemented and merged (PR #24). Themes are colours only, with a fixed built-in base; preferences remain separate. Server-wide persistence remains future work.


## Working, verified (M1 / M2 PR-1 / M2-2 baseline) — M1 evidence in `docs/evidence/m1/README.md`; M2 PR-1 and HCIS records in `docs/project/09-PROJECT-MEMORY.md`

Later working skeletons on `main` (M2-3 through file-manager, OIDC) are described in the sections above.
They are not owner-accepted and are not added to this M1/M2-2 inventory.
- **hc-api** (unprivileged): sessions/CSRF/RBAC from M0; scopes `services.read`, `hardware.read` (M1) and
  `services.logs`, `audit.read` (M2 PR-1) and `services.operate` (M2-2), none granted by default; `/capabilities`, `/services`,
  `/hardware`, `/legacy/webmin`, `/services/units/{unit}/logs`, `/auth/sessions` (list, revoke own
  sessions), `/audit`, and for M2-2 `/services/units/{unit}/plan`, `/services/units/{unit}/apply`,
  `/auth/step-up` and `/operations`.
- **hc-broker** (root, socket-activated): peer-uid allow-list, strict framing, eight ops: four
  argument-free read-only (`broker.info`, `legacy.webmin.status`, `hardware.dmi.identity`,
  `services.systemd.list`), the argument-taking read-only `services.unit.logs` (PR #12), `services.unit.plan`
  (unprivileged, read-only) and `operations.journal.list` (read-only), and one write, `services.unit.apply`
  (start/stop/restart; `root-exec`); only `internal/broker/runner` executes programs, as
  `homelab-control`, or as root for the fixed `systemctl` templates of `root-exec`; capabilities limited
  to CAP_SETUID|CAP_SETGID.
- **UI**: capability-aware navigation; Services (with a logs panel and, for `services.operate`, start/stop/restart), Hardware, Legacy Webmin, Audit and
  Sessions pages; HCIS colours, fonts, responsive shell and appearance menu; degrades when broker is down.
- **Packaging**: CI-verified install → run → remove on Debian stable/oldstable, Ubuntu 22.04/24.04/26.04,
  Fedora, openSUSE Tumbleweed/Leap and Arch (containers, no systemd); package + hardened units under real
  systemd on Ubuntu 24.04 (runner VM) and 26.04 (systemd 259.5 container).
- **Guards**: exec only in runner; hc-api/hlctl never link broker internals; OpenAPI contract test.

## Owner decisions recorded
- **2026-10-16 (M1 closeout):**
  - ADR 0001 and 0003 accepted (direction).
  - ADR 0008 accepted for the four existing read-only ops only.
  - ADR 0005 accepted in principle at that point, with release blocked on a licence audit.
  - PAM deferred (`docs/security/PAM-AUTH-DEFERRED.md`).
- **2026-10-16 (compliance PR #5, later the same day):**
  - ADR 0005 accepted after the audit (GPL-3.0-or-later core; MIT SDK/schemas/themes/tokens).
  - ADR 0009 accepted with **mandatory** Sigstore and OpenPGP/hardware-token signing.
  - DCO policy approved.
  - Webmin excluded from release source archives.
- **2026-10-19:** ADR 0009 **revised**; this **supersedes** the 2026-10-16 signing requirement.
  - Phase 1: unsigned development/prereleases, published manually by the owner.
  - Phase 2: optional keyless Sigstore attestations.
  - Phase 3: OpenPGP/hardware-token signing, deferred.
  - No signing keys are current prerequisites.

- **2026-10-21 (M2 readiness):** M2 PR-1 authorized; D-1 (`modernc.org/sqlite`), D-6 (persistent sessions)
  and D-7 (new scopes not granted by default) approved. D-2…D-5 and D-8…D-12 were open at that date; D-2…D-5 were approved on 2026-10-23 (below);
  D-8 was decided 2026-11-07 (keep `docs/project/04-ROADMAP.md`; Theme/Plugin SDK is M3); D-9…D-12 remain open (`docs/m2/OWNER-DECISIONS.md`).
- **2026-10-21/22 (HCIS):** HCIS adopted as the UI design direction; density names, the OLED theme and the
  font licensing policy decided 2026-10-22. Migration step 1 approved 2026-10-22; steps 2 and 3 authorized
  2026-10-23 (`docs/ui/hcis/README.md`, project memory). The owner authorized the remaining frontend and TDK on 2026-10-25 (UI continuation above).

- **2026-10-23 (M2-2):** M2-2 authorized; D-2, D-3, D-4 and D-5 approved (option A each). ADR 0011
  accepted; ADR 0008 Amendment 2 (`root-exec`). Only service start/stop/restart; everything else in M2
  still needs its own authorization.

- **2026-10-27 (post-M2-2 documentation reconciliation; owner message "HomeControl — Documentation Reconciliation
  Decisions", 00:38 America/Chicago; quoted in `docs/m2/M2-2-ACCEPTANCE.md`):** acceptance item E2
  (workstation-VM browser validation) is **deferred, not waived**, and is required before `services.operate`
  is enabled in a real deployment; it was not accepted at that time. The decision names E2 only. M2-3 and
  later milestones remain unauthorized. Failed CI run #80 is recorded in the merged-work history only; its cause
  is unknown, and ci #85 then concluded success.

- **2026-10-27 (later owner validation instructions):** E2 accepted on the owner's Ubuntu KVM run; W1 owner review/sign-off accepted. Non-Debian validation was still deferred.
- **2026-10-29:** owner approved closing E2/E4 on the final combined-main KVM evidence and preparing this documentation PR. Production permission remains a separate decision; M2-3 remains unauthorized *as of that date*. See [decision register](m2/OWNER-DECISIONS.md).
- **2026-11-04:** M2-3 development and isolated testing authorized; owner acceptance and production
  activation remain separate (`docs/m2/OWNER-DECISIONS.md`, "M2-3 development authorization").
- **2026-11-05:** continuation through M2-4, M2-5 and M2-6 authorized for collaborative development and
  isolated tests. No owner design acceptance, KVM, production settings, or M3 inferred
  (`docs/m2/OWNER-DECISIONS.md`, "Continuation through M2-6").
- **2026-11-06/07 (first-parent on `main`):** working skeletons squash-merged as PRs #36, #37, #38, #40,
  #41, #42, #43, #45 and #46, then STATUS/D-8 #47, `/files` Playwright #50, layout axe #48, files Go
  fixture #49, catalog fold #51, and layout contrast #52 (SHAs in Merged work). Not M2 close. ADR 0012,
  ADR 0008 Amendment 4, and the ADR 0011 M2-6 extension stay **Proposed**.
- **2026-11-07 (ask_ui `m2-close-m3-d8`):** D-8 decided: keep `docs/project/04-ROADMAP.md`; M3 is Theme SDK
  + plugin SDK. Working-skeleton close is docs+tests (layout, `/files`, and host-settings preview
  Playwright/axe; this STATUS reconcile). Isolated `/files` and host-settings preview scripts are opt-in
  and not in CI. M3 Theme/Plugin SDK is authorized for isolated development after owner merge of that
  remaining fold. Not M2 close, not production, not KVM acceptance, not ADR acceptance.

## Verified on the owner's workstation VM (2026-10-16, product code `738fa37b`)
Ubuntu 26.04.1 KVM guest, systemd 259.5 (`dev/vm/`, evidence `docs/evidence/m1/vm-26.04/`):
systemd smoke + M1 checks **40/40** (hardening, socket-activated broker, auth, routes, host filesystems),
`systemd-analyze security` 1.6 (hc-api) / 1.5 (broker), rollback + Webmin coexistence **13/13**,
browser e2e **25/25**, axe WCAG 2.1 A/AA **0 violations** on 4 pages, final removal **4/4**.
CI run 38866773032 on the same commit: all 15 jobs green.

## Outstanding external validation (not verified)
- Debian and openSUSE M2-2 real-systemd validation; optional second browser engine B16; screen reader and Windows High Contrast checks.
- Historical CI job logs for the original M2-2 runs were not inspected. Later 21/21 job conclusions are recorded separately.
- P1 unloaded-unit inventory, H1/H2 VM tooling and optional dedicated SELinux confinement remain separate work.
- Upstream Webmin 2.680 not released; ~25 unreleased upstream security fixes since 2.670 — see
  `docs/security/WEBMIN-2.670-UPSTREAM-FIXES.md` (mitigations: RPC off, loopback bind, trusted admins).

## Security items that blocked M2 writes (status after M2-2)
Items 1, 3 and 4 are addressed for service start/stop/restart by ADR 0011 (plan tokens, revalidation,
locks, step-up, strict arguments); item 2 by the operation journal. The residual limitation of item 1
(a compromised `hc-api` can request any operation the broker's policy allows) is accepted and
documented in ADR 0011. E2/E4 VM validation is now closed by the owner on 2026-10-29. The original list is kept below for
reference.

1. **Broker trust:** a compromised hc-api can call every registered broker op. Safe today only because all
   five are read-only (four take no arguments; `services.unit.logs` takes validated ones). Write ops need per-operation plan/confirmation tokens bound to the
   user and target, target revalidation in the broker, exclusive locks and timeouts (ADR 0001/0008 follow-up).
2. **Audit durability (partly addressed):** PRs #12 and #13 merged a SQLite state store, persistent
   sessions (hashes only) and hash-chained, tamper-evident audit logs that fail closed for broker results
   and log reads. Still open: the login throttle is in memory, there is no audit retention or rotation,
   and no operation state exists. Writes need durable, ordered audit and operation state before the first
   privileged change; whether the merged work meets that is for the M2 security review
   (`docs/m2/M2-SECURITY-ACCEPTANCE.md`), not decided here.
3. **Step-up authentication:** no re-authentication or second factor before a privileged action; local
   accounts have no MFA.
4. **Argument-taking broker ops (partly addressed):** one exists, the read-only `services.unit.logs`
   (PR #12; ADR 0008 Amendment 1: strict decoding, validation in both tiers, fuzz test). Argument
   handling for write operations, PAM and secrets still needs its own reviewed design.
5. ~~Validation gap~~ — closed 2026-10-16: hardening and socket activation verified on the workstation
   KVM VM (Ubuntu 26.04).

The full blocker list, B1–B5, is in `docs/m2/M2-READINESS.md`; B5 (root child processes) is not repeated here.

No licence/release blocker remains for Phase 1 prereleases (LA-13 closed 2026-10-21). CI and releases build with the same exact Go version (1.26.11, `packaging/release/toolchain.env`); govulncheck is pinned (v1.9.0).

## Not done / limits
- Earlier M1 evidence (d1077e4a) was never committed and is withdrawn (see evidence README).
- Ubuntu 22.04 under real systemd: not verified (CI covers 24.04 runner and 26.04 container; VM covers 26.04).
- PAM login deferred; upstream pin 2.670; licence decided (ADR 0005: GPL-3.0-or-later core, MIT SDK/schemas/themes/tokens); one unsigned development prerelease, v0.1.0-dev.1, published 2026-10-21 (ADR 0009), and none since.
- The login throttle is in memory (sessions are persisted since PR #13); no TLS management; govulncheck runs in CI only (v1.9.0 on Go 1.26.11).
- No storage-configuration writes exist (M2-4 is read-only inventory). Privileged host-changing working
  skeletons on `main` are M2-2 start/stop/restart (scope `services.operate`), M2-3 boot-time enable/disable
  (scope `services.configure`), and M2-6 hostname/timezone apply (scope `host.settings.apply`). Those later
  skeletons are not owner-accepted, not production, and not KVM acceptance. File-manager listing is
  read-only (`files.read`). Homelab Control also writes its own state database, audit logs and operation
  journal. This is not M2 close.
