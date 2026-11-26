# M8 review: SnapRAID status, snapraid.conf grammar, hand-written config (2026-11-26)

Part of the M5–M9 review ("use the standard tools"). Not M8 acceptance.

## Findings and fixes

| # | Finding | Cause | Fix | Test that fails without it |
|---|---|---|---|---|
| 1 | SnapRAID status never worked on a real array | The broker ran `snapraid status` as the service user under `ProtectSystem=strict`. SnapRAID takes its array lock for status too (cmdline/snapraid.c: only devices, smart, probe skip it) and content files are root `0600`. Guest: "Failed to create the lock file '/var/snapraid.content.lock'. Read-only file system." | The op sends the closed `status` action to the root helper `hc-mergerfs-apply`; `snapraid` leaves the broker allow-list (ADR 0008 Amendment 18) | `TestSnapraidStatusAsksTheHelper` |
| 2 | "Sync in progress" was never detected | Matcher looked for "sync is in progress"; status.c prints "You have a sync in progress at N%." | Phrase list taken from status.c | `TestParseSnapraidStatusSyncInProgress` |
| 3 | The per-disk table was dropped | Parser kept prose lines only | Table rows parsed (files, fragments, used, free, use%) and shown on the page | `TestParseSnapraidStatusReal` (real 13.0 output) |
| 4 | The note said content files are not read | Wrong: status loads the content file | Note and docs corrected | `TestParseSnapraidStatusReal` |
| 5 | Valid snapraid.conf shown as invalid or missing disks | Reader split on whitespace and knew only `data`, `parity`, `N-parity`. SnapRAID reads a path as the rest of the line, accepts `disk`, `1-parity`, `q-`/`r-`/`z-parity` (state.c) | Reader follows state.c | `TestParseSnapRAIDConfigFollowsSnapRAIDGrammar` |
| 6 | Saving the array definition overwrote a hand-written snapraid.conf | `persist` always wrote the generated file, renaming disks to d1…dN (SnapRAID's content file is keyed on names) and dropping excludes and settings | Refused unless the file is absent, empty, or has the managed first line; nothing (including fstab) is written then | `TestPersistRefusesUnmanagedSnapraidConf` |
| 7 | AppArmor: SnapRAID's example content path | Profile allowed `/var/snapraid/**` only | `/var/snapraid.content* rwlk` | VM: no denial |
| 8 | VM: helper disabled answered 502 | The socket accepts and the unit's condition closes the connection with no reply | Empty reply is "helper not enabled" (503) | `TestSnapraidStatusHelperClosedWithoutReply` |
| 9 | VM: lock notice shown twice | SnapRAID prints two "already in use" lines | Shown once | `TestParseSnapraidStatusLocked` |
| 10 | VM (Ubuntu 22.04): AppArmor denial for the Go runtime's huge page size read | Profile lacked the read (the APT helper has it) | `/sys/kernel/mm/transparent_hugepage/hpage_pmd_size r` | VM: no denial |

**Review fixes (2026-11-26, #181).**
- **Decimal sizes:** a status row whose used or free size had a decimal was rejected by an integer-only
  pattern and vanished. Every size column now accepts a decimal, and `usedGB`/`freeGB` are numbers.
  `TestParseSnapraidStatusDecimalSizes` fails with the old pattern.
- **Cut-off output:** when the helper cut snapraid's text at its 48 KiB cap, the broker returned the
  prefix as a complete report. The helper now sets `outputTruncated`, the broker sets `truncated`,
  and the page says the report is incomplete. `TestSnapraidStatusMarksHelperCutOutput`,
  `TestStatusResultMarksCutOutput` and the panel test fail without the fix.

## VM validation

Installed `.deb` of `9655dcb` (`homelab-control_0.1.0+dev.1+188+g9655dcb_amd64.deb`), check
`harness/snapraid-check.py` and `harness/snapraid-setup.sh` (loop-backed ext4 disks, hand-written
snapraid.conf in SnapRAID's example layout), run as root in the guest.

| Guest | Kernel | snapraid | Result |
|---|---|---|---|
| Ubuntu 26.04.1 LTS | 7.0.0-43 | 13.0-1 | 13/13 |
| Debian 13 (trixie) | 6.12.118+deb13 | 12.4-1 | 13/13 |
| Ubuntu 22.04.5 LTS | 5.15.0-203 | 12.1-1 | 13/13 |

Checks: status 200 with exit 0; per-disk table equals `sudo snapraid status`; SnapRAID's sentences
with no path; scrub age; lock held with `flock` → `locked`, one notice, no path; `disk d2 /mnt/disk two/`
accepted by snapraid and listed by the page; helper refuses to save over the hand-written
snapraid.conf and fstab/snapraid.conf are byte-identical afterwards; no `mergerfs.read` → 403; enable
marker removed → 503; marker restored → 200; no AppArmor denial for `hc-mergerfs-apply`.
Earlier builds: `24fcfa6` 12/13 on Ubuntu 26.04 (findings 8, 9); `81ffacb` 13/13 on Ubuntu 26.04 and
Debian 13, 12/13 on Ubuntu 22.04 (finding 10). The first Debian run of `81ffacb` failed 6 checks because
the harness put both data directories on one device, which SnapRAID 12 refuses; the page reported
SnapRAID's own warning with paths redacted. Evidence: `/mnt/tank/claude-vms/artifacts/m8-snapraid-24fcfa6/`.

Not covered: Ubuntu 24.04 and Debian 12; "sync in progress" from a real running sync (parser tested on
status.c's sentence); the UI in a browser (Vitest covers the table and the lock notice).

## Graphify

Indexed `9655dcb` (git archive, compat excluded): 7189 nodes, no definition-only symbols, 7 self-loops.
Neighbours of the changed symbols: `callMergerFS` is shared with the write apply, where an empty reply
is still "uncertain" as before; `ReadSnapRAID` feeds `Collect` and the honesty warnings, which now see
configs that were rejected before. No other caller; no frontend use of `exec_available` for snapraid.
