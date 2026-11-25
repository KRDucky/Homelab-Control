# M7: Btrfs filesystems from btrfs-progs (2026-11-25)

Part of the M5–M9 review ("use the standard tools"). The Btrfs page used to stat the `btrfs` binary and list
`fstype=btrfs` mounts. It now shows each mounted filesystem as btrfs-progs reports it, through the read-only
storage helper. This is a checkpoint, **not M7 acceptance**. ADR 0008 Amendment 14.

## Behaviour
- Helper request scope `btrfs`: per filesystem `btrfs filesystem usage -b`, `btrfs device stats`,
  `btrfs scrub status`, `btrfs subvolume list`, on a mountpoint the helper picks from its own mount table
  (grouped by device number; first mountpoint it can open). A failing command is reported on its filesystem;
  the others are still shown.
- Broker op `btrfs.status` (scope `btrfs.read`); ZFS and Btrfs share one per-scope 3 s answer cache and one
  API helper (`toolReport`). `GET /btrfs` adds `status`; audit records counts only (`mounts=N filesystems=N`).
- Page: per filesystem a state badge restating the tool (devices missing / N device errors recorded / no
  device errors recorded), usage meter, Data/Metadata/System profiles, last scrub, UUID, mounts, device error
  table and subvolume table. Mounts the helper cannot see are named ("Not inspected"); a subvolume mount on a
  reported filesystem's device is listed under it instead.
- Removed the page's unused optional smartctl offer (SMART is on the Storage page).

## Verification
- Parsers tested on real btrfs-progs 6.17.1 output (single/DUP, RAID1 on two devices, scrub finished and never
  run, subvolumes) plus error counters and the 1024-subvolume limit. Helper test: the scope runs exactly the
  four templates per filesystem on helper-chosen mountpoints, deduplicates subvolume mounts, skips `/home`-only
  filesystems, enforces the filesystem limit, and reports a failing command per filesystem. Broker test: the
  request carries the scope and a second call inside 3 s is cached. API and page tests for report, reason and
  no-kernel paths. Policy guard: 94 mutations rejected (new: file-wide read, directory write, extra file write,
  widened scrub-state rule).
- `go test ./...` (non-root), frontend 317/317, ESLint, Prettier, `tsc`, REUSE, `make lint` (clean up to the
  cargo step; cargo is not installed on this host).
- Graphify at `c66ad36` (indexed from `git archive`): no definition-only symbols, seven baseline self-loops.

### Disposable VMs (installed `.deb`, package `0.1.0+dev.1+187+gc66ad36`)

`btrfs-check.py` compares the API with btrfs-progs run as root in the same guest: filesystem UUIDs equal
`findmnt` minus filesystems mounted only under `/home`; profiles equal `btrfs filesystem df`; error counters
equal `btrfs device stats`; subvolume IDs equal `btrfs subvolume list`; scrub status equals `btrfs scrub
status`; no command failed; RAID1 data on two devices; the hidden mounts appear in the page's mount list;
missing `btrfs.read` gives 403; a stopped helper gives 200 with a reason; no `hc-storage-probe` AppArmor
denial with the profile enforced; audit rows carry counts and no path.

| Guest | Kernel | btrfs-progs | Filesystems | Result |
|---|---|---|---|---|
| Ubuntu 26.04.1 LTS | 7.0.0-43-generic | 6.17.1 | single/DUP on a virtio disk, RAID1 on two loop devices, one `/home`-only | 20/20 |
| Ubuntu 22.04.5 LTS | 5.15.0-203-generic | 5.16.2 | RAID1 on two virtio disks with a read-only snapshot, one `/home`-only | 15/15 |
| Debian 13 (trixie) | 6.12.118+deb13-amd64 | 6.14 | same as 22.04 | 15/15 |

VM findings fixed on the way: AppArmor denied `btrfs scrub status` its saved state
(`/var/lib/btrfs/scrub.status.<uuid>`), first the read and then the exclusive lock; `ProtectHome=yes` removes
`/home` sub-mounts from the helper's namespace, so the page now names Btrfs mounts the helper did not report
and lists subvolume mounts by device. The helper's own skip list therefore only fills for the filesystem limit
or on a systemd that keeps those mounts visible.

Evidence: `/mnt/tank/claude-vms/artifacts/m7-btrfs-cef285d/` (harness, per-guest `check.log`, package
checksums). Not covered: Ubuntu 24.04, Debian 12, a degraded (missing-device) filesystem, and non-zero device
error counters on a real device (both are unit-tested only).
