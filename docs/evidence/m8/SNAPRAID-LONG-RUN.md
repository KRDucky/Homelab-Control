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
| 9 | A slow `snapraid diff` still stopped sync and scrub | diff ran under the 80 s request deadline, before the 5 s start grace: a large array was refused as "diff unreadable", or the broker's 80 s deadline expired and it reported `uncertain` while sync went on | diff runs with no deadline; the reply comes within 60 s (`running` + `checking` if diff is still going), and the block or the run's exit status is logged | `TestSlowDiffDoesNotHoldTheReply`, `TestGateBlockAfterTheReplyIsLogged`, `TestGatePassAfterTheReplyStartsTheRun`, `TestGateBlockWithinBudgetIsTheReply`, `TestMergerFSApplyReportsAStillRunningCheck` |
| 10 | VM (Ubuntu 22.04, status regression): helper disabled answered 502, not 503 | With the marker absent systemd accepts and closes the connection. When that close comes before the broker's write, the write fails with EPIPE, which was returned as an internal error; only the read side mapped a reset to "no reply" | A write that fails with EPIPE or ECONNRESET is "no reply" (the request was not delivered) | `TestSnapraidStatusHelperClosedBeforeTheRequest` |
| 11 | Owner: no clear status, and no way to see how a background run ended on the page | The page only showed `locked`. The end was only in the journal | A run record in the helper's `StateDirectory`, with a run lock that SnapRAID inherits. Status returns it as `lastRun`. The page shows "Latest sync: running / finished / failed / refused / interrupted", reads status after confirm and every 10 s while it runs, and the apply messages point there | `TestRunRecordFollowsARun`, `TestRunRecordSaysRefusedAndFailed`, `TestRunRecordInterrupted`, `TestSecondRunIsRefusedWhileOneIsActive`, `TestDamagedRunRecordIsIgnored`, `TestSnapraidStatusCarriesTheLastRun`, `TestApplyUnitIsOptIn`, frontend "latest SnapRAID run" (2 tests) |

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

## VM validation of findings 9 and 10 (installed `.deb` of `5af69a5`)

Package `homelab-control_0.1.0+dev.1+192+g5af69a5_amd64.deb`. Evidence and harness:
`/mnt/tank/claude-vms/artifacts/m8-snapraid-slowdiff-710714d/` (`harness/slowdiff-check.py`,
`harness/run-guest.sh`, logs `*-slowdiff.log`, `*-longrun.log`, `*-status-regress.log`; the first run on
`710714d` is in `run-710714d/`).

The slow-diff check holds `snapraid diff` with SIGSTOP as soon as it starts. The harness does this as an
observer, to stand in for a diff over a very large array. Then:
- the apply answers within the broker's deadline (60.5–60.6 s), saying the check is still running, with no
  exit code;
- no sync starts while the check is held;
- after SIGCONT the check passes, sync runs, and "snapraid sync exited with status 0" is in the journal;
- the array is then in sync;
- a second held check is killed with SIGKILL: the journal has "snapraid sync was not started:
  diff-unreadable", no sync ran, and the new file is still unsynced;
- no helper instance is left running, and there is no AppArmor denial.

| Guest | SnapRAID | Slow diff | Long-run regression | Status regression |
|---|---|---|---|---|
| Ubuntu 26.04.1, kernel 7.0.0-43 | 13.0-1 | 10/10 | 12/12 | 13/13 |
| Debian 13, kernel 6.12.118 | 12.4-1 | 10/10 | 12/12 | 13/13 |
| Ubuntu 22.04.5, kernel 5.15.0-204 | 12.1-1 | 10/10 | 12/12 | 13/13 |

The run on `710714d` found finding 10 (Ubuntu 22.04: status regression 12/13). It also found a harness
fault: the second journal window began in the same second as the first sync's exit line.

Graphify on `5af69a5`: no definition-only symbols and the 7 baseline self-loops. The callers of
`execute`, `startSnapraid`, `startGated`, `startLong`, `mergerfsApply` and `dialMergerFSAt` are covered by
the tests above. `Checking` is read only by the broker. The frontend shows the broker's message only.

## Graphify

Indexed at `5e381f9`: no definition-only symbols and the 7 baseline self-loops. Every caller of
`DiffGate`, `BlockFromDiff`, `execute`, `startLong` and `blockedMessage` was updated. The removed
`diff-truncated` token is referenced nowhere else (frontend, OpenAPI, docs).

## Limits

- The operation journal keeps "started" and is not updated when a run ends. The run record (finding 11)
  and the journal hold the end.
- Only the latest run is kept. SnapRAID's own output stays in the journal.
- `run()`'s "reply, then wait" path has no unit test. The VM runs cover it.
- Not covered: Ubuntu 24.04, Debian 12, the page in a browser, scrub and fix on a long run (they share
  the code path with sync), and a real multi-terabyte array.
