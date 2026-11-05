# Evidence: product commit `f8d3af2e` (final safety review: running state, R20–R22)

Collected 2026-11-05 by Claude Code in disposable Docker containers (not VMs; SELinux disabled; workstation
kernel shared; logs kept outside `/tmp` first, in `.dev/m2-3-logs/`). Packages built from a clean worktree of
`f8d3af2e` with Go 1.26.10 (`packages.sha256`). **Implementation evidence, not owner acceptance.** Design
review §11 explains the cases.

**Reproduction before and after** (`running-review/`, `dev/disposable/boot-repro.sh` with
`tests/systemd/m2_3_boot_repro.py`; a real container restart, `/run` a tmpfs and empty of fixtures afterwards):
`00fb7f2b-*.log` (before) and `f8d3af2e-*.log` (after), Ubuntu 22.04 (systemd 249), 24.04 (255), 24.04 with
netplan (255), 26.04 (259), Fedora 44 (259). Each line `DECISION` is the product's answer, `BOOT` the state
after the reboot, with controls.

| Case | `00fb7f2b` | `f8d3af2e` |
|---|---|---|
| U1 unreloaded edit of a running dependency | refused (all five) | refused (all five) |
| E1 running intermediate → conflict with protected stand-in | **allowed**; stand-in inactive after boot | refused |
| E2 running dependency with `FailureAction=reboot` | **allowed** | refused |
| E3 conflict with an enabled unit stopped now (stand-in `BindsTo=` it) | **allowed**; stand-in inactive after boot on 4 of 5 | refused |
| E5 socket activating a conflicting service | **allowed**; stand-in stopped at first connection | refused |
| E7e runtime drop-in hiding a persistent `Conflicts=` drop-in | **allowed**; stand-in inactive after boot | refused |
| D1 disable, running or stopped intermediate (4 combinations) | refused | refused |
| D6 disable, runtime alias over a persistent name | **allowed**; stand-in not started (control: started) | refused |
| D7 disable, runtime drop-in hiding a persistent `Wants=` drop-in | **allowed**; stand-in not started (control: started) | refused |
| S1 own unloaded alias, then disable of its puller | refused | refused |

Enable plan durations on `f8d3af2e`: 0.14–0.52 s.

| Regression run on `f8d3af2e` (`containers/`, `HC_M2_LONG=1 HC_M2_INTERRUPT=1`) | 26.04 | 24.04 | 22.04 | 24.04 + netplan | Fedora 44 |
|---|---|---|---|---|---|
| Smoke + M1 | 71/0 | 71/0 | 71/0 | 71/0 | 71/0 |
| M2-2 (long waits) | 89/0 | 89/0 | 89/0 | 89/0 | 89/0 |
| M2-3 (long waits) | 141/0 | 140/0 | 141/0 | 140/0 | 140/0 |
| Broker SIGKILL | 20/0 | 20/0 | 20/0 | 20/0 | 20/0 |
| Browser B1–B18, Chromium (`browser/`) | 18/18 | not run | not run | not run | not run |

- Local gates: `make check` exit 0 with govulncheck, ShellCheck, REUSE and spdx-tools present (nothing skipped)
  — `make-check.log`; Go tests as uid 1000: 310 tests + 137 subtests, 0 failed — `go-tests-nonroot.txt`.
- CI on the previous head `6770ccf0` (`ci/`): run 41991515635, attempt 1: 20 succeeded, `browser-e2e` failed
  (`tests/e2e/theme_cssom_csp.py` line 99, `Page.goto: Timeout 30000ms exceeded` on the second browser after
  Chromium had passed 9/9; excerpt `ci-41991515635-attempt1-browser-e2e-excerpt.txt`); attempt 2: 21/21.
  The final head's CI run is recorded in `M2-3-KVM-ACCEPTANCE-PROCEDURE.md`.
