# M2-4 read-only storage design review (in progress)

Owner authorized M2-4 development and isolated tests on 2026-11-05, including LVM2 RAID/cache visibility and Serial/ID versus WWN labels. This is not storage-write, KVM or production-activation authorization. Existing file-manager and M2-3 worktrees are separate.

## Implemented boundary

The API's `storage.read` route reads bounded fixed sysfs/proc paths as the API service user, never block contents or mountpoints, and audits access before releasing identifiers. Requests supply no paths or executable arguments. Partial graphs explicitly cannot establish that a device is unused. Display preference is independent of persistent identity; duplicated serials remain ambiguous even with a unique WWN. Runtime kernel device numbers are observation-local keys, not write targets.

## Application-owned snapshots

Schema migration 2 adds identifier-bearing graph snapshots to the existing private SQLite database, without changing released migration 1. Before migration, the existing consistent backup mechanism remains in force; migration retry now uses a random suffix so an earlier recovery copy is not overwritten or a same-second retry blocked. Migration failure rolls back and old state remains readable. Old binaries refuse the newer schema; restore a verified pre-migration backup to downgrade, rather than editing schema version records.

Snapshots are bounded to 8 MiB each and the four latest observations. Insert and retention run in one transaction. Unknown or different boot IDs, future timestamps, and expired observations are stale; no historical graph is promoted to current after a collection failure. Saving app state is not a device write. Deleted SQLite rows are not claimed securely erased; retention bounds live records, not WAL/backup disk consumption. API responses distinguish persisted snapshots from ephemeral mode and withhold data if an enabled snapshot store fails. No snapshot-history endpoint exists yet.

## Native probe privilege review — proposal, not registered

The current root broker has `root-read` handlers and an unprivileged program runner. Its `root-exec` handle is explicitly confined to reviewed service write operations. Do not simply run `blkid` or `lvs` through that write handle from a new read op, add the API account to `disk`, or increase broker capabilities.

Proposed next slice: a separately typed, closed root-read-exec handle, made available only to individually registered `storage.read` ops. It must expose only fixed reviewed probe/report templates, scrub environment, bound output/time, reject generic execution and receive only broker-enumerated/revalidated block identities, never request paths. Keep service write templates/authorization and capabilities unchanged. Explicitly review device-node ownership and read requirements under existing hardening/PrivateDevices before claiming it works.

`blkid` must probe only enumerated/revalidated local block devices and never use or trust a stale cache as current proof. Avoid unbounded all-host scans or opening network filesystems. LVM reports must be non-mutating, with fixed fields/JSON/byte units, no caller config/profile/selection, and capability detection per installed version. LVM reporting may ordinarily update hints/caches or require locks; confirm an actually read-only invocation and distinguish metadata-only from live-kernel RAID/cache health. Do not infer health when the chosen read-only mode cannot observe it. Parser tests alone do not settle this security or compatibility review.

Native root-read execution is not implemented yet. Until independently reviewed and tested in new disposable fixtures/containers, the production collector remains unprivileged metadata only. SSD cache, LVM RAID/PV member joins, ZFS/Btrfs/mergerfs and signature collection remain incomplete and prevent M2-4 acceptance.

## Confirmed native-probe integration constraint (2026-11-05)

The packaged broker service explicitly has `PrivateDevices=yes`, a strict system-service syscall filter and only SETUID/SETGID capabilities. A new argv template by itself cannot expose physical disks inside that service's device namespace. This is a real deployment constraint, not a reason to relax the existing service. [systemd's execution documentation](https://www.freedesktop.org/software/systemd/man/257/systemd.exec.html) describes the isolated device view and device policy.

A metadata-only LVM report cannot substitute for observed live health: [the upstream lvs manual](https://man7.org/linux/man-pages/man8/lvs.8.html) documents that `--readonly` does not communicate with device-mapper. Requesting RAID/cache counters in that mode must not invent runtime values.

Recommended design direction for independent review: keep the existing broker unchanged; use a separately packaged, disabled-by-default read-only storage-probe helper with a closed typed protocol and reviewed native templates. Specify its device namespace, read-only device allow-list and kernel status needs explicitly; do not silently grant broad capabilities or block-write access. Activation and SELinux/hardening evidence remain gates. This helper is a proposal only: no service, capability change or root read command is currently installed or registered. Current metadata collector and UI remain partial until this is resolved.

Kernel md/device-mapper UUID observations can be collected safely from sysfs independently. UUID joins to LVM reports follow the upstream [UUID builder](https://github.com/lvmteam/lvm2/blob/main/libdm/libdm-string.c) and [layer rules](https://github.com/lvmteam/lvm2/blob/main/lib/misc/lvm-string.c); layer-suffixed mappings are not guessed to be the top LV. This is independently written format handling, not copied upstream implementation.

## Review outcome and remaining owner decisions (Claude, 2026-11-05)

Accepted from the independent review (`.dev` review ledger, not in Git): keep the existing broker unit unchanged; any native probe is a **separate, disabled-by-default unit** with a closed typed protocol. Verified from kernel source: device-mapper status ioctls require `CAP_SYS_ADMIN` (`drivers/md/dm-ioctl.c`, `ctl_ioctl`). Proposed phasing, for owner decision:

1. **Phase 1 (no new privilege, implemented):** unprivileged sysfs identity, md runtime state, and parsers for metadata-only `lvs`/`pvs` reports. Production hc-api attaches no LVM report today.
2. **Phase 2 (owner decision O-1):** a separate read-only probe unit running `lvs`/`pvs --readonly` (hints disabled by a fixed in-template config) and `blkid -p -c /dev/null` on enumerated, revalidated devices. Proposed restrictions: `PrivateDevices=no` with `DevicePolicy=closed` and `r`-only `DeviceAllow=` for reviewed block majors, empty capability bounding set, `ReadOnlyPaths=/dev`, `PrivateNetwork=yes`, no `[Install]` enablement, per-device child timeouts, skipping suspended dm, multipath, nbd/rbd/iSCSI, sr and fd devices, on-demand only (blkid wakes standby disks). Needs its own SELinux context, tests from review §B4 and KVM evidence.
3. **Phase 3 (owner decision O-2):** live dm status for LVM RAID/cache health. Requires `CAP_SYS_ADMIN`, i.e. full device-mapper control; any helper would need an ioctl-number allow-list enforced in code (systemd cannot filter ioctl numbers) and a separate security review. Not proposed for M2.

Until O-1 is decided, M2-4 can be reviewed only as a metadata/fixture slice: the Storage page cannot show LVM or signature data on real hosts.
