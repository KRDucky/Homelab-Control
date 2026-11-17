<!--
SPDX-FileCopyrightText: 2026 The Homelab Control Authors

SPDX-License-Identifier: GPL-3.0-or-later
-->

# M2-4 O-1 read-only storage probe: device access and MAC policy

Status: **Proposed, for independent review.** Owner approval (2026-11-17,
`docs/m2/OWNER-DECISIONS.md`, "Storage probe O-1") covers implementation and
isolated validation only. Nothing here is enabled, started, loaded or probed on
any host. Nothing wires a broker caller to the helper.

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
  `deny capability/network/mount/ptrace`, `ix`-only child exec, read-only device
  patterns per class, no `Ux`/`Pux`, no shell. Shipped as a conffile; loaded by
  the administrator in a validation VM.
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

## Evidence still missing (not claimed)

Run only in a disposable VM, never on a host:

- SELinux: `build-selinux-policy.sh` compile, load, AVC capture
  (`ausearch -m avc`), confirm `neverallow` assertions compile, confirm the helper
  transitions into `hc_storage_probe_t`, and `blkid`/`lvs`/`pvs` run in-domain.
  The source is uncompiled and untested; interface names (for example
  `init_rw_stream_sockets`) and the `blkid` file context are unverified.
- AppArmor: `apparmor_parser -Q` parse, enforce, `aa-logprof` review of denials.
- systemd: `$`-quoting, BPF device-policy enforcement (a write open of a block
  node must fail with EPERM; a read must succeed), `DeviceAllow` group
  resolution per distro, `@system-service` coverage for `blkid`/`lvs`, and
  confirmation that a missing LSM policy makes the instance fail.
- Evidence that unresolved groups are ignored on the target systemd versions.
- The helper's startup self-check (hc-go) and the broker caller are not
  implemented here.
