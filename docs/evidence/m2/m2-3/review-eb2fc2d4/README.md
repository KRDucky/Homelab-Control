# Evidence: product commit `eb2fc2d4` (final safety review: running state and hidden configuration, R20–R23)

Collected 2026-11-05 by Claude Code in disposable Docker containers (not VMs; SELinux disabled; workstation
kernel shared; logs kept outside `/tmp` first, in `.dev/m2-3-logs/`). Packages built from clean worktrees of
`f8d3af2e` (first correction) and `eb2fc2d4` (final) with Go 1.26.10; `packages.sha256` is the local `eb2fc2d4`
build (the KVM procedure uses the CI build instead). **Implementation evidence, not owner acceptance.** Design
review §11 explains the cases.

**Reboot reproduction** (`running-review/`, `dev/disposable/boot-repro.sh` with `tests/systemd/m2_3_boot_repro.py`;
a real container restart, `/run` a tmpfs and empty of fixtures afterwards). `DECISION` is the product's answer,
`BOOT` the state after the reboot, with controls. Ubuntu 22.04 (systemd 249), 24.04 (255), 24.04 with netplan
(255), 26.04 (259), Fedora 44 (259).
- `00fb7f2b-*.log`: the build before the review (cases E1–D7 and S1);
- `f8d3af2e-*.log`: the first correction (same cases); `f8d3af2e-e8-*.log`: the same build with case E8 added;
- `eb2fc2d4-*.log`: the final build, all cases including E8.

| Case | `00fb7f2b` | `f8d3af2e` | `eb2fc2d4` |
|---|---|---|---|
| U1 unreloaded edit of a running dependency | refused | refused | refused |
| E1 running intermediate → conflict with protected stand-in | **allowed**; stand-in inactive after boot | refused | refused |
| E2 running dependency with `FailureAction=reboot` | **allowed** | refused | refused |
| E3 conflict with an enabled unit stopped now (stand-in `BindsTo=` it) | **allowed**; stand-in inactive after boot on 4 of 5 | refused | refused |
| E5 socket activating a conflicting service | **allowed**; stand-in stopped at first connection | refused | refused |
| E7e runtime drop-in hiding a persistent `Conflicts=` drop-in | **allowed**; stand-in inactive after boot | refused | refused |
| E8 unwalked stand-in's `/etc` drop-in `Conflicts=` hidden by `/run/systemd/system.control` | not run | **allowed** (stand-in survived; outcome not stable) | refused |
| E8b same, hidden in `/run/systemd/system` (precedence control) | not run | refused (`/etc` wins) | refused |
| D1 disable, running or stopped intermediate (4 combinations) | refused | refused | refused |
| D6 disable, runtime alias over a persistent name | **allowed**; stand-in not started (control: started) | refused | refused |
| D7 disable, runtime drop-in hiding a persistent `Wants=` drop-in | **allowed**; stand-in not started (control: started) | refused | refused |
| S1 own unloaded alias, then disable of its puller | refused | refused | refused |

| Regression run on `eb2fc2d4` (`containers/`, `HC_M2_LONG=1 HC_M2_INTERRUPT=1`) | 26.04 | 24.04 | 22.04 | 24.04 + netplan | Fedora 44 |
|---|---|---|---|---|---|
| Smoke + M1 | 71/0 | 71/0 | 71/0 | 71/0 | 71/0 |
| M2-2 (long waits) | 89/0 | 89/0 | 89/0 | 89/0 | 89/0 |
| M2-3 (long waits) | 141/0 | 140/0 | 141/0 | 140/0 | 140/0 |
| Broker SIGKILL | 20/0 | 20/0 | 20/0 | 20/0 | 20/0 |
| Browser B1–B18, Chromium (`browser/`) | 18/18 | not run | not run | not run | not run |

(The same regressions on `f8d3af2e` gave identical counts: `../review-f8d3af2e/containers/`, `../review-f8d3af2e/browser/`; that folder is kept as committed in `551fe322`, its README describing the first correction.)

**Cost of the boot mode (P-9), plan only** (`enable-survey/`, `tests/systemd/m2_3_enable_survey.py`): every
disabled, non-template service of each image plus three homelab-style fixtures (wanting
`network-online.target`, wanting running services, `Requires=dbus.socket`), planned on `00fb7f2b` and on
`eb2fc2d4`. **The decision is identical for every unit on all five images** (allowed 4/4/4/4/7, the rest
refused as protected); only plan time grew, from 17–55 ms to 139–977 ms per plan (slowest on Fedora 44; the
client deadline is 20 s). This measures stock and fixture services in containers, not a population of real
homelab hosts.

- Local gates on the final tree: `make check` exit 0 with govulncheck, ShellCheck, REUSE and spdx-tools present
  (nothing skipped) — `make-check.log`; Go tests as uid 1000 — `go-tests-nonroot.txt`.
- CI on the previous head `6770ccf0` (`ci/`): run 41991515635, attempt 1: 20 succeeded, `browser-e2e` failed
  (`tests/e2e/theme_cssom_csp.py` line 99, `Page.goto: Timeout 30000ms exceeded` on the second browser after
  Chromium had passed 9/9; excerpt `ci-41991515635-attempt1-browser-e2e-excerpt.txt`); attempt 2: 21/21.
  Intermediate head `551fe322` (product `f8d3af2e`): run 42003162105, 21/21 (superseded by `eb2fc2d4`). The final
  head's run is recorded in `M2-3-KVM-ACCEPTANCE-PROCEDURE.md`.
- **Final CI** (`ci/`): run 42008315859 on head `660151b0` (product `eb2fc2d4`), attempt 1, 21/21 succeeded
  (`ci-42008315859-jobs.tsv`); merge ref `f7c6bfca` (parents `81ee254f`, `660151b0`), tree `21cafccc` identical
  to the head's; stock runner `systemd-ubuntu` M2-3 137/0 with netplan's runtime entries
  (`systemd-ubuntu-42008315859-results.txt`); artifact hashes `build-artifact-42008315859.sha256` (as in the KVM
  procedure).
