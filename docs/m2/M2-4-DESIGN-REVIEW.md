# M2-4 read-only storage design review (historical decisions and current implementation)

**Current status, 2026-11-18:** M2-4 is accepted for its scoped read-only
inventory. The native LVM2 metadata reader is wired into the separate helper,
broker, API graph and UI; the runtime executes `blkid` only (never `lvs`, `pvs`
or `lvm`). The explicit CSRF-protected POST, non-default scope, audit,
rate-limit, OpenAPI contract and UI action are implemented. The exact helper
passed enforcing SELinux on Fedora 44 and enforcing AppArmor on Debian 13 in
disposable KVM guests. See `M2-4-ACCEPTANCE.md` and
`M2-4-O1-DEVICE-POLICY.md` for checksums and evidence. The service remains
disabled by default; this acceptance does not authorize host activation or
production deployment. Historical proposal sections below are design history,
not current runtime behavior.

Owner authorized M2-4 development and isolated tests on 2026-11-05, including LVM2 RAID/cache visibility and Serial/ID versus WWN labels. On 2026-11-17 the owner additionally authorized O-1 VM testing. This does not authorize storage writes to the host or production activation. Existing file-manager and M2-3 worktrees are separate.

## Implemented boundary

The API's `storage.read` route reads bounded fixed sysfs/proc paths as the API service user, never block contents or mountpoints, and audits access before releasing identifiers. Requests supply no paths or executable arguments. Partial graphs explicitly cannot establish that a device is unused. Display preference is independent of persistent identity; duplicated serials remain ambiguous even with a unique WWN. Runtime kernel device numbers are observation-local keys, not write targets.

## Application-owned snapshots

Schema migration 2 adds identifier-bearing graph snapshots to the existing private SQLite database, without changing released migration 1. Before migration, the existing consistent backup mechanism remains in force; migration retry now uses a random suffix so an earlier recovery copy is not overwritten or a same-second retry blocked. Migration failure rolls back and old state remains readable. Old binaries refuse the newer schema; restore a verified pre-migration backup to downgrade, rather than editing schema version records.

Snapshots are bounded to 8 MiB each and the four latest observations. Insert and retention run in one transaction. Unknown or different boot IDs, future timestamps, and expired observations are stale; no historical graph is promoted to current after a collection failure. Saving app state is not a device write. Deleted SQLite rows are not claimed securely erased; retention bounds live records, not WAL/backup disk consumption. API responses distinguish persisted snapshots from ephemeral mode and withhold data if an enabled snapshot store fails. No snapshot-history endpoint exists yet.

## Native probe privilege review — original proposal (superseded by O-1 below)

The current root broker has `root-read` handlers and an unprivileged program runner. Its `root-exec` handle is explicitly confined to reviewed service write operations. Do not simply run `blkid` or `lvs` through that write handle from a new read op, add the API account to `disk`, or increase broker capabilities.

Proposed next slice: a separately typed, closed root-read-exec handle, made available only to individually registered `storage.read` ops. It must expose only fixed reviewed probe/report templates, scrub environment, bound output/time, reject generic execution and receive only broker-enumerated/revalidated block identities, never request paths. Keep service write templates/authorization and capabilities unchanged. Explicitly review device-node ownership and read requirements under existing hardening/PrivateDevices before claiming it works.

`blkid` must probe only enumerated/revalidated local block devices and never use or trust a stale cache as current proof. Avoid unbounded all-host scans or opening network filesystems. LVM reports must be non-mutating, with fixed fields/JSON/byte units, no caller config/profile/selection, and capability detection per installed version. LVM reporting may ordinarily update hints/caches or require locks; confirm an actually read-only invocation and distinguish metadata-only from live-kernel RAID/cache health. Do not infer health when the chosen read-only mode cannot observe it. Parser tests alone do not settle this security or compatibility review.

This paragraph describes the state before O-1 implementation. The native
reader and current validation supersede the old report-execution proposal.

## Confirmed native-probe integration constraint (2026-11-05)

The packaged broker service explicitly has `PrivateDevices=yes`, a strict system-service syscall filter and only SETUID/SETGID capabilities. A new argv template by itself cannot expose physical disks inside that service's device namespace. This is a real deployment constraint, not a reason to relax the existing service. [systemd's execution documentation](https://www.freedesktop.org/software/systemd/man/257/systemd.exec.html) describes the isolated device view and device policy.

A metadata-only LVM report cannot substitute for observed live health: [the upstream lvs manual](https://man7.org/linux/man-pages/man8/lvs.8.html) documents that `--readonly` does not communicate with device-mapper. Requesting RAID/cache counters in that mode must not invent runtime values.

This was the pre-O-1 recommendation. The implementation follows the
separate-helper design and reviewed device policy. The helper remains disabled
by default; runtime activation is limited to disposable validation.

Kernel md/device-mapper UUID observations can be collected safely from sysfs independently. UUID joins to LVM reports follow the upstream [UUID builder](https://github.com/lvmteam/lvm2/blob/main/libdm/libdm-string.c) and [layer rules](https://github.com/lvmteam/lvm2/blob/main/lib/misc/lvm-string.c); layer-suffixed mappings are not guessed to be the top LV. This is independently written format handling, not copied upstream implementation.

## Review outcome and remaining owner decisions (Claude, 2026-11-05)

Accepted from the independent review (`.dev` review ledger, not in Git): keep the existing broker unit unchanged; any native probe is a **separate, disabled-by-default unit** with a closed typed protocol. Verified from kernel source: device-mapper status ioctls require `CAP_SYS_ADMIN` (`drivers/md/dm-ioctl.c`, `ctl_ioctl`). Proposed phasing, for owner decision:

1. **Phase 1 (implemented):** unprivileged sysfs identity, md runtime state, signature graph, and bounded parsers/fixtures.
2. **Phase 2 (owner-approved O-1; implemented and isolated-VM validated):** separate read-only helper, with native LVM2 metadata parsing from pinned descriptors and `blkid -p -c /dev/null` as the only child command. The closed device classes, empty capability set, no network, disabled-by-default unit, explicit POST and separate MAC policies remain. Owner approval does not authorize enabling it on a host or production deployment.
3. **Phase 3 (owner decision O-2):** live dm status for LVM RAID/cache health. Requires `CAP_SYS_ADMIN`, i.e. full device-mapper control; any helper would need an ioctl-number allow-list enforced in code (systemd cannot filter ioctl numbers) and a separate security review. Not proposed for M2.

O-1 device and MAC policy (unit keys, reviewed block classes, residual privilege, SELinux/AppArmor) is specified in `docs/m2/M2-4-O1-DEVICE-POLICY.md`; where it differs from earlier text here (for example removable/USB skipping), that document governs: USB skipping is helper behaviour, not a cgroup restriction.

O-1 implementation and Fedora/Debian VM validation are complete for the
accepted scope. Signature and native LVM metadata collection occur only
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

**Historical release blockers (superseded):** The earlier external LVM reports
were unusable inside the helper sandbox. The native reader replaces them. The
SELinux policy has been validated on Fedora 44 and AppArmor on Debian 13.
Fedora's base SELinux policy lets the helper domain read some `etc_t` files,
including `hc-api.env`; the helper policy no longer adds
`files_read_etc_files`. The exact service `InaccessiblePaths` setting was
tested and denied access to `hc-api.env` with and without the SELinux domain.
The remaining non-blocking coverage limits are Ubuntu LTS, systemd-nspawn
containment, physical vendor hardware, and CI/package builds across every
supported distribution. The `storage.probe` route, non-default scope, CSRF
POST, audit, rate limit, OpenAPI contract and UI action are wired and covered by
tests. Live dm-status (O-2, `CAP_SYS_ADMIN`) is deliberately not implemented.

## Current O-1 implementation and verification (2026-11-18)

- The helper resolves each enumerated device through `/dev/block/MAJ:MIN`,
  opens and verifies the block descriptor, and reads both blkid and native LVM
  metadata from pinned descriptors. `blkid` is the only executed child tool;
  lvs/pvs/lvm are not in the runtime path.
- The native reader validates LVM labels, metadata-area/text CRCs, bounds,
  required scalar fields and segment numbering. It reads committed metadata,
  enforces per-device and aggregate byte/token limits, and returns typed
  partial states for unsupported or malformed metadata. Checked-in fixtures
  include 20 supported layouts with field-by-field comparisons to matching
  `vgcfgbackup` references, plus malformed, unsupported and missing-PV cases.
- The helper result is size-capped and version/code validated before the broker
  relays it. The API converts it to a metadata-only graph. PV-to-disk links are
  provisional or withheld until a same-boot revalidation and signature/size
  corroboration; live RAID/cache health remains unknown.
- The Fedora 44 SELinux enforcing retest produced a complete six-PV VG, one LV
  and three striped segments with zero new denials after adding a narrow
  `cgroup_t:dir search` permission. Debian 13 AppArmor enforcing also passed
  with the corrected per-family network denies and inherited AF_UNIX rule.
  Evidence and checksums are recorded in `M2-4-O1-DEVICE-POLICY.md`.
- The service remains disabled by default. Remaining platform-coverage items
  are Ubuntu LTS, systemd-nspawn containment, broader distro packaging/CI,
  physical vendor hardware, and additional AppArmor negative/device variants.
  They do not change the accepted read-only scope; host/production activation
  remains a separate owner decision and requires a narrower device grant.
