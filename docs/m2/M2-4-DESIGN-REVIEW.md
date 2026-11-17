# M2-4 read-only storage design review (implementation integrated; acceptance open)

**Status update, 2026-11-17:** The O-1 helper, typed broker operation, explicit
CSRF-protected `POST /api/v1/storage/probe`, non-default `storage.probe` scope,
count-only audit, broker-side rate limit, OpenAPI contract, and UI action are
implemented on the isolated readiness branch. LVM remains disabled by default
because its device identity binding is not yet proven. The separate helper unit
and MAC policy are disabled by default and limited to isolated validation. This
does **not** close M2-4 acceptance: systemd/device-policy, SELinux/AppArmor,
real LVM output, KVM/container and supported-distribution evidence remain
outstanding. Older proposal text below records design history; this update and
the final repair notes supersede conflicting claims that the route, broker
client, startup check, or policy are absent.

Owner authorized M2-4 development and isolated tests on 2026-11-05, including LVM2 RAID/cache visibility and Serial/ID versus WWN labels. This is not storage-write, KVM or production-activation authorization. Existing file-manager and M2-3 worktrees are separate.

## Implemented boundary

The API's `storage.read` route reads bounded fixed sysfs/proc paths as the API service user, never block contents or mountpoints, and audits access before releasing identifiers. Requests supply no paths or executable arguments. Partial graphs explicitly cannot establish that a device is unused. Display preference is independent of persistent identity; duplicated serials remain ambiguous even with a unique WWN. Runtime kernel device numbers are observation-local keys, not write targets.

## Application-owned snapshots

Schema migration 2 adds identifier-bearing graph snapshots to the existing private SQLite database, without changing released migration 1. Before migration, the existing consistent backup mechanism remains in force; migration retry now uses a random suffix so an earlier recovery copy is not overwritten or a same-second retry blocked. Migration failure rolls back and old state remains readable. Old binaries refuse the newer schema; restore a verified pre-migration backup to downgrade, rather than editing schema version records.

Snapshots are bounded to 8 MiB each and the four latest observations. Insert and retention run in one transaction. Unknown or different boot IDs, future timestamps, and expired observations are stale; no historical graph is promoted to current after a collection failure. Saving app state is not a device write. Deleted SQLite rows are not claimed securely erased; retention bounds live records, not WAL/backup disk consumption. API responses distinguish persisted snapshots from ephemeral mode and withhold data if an enabled snapshot store fails. No snapshot-history endpoint exists yet.

## Native probe privilege review — original proposal (superseded by O-1 below)

The current root broker has `root-read` handlers and an unprivileged program runner. Its `root-exec` handle is explicitly confined to reviewed service write operations. Do not simply run `blkid` or `lvs` through that write handle from a new read op, add the API account to `disk`, or increase broker capabilities.

Proposed next slice: a separately typed, closed root-read-exec handle, made available only to individually registered `storage.read` ops. It must expose only fixed reviewed probe/report templates, scrub environment, bound output/time, reject generic execution and receive only broker-enumerated/revalidated block identities, never request paths. Keep service write templates/authorization and capabilities unchanged. Explicitly review device-node ownership and read requirements under existing hardening/PrivateDevices before claiming it works.

`blkid` must probe only enumerated/revalidated local block devices and never use or trust a stale cache as current proof. Avoid unbounded all-host scans or opening network filesystems. LVM reports must be non-mutating, with fixed fields/JSON/byte units, no caller config/profile/selection, and capability detection per installed version. LVM reporting may ordinarily update hints/caches or require locks; confirm an actually read-only invocation and distinguish metadata-only from live-kernel RAID/cache health. Do not infer health when the chosen read-only mode cannot observe it. Parser tests alone do not settle this security or compatibility review.

This paragraph describes the state before O-1 implementation. The later O-1
sections document the current implementation. SSD cache, LVM RAID/PV member
joins, ZFS/Btrfs/mergerfs and acceptance evidence remain incomplete.

## Confirmed native-probe integration constraint (2026-11-05)

The packaged broker service explicitly has `PrivateDevices=yes`, a strict system-service syscall filter and only SETUID/SETGID capabilities. A new argv template by itself cannot expose physical disks inside that service's device namespace. This is a real deployment constraint, not a reason to relax the existing service. [systemd's execution documentation](https://www.freedesktop.org/software/systemd/man/257/systemd.exec.html) describes the isolated device view and device policy.

A metadata-only LVM report cannot substitute for observed live health: [the upstream lvs manual](https://man7.org/linux/man-pages/man8/lvs.8.html) documents that `--readonly` does not communicate with device-mapper. Requesting RAID/cache counters in that mode must not invent runtime values.

This was the pre-O-1 recommendation. The implementation now follows the separate-helper design and reviewed device policy; it remains disabled by default. Runtime activation is limited to disposable validation, and acceptance still requires the evidence listed below.

Kernel md/device-mapper UUID observations can be collected safely from sysfs independently. UUID joins to LVM reports follow the upstream [UUID builder](https://github.com/lvmteam/lvm2/blob/main/libdm/libdm-string.c) and [layer rules](https://github.com/lvmteam/lvm2/blob/main/lib/misc/lvm-string.c); layer-suffixed mappings are not guessed to be the top LV. This is independently written format handling, not copied upstream implementation.

## Review outcome and remaining owner decisions (Claude, 2026-11-05)

Accepted from the independent review (`.dev` review ledger, not in Git): keep the existing broker unit unchanged; any native probe is a **separate, disabled-by-default unit** with a closed typed protocol. Verified from kernel source: device-mapper status ioctls require `CAP_SYS_ADMIN` (`drivers/md/dm-ioctl.c`, `ctl_ioctl`). Proposed phasing, for owner decision:

1. **Phase 1 (no new privilege, implemented):** unprivileged sysfs identity, md runtime state, and parsers for metadata-only `lvs`/`pvs` reports. Production hc-api attaches no LVM report today.
2. **Phase 2 (owner-approved O-1, implementation and isolated validation only):** a separate read-only probe unit running `lvs`/`pvs --readonly` (hints disabled by a fixed in-template config) and `blkid -p -c /dev/null` on enumerated, revalidated devices. Target restrictions: `PrivateDevices=no` with `DevicePolicy=closed` and `r`-only `DeviceAllow=` for explicitly reviewed block-device classes, empty capability bounding set, no character devices or `CAP_SYS_ADMIN`, `PrivateNetwork=yes`, no `[Install]` enablement, per-device child timeouts, skipping suspended dm, multipath, nbd/rbd/iSCSI, sr, fd, removable and USB devices, and explicit POST only (blkid wakes standby disks). It needs its own SELinux/AppArmor policy and KVM/systemd-container evidence. Owner approval does not authorize enabling it on a host or production deployment.
3. **Phase 3 (owner decision O-2):** live dm status for LVM RAID/cache health. Requires `CAP_SYS_ADMIN`, i.e. full device-mapper control; any helper would need an ioctl-number allow-list enforced in code (systemd cannot filter ioctl numbers) and a separate security review. Not proposed for M2.

O-1 device and MAC policy (unit keys, reviewed block classes, residual privilege, SELinux/AppArmor) is specified in `docs/m2/M2-4-O1-DEVICE-POLICY.md`; where it differs from earlier text here (for example removable/USB skipping), that document governs: USB skipping is helper behaviour, not a cgroup restriction.

O-1 implementation is now present, but runtime validation and M2-4 acceptance
remain open. LVM is still gated off; signature collection is available only
through the explicit, disabled-by-default O-1 path.

## O-1 helper (2026-11-17, owner-approved development; fail-closed until validated)

The O-1 native probe helper is implemented as a separately packaged,
disabled-by-default unit. The owner has authorized continuing its implementation
and isolated validation. It does not weaken hc-broker; the broker invokes it
only through the fixed typed `storage.probe` operation and root-only socket.
Runtime evidence remains incomplete, and the unit must not be activated on a
host or in production.

- **Binary:** `hc-storage-probe` (`backend/cmd/hc-storage-probe`), built from
  `backend/internal/storageprobe`. It reads one closed typed Request from
  stdin and writes one closed typed Result to stdout.
- **Device resolution (blkid fd-pinned; LVM path-based):** each name is
  revalidated against `/sys/class/block`, resolved to `/dev/block/MAJ:MIN`,
  opened following the symlink, and fstat-checked to be a block device whose
  rdev matches the resolved MAJ:MIN, with `diskseq` read before/after. blkid
  reads that exact fd via `/proc/self/fd/3` (the fd is inherited as the first
  extra file), so its identity is pinned to the opened device, not a re-resolved
  path. lvs/pvs cannot safely consume a single inherited fd: they scan a device
  *set* by path, so they stay on the fixed `devices{filter=[…]}` filter plus a
  `diskseq` recheck — a weaker TOCTOU posture, documented below as the remaining
  LVM blocker. When identity cannot be verified (e.g. the fail-closed unit has
  no device access), the device is reported and the result is Partial.
- **Skip by default:** loop/zram/ram/sr/fd/nbd/rbd and device-mapper (multipath,
  LVM, crypt); removable and USB media (via the resolved `device` sysfs link);
  devices whose sysfs state is not `running`. Partitions inherit their whole
  disk's classification. Skips are reported, not silently probed.
- **Templates (fixed, no caller argv/path):** `lvs/pvs --readonly
  --reportformat json --units b --nosuffix -a --segments -o <fixed fields>
  --config devices{hints="none" filter=[…]}` against a private empty
  `LVM_SYSTEM_DIR` (no host lvm.conf or devices file), and `blkid -p -c
  /dev/null -o export` per resolved node. blkid output is allow-listed by key
  (LABEL/PARTLABEL are dropped; retained values are bounded), so raw
  user-controlled media strings are not copied into the result.
- **Guards:** environment replaced (LANG=C, LC_ALL=C, minimal PATH; no
  BLKID_FILE/BLKID_DEBUG/LVM_SYSTEM_DIR inherited); per-command output capped
  and time-bounded via a `Setsid` process-group kill; an in-process cap and
  rate window limit on-demand probes (blkid can wake disks).
- **Unit:** `homelab-control-storage-probe@.service` (template) + `.socket`
  (`Accept=yes`, `MaxConnections=1`), no `[Install]`. The service runs
  `DynamicUser=yes`, `PrivateDevices=no`, `DevicePolicy=closed` with only the
  four reviewed read-only block classes, an empty capability bounding set,
  and `PrivateNetwork=yes`. The root-only socket is at
  `/run/homelab-control/storage-probe.sock` (0600); packages do not enable it.
- **Guard:** executable use is narrowly allowed for the helper implementation;
  the guard prevents `hc-api`, `hlctl` and the broker from linking the helper.
  Static unit and packaging guards assert the reviewed disabled-by-default
  policy.

**Release blockers (not production-ready):** the owner approved O-1 development
and isolated validation, but the device-open mechanism and identity handling
are not production-ready; the dedicated SELinux/AppArmor policy and
KVM/systemd-container runtime evidence are unverified; the `storage.probe`
route, non-default scope, CSRF POST, audit, rate limit, OpenAPI contract and UI
action are wired and covered by synthetic tests. Those tests use fixtures/fake
tools and do not validate real distributions or hardware. LVM remains disabled
until its device identity binding and output are validated. Live dm-status
(O-2, `CAP_SYS_ADMIN`) is deliberately not implemented.

## O-1 helper — final repair notes (2026-11-17)

- **Identity handling (blkid fd-pinned; LVM still path-based):** each name is
  resolved to `/dev/block/MAJ:MIN`, opened following the symlink, fstat-checked
  block+rdev, and `diskseq`-checked before/after. blkid reads the pinned fd via
  `/proc/self/fd/3` (fd inherited as the first extra file), so it cannot
  re-resolve a kernel-name path. lvs/pvs cannot consume a single inherited fd
  for their device scan, so they remain on the fixed filter + `diskseq` recheck
  (weaker). When identity cannot be verified (e.g. no device access in the
  fail-closed unit), every device is reported `skip` + `Partial`.
- **LVM config:** `devices{hints="none" filter=[…]}` + private empty
  `LVM_SYSTEM_DIR`. The `hints="none"` and quoted `"a|regex|"`/`"r|regex|"` filter patterns follow
  the LVM primary docs, but are **unverified** here (no lvs/pvs binary in this
  environment); they must be validated on a real LVM host before activation.
  blkid's `-p -c /dev/null -o export` flags were confirmed against
  `/usr/sbin/blkid -h` (no device probed). lvs/pvs are skipped when no devices
  are eligible, and the LVM phase budget (5s each) reserves time for blkid.
- **Deadlines/rate:** the helper's request-wide deadline is 25s, below the
  unit's `TimeoutStartSec=30`. The in-process rate window and semaphore are
  per-instance (each `Accept=yes` connection is a new process); the real
  serialisation is the socket's `MaxConnections=1`, and cross-instance rate
  limiting remains a broker/action concern.
- **Account:** the helper runs `DynamicUser=yes` (throwaway unprivileged UID,
  no persistent account); it does not use the shared `homelab-control` user.
- **Binary resolution:** `ResolveToolPaths` picks lvs/pvs/blkid from reviewed
  distro paths only, resolving symlinks and requiring the resolved target and
  every ancestor directory to be root-owned and non-group/other-writable (a
  multi-call `lvs -> lvm` symlink is executed by its original name so argv[0]
  dispatches correctly). Missing or insecure binaries fail closed and surface
  as a typed Partial result.
- **Output:** blkid export is key-allow-listed with LABEL/PARTLABEL dropped
  and every value bounded to 255 printable bytes; LVM reports are bounded raw
  JSON (fixed field list, consumed by `storageinfo` parsers); the reply is
  frame-capped (3 MiB helper / 4 MiB broker) via an encoded-size preflight with HTML escaping off,
  and the writer rejects over-limit frames.

Remaining gates (unit stays disabled by default): prove LVM device-set behavior
with real `lvs`/`pvs` binaries before enabling LVM; validate the SELinux module
and AppArmor profile in supported disposable environments; verify systemd
device-policy enforcement and fail-closed behavior in KVM/systemd-container
tests; and validate packaging and outputs across supported distributions. The
broker route, non-default scope, audit, rate limit, OpenAPI contract and UI
action are implemented and covered by synthetic tests. No host activation or
real-device probe was performed.
