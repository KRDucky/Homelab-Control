# Ubuntu U1 recovery and final-main acceptance

**PASS** on exact combined main d812995e3f9e0a5ca3c12f34991c035b01e121e5, CI 40666815912.

| Gate | Actual result | Evidence |
|---|---|---|
| H3 regression | 14 passed, 0 failed | 08-stage-and-h3.txt |
| M2-2 extended, HC_M2_LONG=1 | 89 passed, 0 failed, 0 skipped; exit 0 | 01-systemd-smoke-m2.log |
| Package smoke/M1 | 67 passed, 0 failed, 0 skipped | same log |
| Browser B1–B15 | 15/15 passed, exit 0; B16 not run | resumed-browser-e2/ |
| E4 | API 1.6, broker 1.5; no regression | e4-security.txt |
| Audit | operation, broker and API chains all verify | audit-final.txt |
| SSH | scenario 903 → 903; resumed browser 916 → 916; reachable/authenticated | scenario-ssh-and-mac.txt, resumed-browser-e2/ssh-safety.txt |
| Final state | shut off, persistent, autostart disabled | final-snapshot-and-shutdown.txt |

## U1 remediation
The original failed run remains unchanged in ../ubuntu-26.04/. Its automatic package upgrades restarted SSH mid-scenario.
Using clean pre-m2, apt metadata was refreshed, dpkg operations checked, and a full-upgrade completed: 63 packages upgraded, 8 newly installed, 1 tmux update deferred under Ubuntu's phasing policy (not forced).
The guest rebooted into kernel 7.0.0-42-generic and systemd 259.9-0ubuntu0.1. The overdue apt-daily-upgrade ran to completion with nothing left to upgrade. No restart-required condition remained. apt/dpkg integrity checks passed; the clock synchronized. Automatic security updates and both apt timers remain enabled.
The baseline contains neither HomeControl nor test fixtures. Disk-consistent snapshot pre-final-main-updated-20261028 was made while shut off (06-baseline-snapshot.txt).

## Resume and preservation
The earlier browser run was interrupted after B8 and has screenshots only; it is not a complete acceptance record. It was preserved with a new snapshot pre-u1-browser-resume-20261028 before resetting the disposable browser account and test fixtures.
The complete browser rerun ran 2026-10-29 UTC. Package and broker hashes still matched final main. No application or harness source was edited.
Only Ubuntu was started or changed. Fedora/Arch were queried read-only for final state. Historical per-guest files are protected by historical-file-hashes.json.

## Security and caveats
The exact DEB is homelab-control_0.1.0+dev.1+52+gd812995e_amd64.deb, SHA-256 53c2a5dc0c3a2be700d5993643f238b713328b15a779c33afa44dd2dad5b5a54.
Installed broker SHA-256 a34f460450901650db2513fc5a7608800d15f89f4c20fbdeeb00dd9ab125c048 matches the CI package binary.
API capabilities all zero; broker effective/bounding c0 (setgid/setuid only); ambient zero; NNP=1, seccomp=2 for both. AppArmor is enabled; HomeControl processes are unconfined. The observed denials belong exclusively to Ubuntu's who profile (4 in the scenario, 22 over the resumed boot), not HomeControl. No security control was disabled.
Audit verify without a checkpoint establishes chain consistency; it cannot independently detect truncated tails or rollback. Earlier scenario checkpoint checks also passed.

## Retained recovery points
pre-m2, pre-final-main-20261028 and post-ssh-stop-failure-20261028 all remain.
New clean baseline: pre-final-main-updated-20261028 (disk-only).
Preserved interrupted state: pre-u1-browser-resume-20261028 (RAM+disk).
Final passing checkpoint: m2-final-validated-ubuntu-20261028 (disk-only, guest shut off).
All historical snapshot XML compared equal after final creation. Final Ubuntu overlay allocation: 14.83 GiB, six snapshots; seed ISO and original base image retained.
The final checkpoint contains HomeControl configured for guest-loopback test HTTP and the disposable account and test units. It is a reusable test environment, not production configuration. The random browser password was memory-only and was not archived.
