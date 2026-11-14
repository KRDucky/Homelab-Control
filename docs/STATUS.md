# Current status — M2 working skeletons and M3 plugin SDK first checkpoint on main (2026-11-07); not accepted

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
`os.Stat` of well-known `/usr/sbin` then `/usr/bin` paths, no `os/exec`) and kernel ZFS from bounded
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
create, import, export, destroy, or `zpool`/`zfs` execution exists. This is a checkpoint, not M7
acceptance, not a merge, not production. Never touch production pool `tank`.

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

## M8 first checkpoint: MergerFS + optional SnapRAID (read-only; not M8 acceptance)

One read-only page at `/storage/mergerfs` (module id `mergerfs`, scope `mergerfs.read`, never default
and implied by nothing) probes the required `mergerfs` and `fusermount3` (package `fuse3`) binaries
and optional `snapraid`, `smartctl`, `dumpe2fs`, `xfs_info`, and `btrfs` (unprivileged `os.Stat` of
well-known paths, no `os/exec`, no `packages.installed`, never those binaries run, never a disk
opened or formatted), plus kernel FUSE from bounded
`/proc/filesystems` (never `/dev/fuse`, never modprobe) and kernel release from bounded
`/proc/sys/kernel/osrelease` (visible to `mergerfs.read` without `system.read`; never `uname`, never
`mergerfs --version`; unread is not degraded). FUSE3 (`fusermount3`, package `fuse3`) is the
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
as is fewer than two content files. Content files are not read, and a missing mount is not inferred.
Writes between SnapRAID syncs are not protected; this page does not know the last sync time.
The warning list is capped (64 total, 8 per pool) with `warningsTruncated` when the cap or
collect deadline hits. Each root-owned live pool branch (`user_id=0` in mount super-options)
is measured with `fstatfs` of the directory under the collect root (`os.OpenRoot`, directory,
1.5s budget, 8 workers, 8 parked goroutines, 64 per pool / 64 total). `/proc`, `/sys`, `/dev`,
the fused mount (any observed pool mountpoint), fstab/systemd paths, and listed SnapRAID
paths are not opened as such; a data disk that is also a branch is measured as a branch.
A last-component symlink is unread. Capacity timeout degrades usage and does not 503 the
inventory. `availBytes` is unprivileged free (`bavail`). A missing measurement is not zero
capacity. Zero-byte measured filesystems still include the numbers.
No pool create, mount, policy write, fstab/unit write, or
snapraid sync/diff/fix path exists; there are no broker ops. This is a checkpoint, not M8 acceptance,
not a merge, not production.

## P0 storage shared (development; not production, not ADR 0014)

`backend/internal/storagecap` is the shared P0 probe, install-plan preview, discovery gate,
identity/extra-confirm types, and SHARED-02 operation phases (`Advance`). Probe is still
`os.Stat` of well-known paths only. Install plans set `executes: false` (Software Center
`/software?q=` only). The MergerFS install offer opens a review dialog (`executes: false`); it
never POSTs a package. `mkfs.ext4` is on main unused (PR #74); no broker op calls it.
`Advance` confirm now requires observed block-device preflight bound to the planned
device identity (each target once, including the same rdev under two keys or in
`PlannedRdev`,
block device, canonical major:minor rdev with major ≥ 1 matching the plan, not
mounted including partitions of the disk, no holders) and still never enters
execute: no storage apply op. An unused closed storage helper unit is packaged
(G3 placeholder: no `[Install]`, `PrivateDevices=yes`, no `DeviceAllow`, empty
caps, `ExecStart=/bin/false`, `RefuseManualStart=yes`). The main broker unit is
unchanged (`PrivateDevices=yes`). This is not owner O-1 (no live device access)
and not execute wiring. Preflight facts are
caller-supplied and untrusted; this package does not stat devices. ADR 0011 apply
unused here. Independent security review is required before execute wiring. `storageinfo.OverlapClaims` flags two unrelated stacks on one block node
(layered LVM/md/crypt plus a matching filesystem is one stack). Collect still does not read
`/dev/disk/by-id` (`PrivateDevices=yes`). Default is read-only. No storage mutation, no host
package install. MergerFS tooling JSON is unchanged (types aliased). Not P0 complete, not M8,
not production. Not ready for write tests. This does not make the storage write
path ready for guest or host write tests.

## M2-3: expanded service management (development authorized 2026-11-04; working skeleton on main, not accepted)

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
  owner approval (not decisions); P-7 objective and P-8 approach approved, implementations for review; D-10
  open; KVM acceptance not authorized.
- **Still not authorized:** mask/unmask, unit editing or creation, daemon-reload, and other privileged writes
  outside the working skeletons listed below. M3 product, acceptance, and production are not granted;
  isolated Theme/Plugin SDK development is authorized after skeleton closeout (ask_ui `m2-close-m3-d8`).
  M2-4, M2-5 and M2-6 development and isolated testing were authorized on 2026-11-05
  (`docs/m2/OWNER-DECISIONS.md`, "Continuation through M2-6"); their working skeletons are on `main`
  (below). Owner acceptance, production activation, and M2 close remain pending.

## M2-4: read-only storage inventory (development authorized 2026-11-05; working skeleton on main, not accepted)

Working skeleton on `main` as PR #37 (`058f4725`, 2026-11-06). Read-only storage discovery and ownership
graph (`GET /api/v1/storage`, scope `storage.read`, never default). No storage-configuration writes.
Scope notes: [`docs/m2/M2-4-STORAGE-SCOPE.md`](m2/M2-4-STORAGE-SCOPE.md). That document still records a
native LVM/signature collector as pending and fixture-tested parsers only; this STATUS update does not
claim live RAID/cache completeness, distro coverage, or KVM acceptance. Not M2 close.

## M2-5: manual drive-layout skeleton (development authorized 2026-11-05; working skeleton on main, not accepted)

Stage A manual chassis/bay layout in app state (no host writes, no new broker op). D-12 (HBA Viewer
reuse) remains open. Contract: [`docs/m2/M2-5-STAGE-A-CONTRACT.md`](m2/M2-5-STAGE-A-CONTRACT.md). The
slice entered `main` through PR #40 (`1f885993`, 2026-11-06), whose squash includes the M2-5 handoff
commit labelled `#39`. There is no separate first-parent commit for PR #39. Not accepted, not production,
not KVM acceptance, not M2 close.

## OIDC login (working skeleton on main, not accepted)

PR #38 (`7a121dbd`, 2026-11-06) on `main`. One configured OpenID Connect provider can sign in to an
existing local account; scopes stay on the local account. Notes: [`docs/security/OIDC-SSO.md`](security/OIDC-SSO.md)
(not tested against a real provider; not accepted).

## M2-6: hostname and timezone apply (development authorized 2026-11-05; working skeleton on main, not accepted)

Owner-authorized collaborative development of hostname (static and pretty) and timezone apply. On `main`:
read-only skeleton PR #40 (`1f885993`, 2026-11-06); apply backend PR #41 (`4c3c30a7`, 2026-11-07);
apply UI PR #42 (`90453f0a`, 2026-11-07). ADR 0011 `POST /host/settings/plan` and `/apply` (scope
`host.settings.apply`, never default) with closed `hostnamectl`/`timedatectl` templates. Preview stays
non-executing and never posts plan or apply. ADR 0008 Amendment 4 and the ADR 0011 M2-6 extension remain
**Proposed**. Isolated fixtures only; no production or retained-host apply. Opt-in Playwright+axe
(`TestHostSettingsBrowserFixture`, `tests/e2e/host_settings_browser.py`) is preview-only against a copy of
`testdata/hostsettings`; it has no broker and never runs `hostnamectl` or `timedatectl`. Not in CI. This is
a working skeleton, not M2-6 acceptance, not production, not KVM acceptance, and not M2 close. Working notes:
[`docs/m2/M2-6-HOST-SETTINGS-SKELETON.md`](m2/M2-6-HOST-SETTINGS-SKELETON.md).

## File manager: read-only listing (development authorized 2026-11-07; working skeleton on main, not accepted)

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
  - DEB/RPM/Arch payload checks with lintian;
  - install matrix on Debian stable/oldstable, Ubuntu 22.04/24.04/26.04, Fedora, openSUSE Tumbleweed/Leap and Arch;
  - real systemd on Ubuntu 24.04 and 26.04, configured to run the M2-2 service-operations scenario in
    short mode (the token and step-up expiry waits need `HC_M2_LONG=1`, which CI does not set);
  - browser e2e plus the theme CSP probe (Chromium, Firefox, WebKit), and the M2-2 service-operations
    dialog test configured for the same engines;
  - upstream Webmin Perl tests;
  - upgrade test from the published v0.1.0-dev.1 package (`upgrade-ubuntu`, PR #13) and 60-second fuzz
    runs of the log-argument (`FuzzLogsArgs`) and write-operation argument (`FuzzWriteArgs`) fuzzers;
  - release dry run through the same reusable workflow as the release (`release-build.yml`, exact
    toolchain pins): two runners plus Arch, reproducibility comparison, fail-closed candidate verification,
    negative tests, release notes.
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
- Release **dry run** in CI: complete corresponding-source archive; binaries, DEB, RPM and Arch packages
  built only from it; archive build = Git build; two independent runs compared file by file. RPM and Arch
  nondeterminism found and fixed. Not tested with other toolchain versions or distributions.
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
