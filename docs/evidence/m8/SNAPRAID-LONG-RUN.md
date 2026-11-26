# M8 review: SnapRAID sync, scrub and fix run to completion (2026-11-26)

Part of the M5–M9 review. Design: ADR 0008 Amendment 19. Not M8 acceptance.

## Findings and fixes

| # | Finding | Cause | Fix | Test that fails without it |
|---|---|---|---|---|
| 1 | Sync, scrub and fix were killed after 80 s | They ran inside the helper's request: 80 s context, `TimeoutStartSec=85`, the broker's 80 s call | The helper starts SnapRAID with no deadline. An exit within 5 s is the reply; otherwise it replies `running` and waits for the exit, then logs the status | `TestLongRunRepliesWhileItContinues`, `TestLongRunReportsAnEarlyExit`, `TestMergerFSApplyReportsARunningSync`, `TestApplyUnitIsOptIn` |
| 2 | Output cap could stop a run | SnapRAID wrote to a 4 KiB capped pipe; a full cap fails the write | SnapRAID's output goes to the helper's stderr (the journal), not a pipe | `TestLongRunRepliesWhileItContinues` |
| 3 | `MemoryMax=512M` | SnapRAID needs about 1 GiB of RAM per 16 TB (manual, "hashsize") | Removed | `TestApplyUnitIsOptIn` |
| 4 | Status refused during any helper run | Socket `MaxConnections=1` | `MaxConnections=4`; SnapRAID's own lock refuses a second sync | `TestApplyUnitIsOptIn`; VM |
| 5 | Sync blocked for good after many new files | diff output over 1 MiB was "truncated", so the gate refused | The gate keeps the first and last 64 KiB (disk checks and summary) | `TestDiffGateReadsSummaryAfterLongFileList` |
| 6 | A held lock read as "diff unreadable" | No lock case | Block `locked`, with its own message | `TestDiffGateSaysArrayIsLocked`, `TestMergerFSApplyNamesTheHeldLock` |
| 7 | VM: the reply still waited 80 s ("uncertain") | With `Accept=yes` systemd keeps its own copy of the connection until the instance stops, so closing the helper's descriptors did not end it | `shutdown(2)` on the connection | `TestEndReplyEndsTheConnectionWhileAnotherCopyIsOpen` (holds a second copy) |
| 8 | VM (Ubuntu 22.04, 26.04): AppArmor denied that shutdown | Those kernels mediate `socket_shutdown` on a path-bound socket as a write to the path; Debian 13's 6.12 does not | `/run/homelab-control/mergerfs-apply.sock rw` (the helper's own socket only) | `TestApplyUnitIsOptIn`; VM: no denial |

## VM validation (installed `.deb` of `5e381f9`)

Package `homelab-control_0.1.0+dev.1+189+g5e381f9_amd64.deb`. Evidence and harness:
`/mnt/tank/claude-vms/artifacts/m8-snapraid-longrun-463f913/` (`longrun-setup.sh`, `longrun-check.py`,
logs `*-5e381f9.log`, `*-status-regress.log`).

Array: two loop-backed ext4 data disks with 600 MB of random data each and a 3 GB parity disk, unsynced,
with a hand-written `snapraid.conf`. A test-only runtime drop-in (`IOReadBandwidthMax=` 4 MB/s on the two
data loop devices, in `/run`, removed afterwards) makes the first sync last well over 80 s. The sync goes
through the API: plan, then step-up, then apply, as an account with `mergerfs.maintain`.

| Guest | SnapRAID | Long-run check | Status regression (`snapraid-check.py`) |
|---|---|---|---|
| Ubuntu 26.04.1, kernel 7.0.0-43 | 13.0-1 | 12/12 (sync 169 s) | 13/13 |
| Debian 13, kernel 6.12.118 | 12.4-1 | 12/12 (sync 168 s) | 13/13 |
| Ubuntu 22.04.5, kernel 5.15.0-203 | 12.1-1 | 12/12 (sync 166 s) | 13/13 |

The long-run check covers:
- the unit settings;
- the apply answering "Started" in seconds with no exit code;
- SnapRAID still running in an active helper instance;
- status answering `locked` during the run;
- a second sync refused on the lock with nothing started;
- the run ending past 80 s with "snapraid sync exited with status 0" in the journal;
- status afterwards not locked, and `snapraid diff` reporting no differences;
- a diff of 1.2 MB (2,000 files under long paths), which no longer blocks sync, and those files synced;
- 403 without `mergerfs.maintain`;
- no AppArmor denial;
- no OOM kill.

Runs on the earlier builds (`463f913`, `774683f`) found findings 7 and 8. They also found three harness
faults:
- the account had not been reloaded;
- 30,000 files need about 7 GB of parity, and SnapRAID said so;
- a oneshot that is still running is `activating`, not `active`.

## Graphify

Indexed at `5e381f9`: no definition-only symbols and the 7 baseline self-loops. Every caller of
`DiffGate`, `BlockFromDiff`, `execute`, `startLong` and `blockedMessage` was updated. The removed
`diff-truncated` token is referenced nowhere else (frontend, OpenAPI, docs).

## Limits

- The diff gate still runs within the 80 s request. A diff that takes longer (very many files on slow
  disks) is reported "unreadable", and nothing starts.
- The broker does not follow a background run. The journal, the status page (`locked`, then the new
  state) and the operation journal's "Started" message are the record.
- `run()`'s "reply, then wait" path has no unit test. The VM run covers it.
- Not covered: Ubuntu 24.04, Debian 12, the page in a browser, scrub and fix on a long run (they share
  the code path with sync), and a real multi-terabyte array.
