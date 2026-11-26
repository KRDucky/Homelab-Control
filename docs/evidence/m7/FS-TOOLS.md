# M7: ext4, XFS and f2fs from tune2fs, xfs_info and the kernel (2026-11-26)

Part of the M5–M9 review ("use the standard tools"). The three pages used to stat a binary and list mounts.
They now show what the standard tools report. This is a checkpoint, **not M7 acceptance**. ADR 0008
Amendment 17.

## Behaviour
- **ext4:** helper scope `ext4` runs `tune2fs -l /dev/block/<major>:<minor>` once per mounted device. `GET
  /ext4` adds `status`: state, recorded errors (count, first/last time and function), mount count against the
  forced-check count, last check and interval, created/last write, lifetime writes, block and inode counts,
  features, label and UUID. Loop and other ungranted device classes are skipped with a reason.
- **XFS:** helper scope `xfs` runs `xfs_spaceman -p xfs_info -c info <mountpoint>` (xfs_info's own
  command). `GET /xfs` adds `status`: size, block size, allocation groups, inode and sector size, stripe unit
  and width, log, realtime, and the on-disk features reported as enabled.
- **f2fs:** `GET /f2fs` adds `status` from `/sys/fs/f2fs/<disk>/` (free, dirty and overprovision segments,
  reserved blocks, lifetime writes, encoding, features), read unprivileged in hc-api. The disk name comes from
  `/sys/dev/block/<maj:min>/uevent`.
- Helper stopped: the ext4 and XFS pages still load and give the reason; f2fs is unaffected. No kernel
  filesystem: no broker call. Audit records counts only (`mounts=N filesystems=N`).
- Mounts under /home, /root or /run/user are hidden from the helper (ProtectHome); the pages name them.

**Review fix (2026-11-26, #178).** `collectStatus` dropped an f2fs mount whose device it could not name, so
the page said "No f2fs filesystem is mounted" while the mount list showed it. Every f2fs mount is now in the
report, in `skipped` with the reason (device not named in /sys/dev/block, or no /sys/fs/f2fs entry), or
counted by `truncated` (an unusable mount record). The shared Filesystems panel shows its "nothing mounted"
line only when nothing was skipped, hidden or cut off (ext4 and XFS too). `TestCollectReportsMountsItCannotRead`,
`TestCollectMarksUnusableMountRecords` and the panel tests failed without the fix.
Rerun of `fs-check.py` on the installed `0.1.0+dev.1+189+g61762e3` on all three guests below: 18/18 each (new
check: nothing skipped or truncated on a healthy host). Evidence `/mnt/tank/claude-vms/artifacts/m7-fs-61762e3/`.

## Verification
- Parsers tested on real `tune2fs` 1.47.4 output (clean, and with an error record set by `debugfs`) and real
  `xfs_info` 6.19.0 output, including a mountpoint shaped like a feature pair. Helper tests: exact
  invocations, one tune2fs per device with bind mounts grouped, loop skipped, a failing device recorded on
  its row, ext4 refused without block classes, XFS independent of them. Broker op and API tests (report
  merged, reason on error, no call without the kernel filesystem, no shorter hc-api deadline). f2fs sysfs
  tests with a fake root. Policy guard: 94 mutations rejected.
- `go test ./...` (non-root), frontend 324/324, ESLint, Prettier, `tsc`, REUSE, `make lint` (clean up to the
  cargo step; cargo is not installed on this host).
- Graphify at `12b7e45` and `fc75e64` (indexed from `git archive`): no definition-only symbols, seven
  baseline self-loops. Finding fixed: the three page comments still described the probe-only checkpoint.

### Disposable VMs (installed `.deb`, package `0.1.0+dev.1+189+gfc75e64`)

`fs-check.py` compares the API with the tools run as root in the same guest: every mounted ext4 device
equals `tune2fs -l` (UUID, state, block count, features, mount count, error count), including the root disk
(virtio) and an LV (device-mapper) with an error record; a bind mount is grouped with its device; a loop ext4
is skipped with its reason; XFS geometry and features equal `xfs_info`; an XFS under /home is absent from the
helper report and present in the page's mount list; f2fs free segments equal the sysfs file on a dm device
and lifetime writes are non-zero; missing scope gives 403 on all three; a stopped helper gives 200 with
reasons; the unit grants `block-device-mapper r`; the profile is enforced with no `hc-storage-probe`
AppArmor denial; audit rows carry counts only.

| Guest | Kernel | e2fsprogs | xfsprogs | f2fs-tools | Result |
|---|---|---|---|---|---|
| Ubuntu 26.04.1 LTS | 7.0.0-43-generic | 1.47.2-3ubuntu4 | 6.18.0-3 | 1.16.0-2build1 | 17/17 |
| Ubuntu 22.04.5 LTS | 5.15.0-203-generic | 1.46.5-2ubuntu1.2 | 5.13.0-1ubuntu2.2 | 1.14.0-2build1 | 17/17 |
| Debian 13 (trixie), UEFI, Secure Boot off | 6.12.118+deb13-amd64 | 1.47.2-3+b12 | 6.13.0-2+deb13u1 | 1.16.0-1.1+b1 | 17/17 |

VM findings fixed on the way: AppArmor denied `tune2fs` the libblkid cache and then the local NSS files it
reads to name the reserved uid/gid (both allowed read-only); ProtectHome hides /home sub-mounts from the
helper, so they were silently missing (the pages now name them). Harness fix (not product): Ubuntu 22.04
needs `linux-modules-extra` for the f2fs module.

Evidence: `/mnt/tank/claude-vms/artifacts/m7-fs-fc75e64/` (harness, per-guest `check.log`, package checksum);
earlier runs `m7-fs-b8533d9/` and `m7-fs-bdb42e0/`. Not covered: Ubuntu 24.04 and Debian 12 (same gap as M5),
ext4 on md or NVMe on a real host, and external XFS logs or realtime devices.
