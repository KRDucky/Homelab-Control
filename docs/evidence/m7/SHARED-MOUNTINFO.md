# M7 prep: one shared mountinfo parser (2026-11-25)

Graphify finding from the screenshot review: seven packages carried their own `/proc/self/mountinfo`
parser with drifting rules. `internal/mountinfo` replaces them (213 lines added, 1122 removed). This is a
refactor checkpoint, **not M7 acceptance**.

## Behaviour
- One bounded parser (4 MiB, 8192 records, 64 KiB lines) returning typed, unescaped records.
- Filesystem pages (ZFS, Btrfs, ext4, XFS, f2fs): a malformed or oversized line is skipped and the page
  marks its list incomplete. XFS already did this; ZFS, Btrfs, ext4 and f2fs used to fail the whole page.
- The storage graph and MergerFS still fail on a skipped line, so a broken inventory never reads as empty.
- ext4, XFS and f2fs share `mountinfo.DeviceMounts` (device-path and mountpoint checks, 256-byte cap).

## Verification
- `go test ./...` (non-root); `internal/mountinfo` coverage 92.8 %. `make lint` clean.
- Graphify at `4019443`: no missing endpoints, no import cycles, no unused symbols.
- Disposable Ubuntu 26.04.1 VM with real ext4, XFS, ZFS, Btrfs and MergerFS mounts, package
  `0.1.0+dev.1+179+g4019443`: each page's API mount list equals `findmnt --task <hc-api pid>` for its
  filesystem type, none marked truncated; the MergerFS pool keeps its fstab branches; the storage graph lists
  all 53 mounts. Output: `/mnt/tank/claude-vms/artifacts/webui-shots-3ed3271/mountinfo-refactor-4019443.txt`.
