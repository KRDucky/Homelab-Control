<!--
SPDX-FileCopyrightText: 2026 The Homelab Control Authors

SPDX-License-Identifier: GPL-3.0-or-later
-->

# M2-4 O-1 read-only storage probe: device access and MAC policy

Status: **Implemented and integrated on the isolated readiness branch; disabled
by default and not accepted for host or production activation.** The owner
approved implementation and isolated validation (2026-11-17,
`docs/m2/OWNER-DECISIONS.md`, "Storage probe O-1"). The helper was started and
its SELinux policy loaded only in a disposable Fedora 44 VM. The typed broker
caller and explicit CSRF POST are wired; packages do not enable or load the
helper.

## What ships

| File | Purpose |
|---|---|
| `packaging/common/homelab-control-storage-probe@.service` | One helper instance per accepted connection |
| `packaging/common/homelab-control-storage-probe.socket` | `Accept=yes`, `MaxConnections=1`, root-only socket |
| `packaging/fedora/selinux/homelab_control_storage_probe.{te,fc}` | SELinux module source (compile-only; never loaded by packages) |
| `packaging/fedora/selinux/build-selinux-policy.sh` | Compile helper for the validation VM |
| `packaging/debian/apparmor/usr.lib.homelab-control.hc-storage-probe` | AppArmor profile (conffile; not loaded by scripts) |
| `tests/packaging/storage-probe-policy-check.sh` | Fail-closed static guard, run by `make lint` |
| `backend/internal/storageprobe/unit_test.go` | Go-level guard of the same unit keys |

## Disabled by default (three independent gates)

1. No `[Install]` section, no `WantedBy=`, no preset: no package script enables
   or starts the socket or the service.
2. `ConditionPathExists=/etc/homelab-control/storage-probe.enabled` on both
   units. No package ships that marker; an administrator creates it in a
   disposable validation environment. Without it, starting the socket or the
   service is a silent no-op.
3. `RefuseManualStart=yes` on the service; instances exist only through the
   socket.

The guard fails if any package script references the marker, enables, starts or
presets the helper, or installs it under `libexec`.

## Device policy

```
PrivateDevices=no
DevicePolicy=closed
DeviceAllow=block-sd r
DeviceAllow=block-blkext r
DeviceAllow=block-virtblk r
DeviceAllow=block-md r
```

- `PrivateDevices=no` is necessary: `PrivateDevices=yes` builds a private `/dev`
  without physical disks, and a static unit cannot bind per-device nodes (the
  device set is only known at request time).
- `DevicePolicy=closed` allows only null, zero, full, random, urandom plus the
  `DeviceAllow=` entries. Mode is `r` only: no `w`, no `m` (no `mknod`).
- No `char-*` groups, no explicit `/dev` paths, no wildcards, no loop, nbd,
  zram, ram, sr, fd, rbd, device-mapper, mmc or xen groups.
- No capabilities at all: empty `CapabilityBoundingSet=` and
  `AmbientCapabilities=`, so no `CAP_SYS_ADMIN`, `CAP_SYS_RAWIO`, `CAP_MKNOD`,
  `CAP_DAC_*`. `@raw-io`, `@mount`, `@module`, `@resources` and other groups are
  denied by `SystemCallFilter=` (EPERM).
- Identity: `DynamicUser=yes` with `SupplementaryGroups=disk` as its only
  group. Block nodes are `root:disk 0660`, so group membership alone would also
  permit writes; **the cgroup device policy is the only write barrier.** A
  read-only mount of `/dev` is not a barrier and is not used.
- **Implicit grants.** systemd adds `/dev/loop-control` and `block-loop`/
  `block-blkext` with `rwm` when `RootImage=`, `MountImages=` or
  `ExtensionImages=` is set (systemd.exec(5)). The guard bans all three, plus
  `RootDirectory=`, `BindPaths=` and other keys outside a closed allow-list, so
  the effective policy equals the four lines above. A later assignment cannot
  override an earlier one: every key except `DeviceAllow`/`SystemCallFilter` must
  be assigned exactly once, and `DeviceAllow` must equal the reviewed set with
  no duplicates and no empty reset.

### Reviewed block classes

| Group | Covers |
|---|---|
| `block-sd` | SCSI/SATA/SAS **and USB-attached storage** (disks and partitions) |
| `block-blkext` | NVMe namespaces and extended-minor partitions |
| `block-virtblk` | virtio-blk disks (virtual machines) |
| `block-md` | Linux software RAID arrays |

## Residual privilege (read this before approving)

A compromised or buggy helper can read the **raw contents of every block device
in these classes**: the system disk, data disks, **USB and other removable disks
in the `sd` class**, and RAID members. Raw reads bypass file permissions and
file-level secrets (`/etc/shadow`, keys, databases) on those disks are readable.
The cgroup policy filters by device class (major number), not by device
identity, so **it does not exclude USB, removable or non-eligible devices**.
Skipping them, per-device timeouts and the `by-id` enumeration are helper
behaviour; they do not constrain a compromised helper. Device-mapper (including
LVM LVs and LUKS mappings) is *not* granted; but their backing physical devices
are, when they are in these classes.

What bounds it: read-only (no `w`/`m`); no network (`PrivateNetwork=yes`,
`IPAddressDeny=any`, AF_UNIX only); no capabilities; no new privileges; no
writable filesystem except a private tmp; hc-api secrets and state masked; one
connection and 64 MiB / 16 tasks / 30 s; a dedicated MAC domain; explicit POST
under a non-default scope with count-only audit.

### Deviation from ADR 0008 Amendment 7 (owner decision gap)

Amendment 7 asked for `DeviceAllow` of *named by-id nodes resolved at plan*. A
static, socket-activated unit cannot express that. Narrower alternatives need a
more specific design and are **not** implemented here:

- **Per-request transient unit** (`systemd-run`-style) with `DeviceAllow=/dev/...`
  for exactly the requested nodes. Needs broker access to the systemd manager,
  i.e. a new privilege path that needs its own ADR 0011 review.
- **FD passing** from a broker that opens the nodes. External `blkid`/`lvs` would
  re-open via `/proc/self/fd`, which is again subject to the device policy; this
  is unverified and would require a new helper code path.

**Decision recorded.** The class-level static policy is accepted **only for
isolated validation**, under the owner decision "O-1 decision updates to ALL
AHEAD GO" (2026-11-17), which `docs/m2/OWNER-DECISIONS.md` records as approval
for implementation and isolated validation, not host activation. Before any host
or production activation a narrower design (per-request transient unit, fd
passing, or another reviewed by-id model) **and a separate owner authorization**
are required. Until both exist, host activation stays blocked and the unit stays
disabled by default.

## Startup self-check (implemented in helper; runtime gate remains)

systemd adds a `DeviceAllow=` group only if it is listed in `/proc/devices` when
the unit starts; an unlisted group is silently ignored. That can only *narrow*
access (never widen it), but a quietly smaller allow-list must not look like an
empty inventory. An `ExecStartPre=grep` was considered and **rejected**: every
Exec line runs inside the helper's SELinux domain / AppArmor profile, which
deliberately cannot exec `grep` or a shell (no `bin_t`/shell shortcuts), so the
unit would never start; the guard therefore bans every Exec line except
`ExecStart`. The check belongs in the helper, before any probing:

1. Read `/proc/devices` (block section).
2. Require **every** reviewed group (`sd`, `blkext`, `virtblk`, `md`) to be
   listed. If any is missing, return the typed
   `device_policy_unresolved` refusal and probe nothing. In a validation VM
   load the matching drivers (`virtio_blk`, `md_mod`, ...).
3. A device whose open fails with EPERM/EACCES is reported as `denied`
   (partial result), never as an empty inventory.

The helper implements this strict-all policy and returns the typed
`device_policy_unresolved` refusal before probing when any group is missing.
Synthetic tests cover the refusal; behavior against target systemd/kernel
combinations remains unverified.

## MAC policy

`SELinuxContext=system_u:system_r:hc_storage_probe_t:s0` and
`AppArmorProfile=hc-storage-probe` are set in the unit. Each is ignored where
that LSM is inactive. Where it is active and the policy is not installed, the
instance fails to start: **fail closed, no unconfined fallback.** Package
scripts never load policy.

- **SELinux (Fedora family).** Dedicated type `hc_storage_probe_t` with a
  labelled entrypoint `hc_storage_probe_exec_t` for
  `/usr/lib/homelab-control/hc-storage-probe` (not `lib_t`, `bin_t`,
  `libexec`). Explicit `blk_file { getattr open read ioctl }` on
  `fixed_disk_device_t`; `neverallow` for write, `sys_admin`, `sys_rawio`,
  `mknod`, `dac_override`, mount and network. `process2
  { nnp_transition nosuid_transition }` for `NoNewPrivileges`; child tools
  (`blkid`, `lvs`, `pvs`) run in the same domain (`can_exec`) because NNP blocks
  domain transitions. Compile-only: `build-selinux-policy.sh` never loads the
  module. `removable_device_t` is intentionally not granted.
- **AppArmor (Debian/Ubuntu).** Profile `hc-storage-probe`, enforce mode,
  `deny capability/mount/ptrace`, per-family `deny network` (inet, inet6,
  netlink, packet, bluetooth, vsock) with the inherited AF_UNIX connection
  allowed by an explicit `unix` rule, `ix`-only exec of `blkid` (LVM metadata is
  read natively; `lvm`/`lvs`/`pvs` are not executable), read-only device
  patterns per class, no `Ux`/`Pux`, no shell. Shipped as a conffile and not
  loaded by package scripts; Debian's `apparmor.service` loads it at boot, or
  an administrator loads it by hand.
  **Root cause of the Debian 13 failure (found 2026-11-17):** the first profile
  had a blanket `deny network,`. In AppArmor a rule without a family matches
  every family including AF_UNIX, and a `deny` overrides the later `unix (...)`
  allow, so the confined helper was denied `recvmsg` on its own systemd socket
  (audit: `operation="recvmsg" class="net" family="unix" denied_mask="receive"`)
  and exited silently without even reaching journald. `apparmor_parser -Q -T`
  accepts both the broken and the fixed profile, so a parse check cannot catch
  it; `tests/packaging/storage-probe-policy-check.sh` now rejects any blanket
  `network,` rule and requires the per-family denies, the `unix` rule and the
  `@{PROC}/@{pid}/cgroup r,` read (the Go runtime reads it; denied otherwise).
- **Arch / openSUSE / others.** No MAC policy is shipped. On those distros the
  helper stays unsupported for any activation until a policy exists; the unit
  still ships disabled. (AppArmor on openSUSE and SELinux on Arch are not
  covered.)

## Checks

```
bash -n tests/packaging/storage-probe-policy-check.sh
tests/packaging/storage-probe-policy-check.sh      # also runs 67 mutation self-tests
make lint                                          # includes the guard
cd backend && go vet ./internal/storageprobe/ ./internal/guard/ && go test ./internal/storageprobe/ ./internal/guard/
git diff --check
```

## Fedora 44 VM validation (2026-11-17)

The exact source at commit `ce76000d` plus the recorded SELinux syntax fix was
tested in a disposable Fedora 44 guest (kernel 6.19.10, systemd 259.10,
SELinux targeted policy 44.11, enforcing). Evidence and checksums are preserved
under `/mnt/tank/claude-vms/artifacts/hc-m24-o1-ce76000d/` and
`/mnt/tank/claude-vms/artifacts/hc-m24-o1-ce76000d-policy-retest2/`.

- `systemd-analyze verify` passed for the socket and template. The socket is
  static/preset-disabled; without the marker it does not listen, and a manual
  template start is refused.
- On the attached synthetic virtio disk only, the cgroup device policy allowed
  reads and returned `EPERM` for write and read-write opens. Omitting the
  `virtblk` allow group or using an empty allow-list denied reads as well.
- The helper's strict `/proc/devices` check passed with the required groups;
  fixtures missing `virtblk`, `md` or `blkext` returned typed
  `device_policy_unresolved` before probing. A real socket request read blkid
  metadata from the synthetic disk and reported LVM as disabled/partial.
- The source SELinux module initially failed to compile. The fixed source now
  compiles with `build-selinux-policy.sh`, installs in enforcing mode, and the
  helper runs in `hc_storage_probe_t` against the synthetic disk with no AVCs.
  Policy inspection found no block-device write permission and no capabilities.
  Negative checks denied block writes and `dac_override`/`dac_read_search`.
- LVM config syntax and read-only invocation passed against the synthetic PV/LV
  outside the unit sandbox; `strace` showed no successful write opens and
  `/etc/lvm` and `/run/lvm` did not change. **Inside the actual DynamicUser unit,
  `lvs`/`pvs` could not access `/dev/mapper/control` and returned unusable
  reports. LVM stays disabled.**
- In `systemd-nspawn`, the outer read-only device policy blocked writes only
  when the payload used `--keep-unit`; without it, the payload landed in
  `machine.slice`. The inner unit's device policy was not enforced in that
  container. This does not establish supported container behavior.
- Fedora has no AppArmor LSM. AppArmor was exercised separately on Debian 13
  (below).
- **Debian 13.7 KVM (`/mnt/tank/claude-vms/artifacts/m24-apparmor-debian13-20261117/REPORT.md`,
  kernel 6.12.107, systemd 257, apparmor_parser 4.1.0).** The shipped profile
  parsed (`-Q -T -r` rc 0) and loaded in enforce mode, but the helper failed
  closed and silently because of `deny network,` (root cause above). A variant
  with per-family denies plus the cgroup read, kept outside the repo, ran
  confined in enforce mode against a synthetic virtio disk with no denials and
  returned blkid metadata. The same run showed: the unit refuses
  (`device_policy_unresolved`) when `sd`/`md` are not registered in a
  virtio-only guest (load `sd_mod md_mod` first), the instance fails to start
  when the profile is not loaded (change_onexec "label not found"), and the
  confined helper cannot read `/etc/homelab-control` or exec `/bin/sh`. A write
  open of the synthetic disk got EPERM from the cgroup policy.

The VM was shut down gracefully with autostart disabled. Its overlay, synthetic
disk, source copies and logs remain preserved. The original evidence manifest
had a stale checksum for the pre-shutdown guest-state log; this is documented in
`t9d-finalization-note.txt`, and a corrected current manifest is
`t9e-evidence-current.sha256`.

## Remaining gates (not accepted)

- LVM device-set identity remains path-based; more importantly, real LVM output
  is unusable inside the service sandbox. Keep `LVMEnabled` false until a
  reviewed sandbox design and synthetic-guest verification resolve this.
- AppArmor: the profile in this branch is the Debian 13 test variant plus two
  least-privilege removals (the `lvm` exec rule and the unused private-tmp
  rules), which are not yet re-run in a guest. Still required in a disposable
  Debian 13 (and Ubuntu LTS) guest: re-run the confined probe with this exact
  file, `AF_INET`/`AF_NETLINK` denial from inside the profile, partition and
  md/NVMe nodes, and no `apparmor="DENIED"` entries beyond the expected
  confinement checks.
- The `systemd-nspawn` result is partial; the inner policy is not enforced in
  that container configuration. Supported-container fail-closed behavior is
  not established.
- No real SATA/SAS/NVMe vendor hardware, USB/removable media, or production
  host was probed. CI and package-build evidence across supported distributions
  is still required.
- The class-level static device grant is authorized for isolated validation
  only. A narrower by-id access design and a separate owner decision are still
  required before any host or production activation.
- Fedora's base SELinux policy lets the helper domain read some `etc_t` files,
  including `/etc/homelab-control/hc-api.env`; the helper policy no longer adds
  `files_read_etc_files`. A VM check confirmed that the service's exact
  `InaccessiblePaths` setting denies access to the environment file both with
  and without the helper SELinux domain. The static policy guard requires this
  path mask. This result is specific to the tested Fedora 44 unit and does not
  establish behavior on other systemd versions.

## Debian/Ubuntu AppArmor validation plan (disposable guest only)

Run inside a disposable Debian 13 (and an Ubuntu LTS) KVM guest with a
synthetic virtio disk; never on a host. The existing guest
`hc-m24-apparmor-debian13-20261117` (shut off, autostart disabled) can be
reused. Copy this branch's files into the guest; do not install anything on the
host.

1. Parse: `apparmor_parser -Q -T -r packaging/debian/apparmor/usr.lib.homelab-control.hc-storage-probe` (rc 0). A parse alone does not prove the network
   rules; step 4 does.
2. Load in enforce mode: install the file to `/etc/apparmor.d/`, then
   `apparmor_parser -r <file>` and `aa-status | grep hc-storage-probe` (enforce).
3. Prepare the guest: `modprobe sd_mod md_mod virtio_blk`; `touch
   /etc/homelab-control/storage-probe.enabled`; start
   `homelab-control-storage-probe.socket`; send one request for the synthetic disk.
   Expect blkid metadata, `lvm` reported disabled/partial by design, and no
   `apparmor="DENIED"` lines in `journalctl -k` other than the checks below.
4. Network: from inside the profile attempt `AF_INET`, `AF_INET6` and
   `AF_NETLINK` socket creation (for example with a tiny test binary labelled
   by a copy of the profile) and confirm denial; confirm the unix connection
   still works.
5. Confinement: reads of `/etc/homelab-control/*`, `/etc/hostname` and exec of
   `/bin/sh` are denied; write opens of the synthetic disk fail (EPERM from the
   cgroup policy, EACCES from AppArmor).
6. Fail closed: `apparmor_parser -R <file>` then start an instance; it must fail
   with change_onexec "label not found" and the helper must not run.
7. Cleanup: remove the marker, stop the socket, unload the profile, shut the
   guest down gracefully (autostart stays disabled). Keep the audit log as
   evidence.
