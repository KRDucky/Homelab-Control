# M7: ZFS pools from `zpool` and `zfs` (2026-11-25)

Part of the M5–M9 review ("use the standard tools"). The ZFS page used to stat the `zfs`/`zpool` binaries
and list `fstype=zfs` mounts; it never showed a pool. It now shows what OpenZFS reports, through the
read-only storage helper. This is a checkpoint, **not M7 acceptance**. ADR 0008 Amendment 13.

## Behaviour
- Helper request scope `zfs` runs three fixed templates: `zpool list -H -p -o
  name,size,alloc,free,frag,cap,health`, `zpool status -p`, and `zfs list -H -p -o
  name,used,avail,refer,mountpoint,compression,type -t filesystem,volume`.
- Broker op `zfs.status` (scope `zfs.read`, root-read, 3 s answer cache). `GET /zfs` adds `status`:
  pools (health, capacity, fragmentation, status/action/scan/errors text, vdev tree with read, write and
  checksum counters, log/cache/spare/special groups) and datasets and volumes.
- No kernel ZFS: no broker call; the page says why. Helper stopped: the page still loads and states the
  reason. The API audit records counts only (`mounts=N pools=N`).
- Unit: `DeviceAllow=/dev/zfs rw` (libzfs opens the control node read-write for its read ioctls); block
  classes stay read-only. AppArmor: `zpool`/`zfs` `ix`, `/dev/zfs rw`, `/etc/zfs/** r`, hostid reads.
- The page's unused optional `smartctl`/`nvme` probes were removed (Graphify finding); SMART is on the
  Storage page.

## Verification
- Parsers tested on real OpenZFS 2.4.1 output (degraded raidz1 with log and cache, healthy mirror) and on
  synthetic resilver/spare/error-count rows. Helper test: the zfs scope runs exactly the three templates
  and nothing else; an unknown scope is refused. API tests: broker report merged, broker error becomes a
  reason, no broker call without kernel ZFS. Policy guard: 90 mutations rejected (new: `/dev/zfs rwm`,
  another node, node removed, extra device write).
- `go test ./...` (non-root), frontend 315/315, ESLint, Prettier, `tsc`, REUSE, `make lint` (clean up to
  the cargo step; cargo is not installed on this host).
- Graphify at `0450d99` and `956e2eb` (indexed from `git archive`): no definition-only symbols, seven
  baseline self-loops. Finding fixed: unused ZFS optional tool probes and stale "never executed" comments.

### Disposable VMs (installed `.deb`, package `0.1.0+dev.1+184+g956e2eb`)

`guest-check.py` compares the API with `zpool`/`zfs` run as root in the same guest: pools and health
equal `zpool list`; each pool's vdev rows equal the `zpool status` config tree; the scan line equals
`zpool status`; datasets and the test zvol equal `zfs list`; offlining a raidz member shows DEGRADED with an
OFFLINE row and the status text; no `zfs.read` gives 403; a stopped helper gives 200 with a reason; the unit
carries `/dev/zfs rw`; no `hc-storage-probe` AppArmor denial; audit rows carry counts, no dataset or path.

| Guest | Kernel | OpenZFS | Result |
|---|---|---|---|
| Ubuntu 26.04.1 LTS | 7.0.0-43-generic | 2.4.1-1ubuntu5.1 | 14/14 |
| Ubuntu 22.04.5 LTS | 5.15.0-203-generic | 2.1.5-1ubuntu6~22.04.7 | 14/14 |
| Debian 13 (trixie), UEFI, Secure Boot off | 6.12.118+deb13-amd64 | 2.3.9-0+deb13u1 (contrib, DKMS) | 14/14 |

On Debian, OpenZFS is `zfs-dkms` from contrib. With Secure Boot enforced the kernel rejects the unsigned
module ("Key was rejected by service") until the administrator enrolls the DKMS signing key (MOK); the
first Debian guest stopped there. The page then reports no kernel ZFS, as designed. The rerun boots the same
UEFI firmware with no enrolled keys.

VM findings fixed on the way: AppArmor denied `zpool` reading `/proc/sys/kernel/spl/hostid` (allowed,
read-only). Harness bugs fixed (not product): the audit check read only the newest row; the scan check
expected the word "scrub" after a resilver; the vdev ground truth included status continuation lines.

Evidence: `/mnt/tank/claude-vms/artifacts/m7-zfs-0450d99/` (harness, per-guest `check.log`, setup logs,
package checksums). Not covered: Ubuntu 24.04 and Debian 12 (same gap as M5), pools with spares, special
or dedup vdevs on a real host, and very large pools (bounds are unit-tested only).
