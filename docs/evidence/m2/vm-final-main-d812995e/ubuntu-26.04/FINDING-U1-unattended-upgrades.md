# Finding U1: unattended-upgrades restarted SSH during the Ubuntu scenario

**Classification:** test-environment interference from the guest's own package-update automation. This is not a HomeControl defect and not an H3 recurrence.

**Run invalidated: yes.** Testing on this guest stopped after the scenario, as the assignment's SSH rule requires. Browser E2 and E4 were **not run** on Ubuntu.

## What happened
| Time (UTC) | Event | Source |
|---|---|---|
| 10:21:42 | Guest reverted to `pre-m2` (internal snapshot from 2026-10-28 04:17) and rebooted | `restore.txt` |
| 10:22:44 | Scenario starts; `ssh.service` MainPID 1087 | `01-systemd-smoke-m2.log` |
| 10:23:50 | `apt-daily-upgrade.timer` fires (`Persistent=yes`; the previous run was before the snapshot) and starts unattended-upgrades | `ssh-stop-root-cause-raw.txt` |
| 10:24:02–10:24:59 | About 50 security updates installed, among them `sudo`, `libssl3t64`, `zlib1g`, `openssh-server` 1:10.2p1-2ubuntu3.6 → 3.8, and kernel 7.0.0-38 | `/var/log/dpkg.log` (same file) |
| 10:24:40 | Library upgrade requests `systemctl daemon-reexec` (journal: "Reexecution requested … (unit apt-daily-upgrade.service)"). Restarted: ssh, journald, resolved, networkd, udevd, udisks2, packagekit. sshd 1087 → 14668 | `ssh-stop-journal-window.txt` |
| 10:24:54 | `openssh-server` postinst stops `ssh.service` (sshd 14958) and closes `ssh.socket` | same |
| 10:24:55 | `ssh.socket` listening again: socket-activated idle, so `ssh.service` is inactive with MainPID 0 | same |
| 10:28:0x | Scenario cleanup: SSH invariant FAIL (ActiveState active → inactive, MainPID 1087 → 0) | `01-systemd-smoke-m2.log` |

## Evidence that the harness and HomeControl did not stop SSH
- **systemd attributes both events to the update job.** The journal names `apt-daily-upgrade.service` as the requesting unit for the 10:24:40 re-exec and the 10:24:55 reloads.
  The 10:24:54 stop and socket close match the second of the `openssh-server` upgrade in `dpkg.log`.
  A sudo-log check does **not** clear the harness: it runs as root inside one `sudo env …` session, so its own `systemctl` calls are not logged by sudo (`environment-after.txt`).
- **The harness has no path to SSH.** Its direct `systemctl` calls touch only HomeControl and `hc-test-*` units, and its cleanup stops are gated by `h3_may_stop` (`../security/harness-review.txt`). The same harness left SSH untouched on Fedora (sshd 1035 → 1035) and Arch (430 → 430).
- **The alias was refused.** The alias check passed: "alias resolving to ssh.service: 403 policy_denied (broker)".
- **No intermediate SSH stop.** "ssh.service untouched" passed at that point in the scenario (10:23).
- **SSH remained reachable.** Every later host connection worked; the socket was down for about 1 s at 10:24:54.

## Results of the contaminated run (recorded, not counted as acceptance)
- **m2-service-ops: 87 passed, 2 failed.** The 2 failures are the SSH invariants, caused by U1.
  Every functional M2-2 check passed: protected units, alias, chain, replay, 121 s token expiry, 5-minute step-up expiry, concurrency, interruption and audit.
- **systemd-smoke: 66 passed, 1 failed.** The one failure is the M2-2 sub-scenario exit code. The Webmin check `legacy: running miniserv detected` passed.
- **AppArmor:** 81 `DENIED` lines, all from Ubuntu's `who` profile reading `/etc/passwd` and `/etc/nsswitch.conf`. None are from hc-api or hc-broker.

## Why earlier Ubuntu runs did not show this
- The 2026-10-28 04:17 rerun started minutes after the guest's first boot, when the timer had just run (04:15).
- The old harness had no SSH invariant check.

## Preserved
- Snapshot `post-ssh-stop-failure-20261028` (internal, running) holds the state right after the run.
- Snapshot `pre-final-main-20261028` holds the state before this assignment.

## Recommended correction (needs owner approval; not done)
Before rerunning on Ubuntu, take a new clean baseline in which automatic updates cannot run during tests:
- **Option 1:** `apt-get upgrade` once, reboot, then snapshot.
- **Option 2:** disable `apt-daily-upgrade.timer` and `apt-daily.timer` (or `unattended-upgrades`) inside the disposable guest only.

Either changes guest configuration beyond what this assignment authorized, so it was not done. A preflight check for active package-update jobs and timers also belongs in the VM runbook.
