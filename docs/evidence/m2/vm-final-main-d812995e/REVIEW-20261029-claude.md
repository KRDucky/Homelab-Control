# Independent review of the Ubuntu U1 recovery run (Claude Code, 2026-10-29)

Reviewer: Claude Code, read-only. No VM, snapshot, guest, evidence file or repository file was changed; this file is the only addition.
Run reviewed: `ubuntu-26.04-u1-rerun/` (performed by Codex 2026-10-28/29), plus the rewritten `README.md` and `SUMMARY.md` in this folder.

## Verdict
**Ubuntu 26.04.1 passes on main `d812995e`**, as claimed. With the earlier Fedora 44 and Arch results, all three guests have now passed M2-2 combined-main validation (scenario, smoke + M1, browser B1–B15, E4). Two caveats below (C1, C2) should be recorded with the result; neither changes a check outcome.

## What was verified against the raw files
| Claim | Checked in | Result |
|---|---|---|
| Exact package | `08-stage-and-h3.txt`, `resumed-browser-e2/preflight.txt`, `environment-final.txt` | DEB `53c2a5dc…5a54` matches the CI 40666815912 manifest; installed broker `a34f4604…c048` is the same before and after the browser run |
| H3 regression 14/0 | `08-stage-and-h3.txt` | 14 passed, 0 failed, run as unprivileged `hc` |
| M2-2 89/0, smoke 67/0, `HC_M2_LONG=1` | `01-systemd-smoke-m2.log` | `m2-service-ops: 89 passed, 0 failed`; `systemd-smoke: 67 passed, 0 failed`; exit 0; the command line includes `HC_M2_LONG=1` |
| SSH untouched during the scenario | `01-systemd-smoke-m2.log` footer, `scenario-ssh-and-mac.txt` | MainPID 903 → 903; no SSH events |
| Browser B1–B15 | `resumed-browser-e2/console.log`, `E2-BROWSER-RECORD.md`, `exit-code.txt`, 16 screenshots | 15 Pass, 0 Fail, exit 0, no page errors (Chromium 156.0.8078.47). Starts again at B1 with a fresh session, not continued from B9 |
| SSH untouched during the browser run | `resumed-browser-e2/ssh-safety.txt`, `ssh-final.txt` | MainPID 916 → 916; the only SSH events of that boot are its own start at 04:57:20Z |
| E4 | `e4-security.txt` | hc-api 1.6, broker 1.5. hc-api CapEff/CapBnd/CapAmb are 0. Broker CapEff/CapBnd are `c0` (setgid/setuid), CapAmb is 0. NNP=1 and Seccomp=2 on both. API listens on 127.0.0.1:9180 only. Identical to Fedora/Arch and the M1 baseline |
| Audit chains | `audit-final.txt` | operations 24, broker 112, API/state 54 records: all `OK: chain verified` (no checkpoint, as noted) |
| MAC | `mac-denials-final.txt` | 22 AppArmor denials, all from the distro `who` profile (`/etc/passwd`, `/etc/nsswitch.conf`); none from HomeControl |
| No package drift between the runs | `environment-final.txt` vs `07-cold-boot-preflight.txt` | kernel 7.0.0-42, systemd 259.9-0ubuntu0.1, openssh-server 1:10.2p1-2ubuntu3.8, sudo unchanged; `dpkg --audit` clean; no reboot-required; no apt/dpkg process running; `apt-daily` shows no run since 2026-10-28 04:15:57Z |
| Final state | `final-snapshot-and-shutdown.txt`, `virsh list --all` (05:24Z) | all three guests shut off, autostart disabled; new disk-only snapshot `m2-final-validated-ubuntu-20261028`; historical snapshot XML unchanged |
| U1 fix approach | `02-*`, `04-*`, `05-*` | the baseline was pre-upgraded (full-upgrade, reboot, the overdue `apt-daily-upgrade` completed with nothing to do). Automatic updates were **not** disabled, so the test does not depend on a weakened guest |

## Caveats to record
- **C1, an undocumented gap between the phases.** The scenario and the interrupted browser run ended about 2026-10-28 11:24Z. The guest's next boot is 2026-10-29 04:57:20Z. No evidence file records how or when the guest was shut down in between, or whether the shutdown was graceful. Codex's summary says the PID change from 903 to 916 is "from the documented guest reboot/manual cold boot". The cold boot is documented; the shutdown before it is not.
  The impact is low: the package identities above are unchanged and the audit chains verify. `/var/log/dpkg.log` itself was not captured for that window.
- **C2, the browser setup was not a fresh install.** The resumed run reused the HomeControl install left by the interrupted run. It reset the `e2-operator` password and read-only scopes, stopped and reset the fixture units, and restarted hc-api (`resume-tooling/run-browser.py`).
  It did not move the state aside and reinstall, as method step 5 says. As a result, the operation journal and the B14 audit view also contain the interrupted run's operations (B14 counts 11 `e2-operator` rows; `E2-BROWSER-RECORD.md` notes this).
  No check depends on an empty journal, and B1's read-only precondition was re-established before B1.
- **Wording.** The rewritten `README.md`/`SUMMARY.md` call the U1 recovery "authorized". That authorization is not in any file reviewed here, so the owner should confirm it.
  The rewrite also dropped the earlier inputs, method, deviation and consolidation sections. The previous versions are preserved verbatim in `ubuntu-26.04-u1-rerun/prior-summary-records/`, so no history is lost. A future docs PR should keep them reachable.
- **Unchanged scope limits.** B16 (a second browser engine) was not run. B9 shows client-side expiry in the UI; the server-side `plan_invalid` for an expired token is covered by the scenario. This is not production configuration (`-dev-insecure-http` on loopback).

## Not decided here
None of this enables `services.operate` in a real deployment, authorizes M2-3, or changes the repository record. See `PROPOSED-DOCS-UPDATE-20261029.md`.
