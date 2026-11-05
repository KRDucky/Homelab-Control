# M2-3 evidence (boot-time enable/disable, unit views, module registry)

Collected 2026-11-04 by Claude Code under the owner's M2-3 development and isolated-testing authorization.
**This is implementation evidence, not owner acceptance.** Logs were written outside the repository first and
copied here unchanged (ANSI colour codes stripped from `make` logs; raw JSON audit lines dropped from the e2e
log). They contain no passwords, cookies, plan tokens or fixture secret values (checked with `grep`; the
scenario also checks this itself).

## Where things ran
- **Workstation:** CachyOS, kernel 7.3.1, Docker. Go tests and fuzzing as uid 1000 (non-root) with the pinned
  release toolchain Go 1.26.10 (`GOTOOLCHAIN=go1.26.10`, from the local module cache).
- **Disposable systemd containers** from `dev/disposable/images/*.Dockerfile` via `dev/disposable/docker-systemd.sh`:
  `--privileged --cgroupns=private` (the host cgroup tree is **not** bind-mounted; the container sees
  `0::/init.scope`), own network namespace, udev and update timers masked, removed after each run. Base image
  digests: `containers/5a14c727/base-image-digests.txt`.
  **These are containers, not VMs:** no separate kernel boot (the workstation kernel 7.3.1 is shared), and
  **SELinux is disabled** (the workstation kernel has no SELinux LSM; `getenforce` = Disabled in the Fedora
  container, `containers/5a14c727/selinux.txt`). M2-2's Fedora defect P2 was SELinux-specific, so the
  Fedora results say nothing about SELinux; KVM VM validation is still required for that.
- No KVM VM was started or modified; the retained Ubuntu, Fedora and Arch acceptance VMs were not touched.
- **Host unchanged:** hashes of the workstation's enabled unit files and of the symlinks under
  `/etc/systemd/system`, sshd state (not running on this workstation) and the Docker container list were
  recorded before and after the final round and are identical (`containers/5a14c727/host-{before,after}.txt`).
- **Packages** built locally from clean trees (`make deb`, `make rpm`; binaries with the workstation's Go 1.27.3,
  not the pinned toolchain); hashes in `containers/5a14c727/packages.sha256`.

## Fourth review round: product commit `00fb7f2b` (current; B-2)
See `review-00fb7f2b/README.md`: B-2 reproduced in a netplan image representative of the GitHub runner; five
images (including the netplan one) with expiry waits and a real broker kill, all exit 0; browser, Phase B,
fresh-install probe, survey, local gates.

## Third review round, final: product commit `d9946a4a` (superseded by `00fb7f2b`)
See `review-d9946a4a/README.md`: four distributions with expiry waits and a real broker kill (all exit 0),
unloaded-change and B-1 checks, standalone Phase B, fresh-install probe, plan-only disable survey, browser,
local gates.

## Third review round: product commit `ae937d8a` (superseded by `d9946a4a`; B-1)
See `review-ae937d8a/README.md`: four distributions with expiry waits and a real broker kill (all exit 0),
B-1 checks, standalone Phase B, fresh-install probe, browser, local gates. The superseded first attempt
(`ad725d43`, failing two checks on all four) is kept in `review-ad725d43/`. The `63979c1b` round left no
results (temporary logs lost in a workstation restart).

## Second review round: product commit `3bec1294` (superseded by `63979c1b` and `ae937d8a`)
Unloaded unit-file changes (findings R12, R13; `docs/m2/M2-3-DESIGN-REVIEW.md` §1.2 and §8). Files:
`review-3bec1294/`. Local packages built from the clean tree at `3bec1294` with Go 1.26.10
(`containers-long/packages.sha256`); the KVM procedure uses CI's packages instead.

| Check | Ubuntu 26.04.1 (259) | Ubuntu 24.04.5 (255) | Ubuntu 22.04.5 (249) | Fedora 44 (259, `.rpm`) |
|---|---|---|---|---|
| Smoke + M1 | 71 / 0 | 71 / 0 | 71 / 0 | 71 / 0 |
| M2-2 regression, `HC_M2_LONG=1` | 89 / 0 | 89 / 0 | 89 / 0 | 89 / 0 |
| M2-3 scenario, `HC_M2_LONG=1` (`containers-long/`) | 106 / 0 | 105 / 0 | 106 / 0 | 105 / 0 |
| Real broker SIGKILL (`containers-long/`) | 19 / 0 | 19 / 0 | 19 / 0 | 19 / 0 |
| Final test files, short mode + broker kill (`containers-final-tests/`) | 86 / 105 / 19 | 86 / 104 / 19 | 86 / 105 / 19 | 86 / 104 / 19 |
| Browser B1–B18 (Chromium) | 18 / 18 | not run | not run | 18 / 18 |

- `containers-long/` ran the test files of `3bec1294`; the final test files (`5bc407d7`: the dependency-edit
  case 6d and an inode/group restore check) were run again in short mode with the broker kill
  (`containers-final-tests/`: M2-2 / M2-3 / interrupt counts). Product code was identical in both.
- **Reproduction before the fix** (`repro-old-82c24a74-ubuntu-2604.log`): the new checks against the old
  `82c24a74` package: all three unloaded changes accepted (200) for a loaded fixture, the edit between preview
  and apply **enabled** the unit, and a disable was accepted despite an unloaded protected-name `Requisite=`
  (6 FAIL, as expected).
- Gates on the final tree: `make check` exit 0 (Go 1.26.10; govulncheck, REUSE, shellcheck, spdx-tools; nothing
  skipped) — `make-check.log`; Go tests as uid 1000: 285 tests + 116 subtests, 0 failed —
  `go-tests-nonroot.txt`; Vitest 73/73; CI browser suites locally 298/0 (Chromium, Firefox) —
  `ci-browser-e2e-chromium-firefox.log` (a first attempt skipped the browser part because a Python without
  Playwright was first on `PATH`; rerun).
- Workstation unchanged (`containers-long/host-{before,after}.txt` identical). Probes: `probes/probe8*`.

## Review round: product commit `82c24a74` (superseded by `3bec1294`)
After the owner's PR #36 review assignment (findings R1–R11, `docs/m2/M2-3-DESIGN-REVIEW.md` §7). Files:
`review-82c24a74/`. Packages built locally from the clean tree at `82c24a74` with Go 1.26.10
(`containers/packages.sha256`); GitHub CI's own packages for the PR head are what the KVM procedure uses.

| Check | Ubuntu 26.04.1 (259) | Ubuntu 24.04.5 (255) | Ubuntu 22.04.5 (249) | Fedora 44 (259, `.rpm`) |
|---|---|---|---|---|
| Package smoke + M1 checks | 71 / 0 | 71 / 0 | 71 / 0 | 71 / 0 |
| M2-2 regression, **`HC_M2_LONG=1`** (expiry waits) | 89 / 0 | 89 / 0 | 89 / 0 | 89 / 0 |
| M2-3 scenario, **`HC_M2_LONG=1`** | 93 / 0 | 92 / 0 | 93 / 0 | 92 / 0 |
| **Real broker SIGKILL** during enable (`m2_3_interrupt.py`) | 19 / 0 | 19 / 0 | 19 / 0 | 19 / 0 |
| Real-backend browser B1–B18 (Chromium) | 18 / 18 | not run | not run | 18 / 18 |
| E4 API / broker | 1.6 / 1.5 | 1.6 / 1.5 | 1.6 / 1.5 | 1.6 / 1.5 |

- Broker kill: a root-owned wrapper (disposable container only, restored and checked afterwards) delayed
  `systemctl enable` of the fixture before / after the real call; the broker was SIGKILLed in the delay,
  systemd restarted it (`Restart=on-failure`), and the journal held one intent + one `interrupted` record per
  variant ("unchanged" / "changed, not verified"), nothing retried, hash chain verified. The printed INFO line
  of the record was empty in this run (harness printed a nested field; fixed afterwards, the assertion checked
  the whole record).
- Interrupt recheck (`interrupt-recheck/`, short mode, Ubuntu 26.04 and Fedora 44) after the harness began
  checking the restored `systemctl` by SHA-256 and SELinux label and printing the wrapper window: 19/0 each
  (smoke 71/0, M2-2 86/0, M2-3 90/0 and 89/0); label empty because SELinux is disabled in the containers.
- An earlier attempt of this round is **invalid** and not kept: the harness did not pass CI's
  `HC_SMOKE_SCOPES`/`HC_WEBMIN_SRC`, so the M1 checks failed with `forbidden`; `docker-systemd.sh` now
  defaults to CI's environment (R11).
- Gates on `82c24a74`: `make check` exit 0 (Go 1.26.10, govulncheck "No vulnerabilities found", REUSE
  compliant, shellcheck, spdx-tools; nothing skipped) — `make-check.log`; Go tests as uid 1000: 275 tests +
  97 subtests, 0 failed — `go-tests-nonroot.txt`; Vitest 73/73; CI browser suites locally 298/0 (Chromium,
  Firefox) — `ci-browser-e2e-chromium-firefox.log`; fuzz 60 s each (`FuzzParseInstall`,
  `FuzzEnablementArgs`, new `FuzzParseCat`), no failure — `fuzz-60s.txt` (`FuzzParseCat` was added after
  `82c24a74`, test-only).
- GitHub CI run 41793837318 on the previous head `9862a8dd` (product code identical to `5a14c727`): 21/21 jobs
  success, none skipped (`ci/ci-41793837318-jobs.tsv`); both systemd jobs ran the M2-3 scenario (83/0 on the
  runner VM, 84/0 in the 26.04 container) with expiry waits skipped; WebKit ran only the theme CSSOM/CSP probe.
- GitHub CI run 41811761629 on head `d7b703b0` (product `82c24a74`; merge ref `6fddf060`): 21/21 success, none
  skipped (`ci/ci-41811761629-jobs.tsv`); M2-3 scenario 89/0 (runner VM) and 90/0 (26.04 container), including
  the stale-view, undescribed-link and link-target checks; expiry waits and broker kill skipped by design in CI.
  `build` artifact hashes: `ci/build-artifact-41811761629.sha256`.
- Workstation unchanged (`containers/host-{before,after}.txt` identical). Probes: `probes/probe7*`.

## Earlier round: product commit `5a14c727` (superseded)
| Check | Ubuntu 26.04.1 (systemd 259.9) | Ubuntu 24.04.5 (255.4) | Ubuntu 22.04.5 (249.11) | Fedora 44 (259.10, `.rpm`) |
|---|---|---|---|---|
| Package smoke + M1 checks | 70 / 0 | 70 / 0 | 70 / 0 | 70 / 0 |
| M2-2 regression scenario (short mode) | 86 / 0 | 86 / 0 | 86 / 0 | 86 / 0 |
| M2-3 scenario (`tests/systemd/m2_3_enablement.py`) | 84 / 0 | 83 / 0 | 84 / 0 | 83 / 0 |
| Real-backend browser workflow B1–B18 (Chromium) | 18 / 18 | not run | not run | 18 / 18 |
| E4 `systemd-analyze security` API / broker | 1.6 / 1.5 | 1.6 / 1.5 | 1.6 / 1.5 | 1.6 / 1.5 |

Counts differ by distribution only where a check does not apply (the Debian `sshd.service` alias check), and
`NeedDaemonReload` after `--no-reload` is recorded as INFO (yes on 255 and 259, no on 249).
E4 equals the M2-2 KVM closeout (API 1.6 / broker 1.5, `../vm-final-main-d812995e/`); the broker bounding set
stays `cap_setgid cap_setuid`. These are container values; VM E4 evidence for M2-3 does not exist.

Files: `containers/5a14c727/*.log`; `browser/{ubuntu-26.04,fedora-44}/` (results, run log, screenshots:
laptop and phone layouts in Midnight and Daybreak, enable preview and result, detail panel, disabled module).

## Local gates on `5a14c727` (idle workstation)
- `make-check.log`: `make check` exit 0 with Go 1.26.10 and **all** security tools present (govulncheck v1.9.0
  "No vulnerabilities found", reuse 6.2.0 "REUSE compliant", spdx-tools 0.8.3, ShellCheck 0.11.0 from its
  container image); nothing skipped. With the workstation's own Go 1.27.3 the notices check stops because that
  Go package ships no `LICENSE` file (environment, not code).
- `go-tests-nonroot.txt`: `go test -count=1 -v ./...` as uid 1000: 266 tests and 86 subtests passed, 0 failed.
- Vitest: 73 / 73 (in `make-check.log`).
- `ci-browser-e2e-chromium-firefox.log`: CI's `tests/integration/run-hc-e2e.sh` (live API checks, `hc_e2e.py`,
  CSSOM/CSP probe, `hcis-ui.mjs` and the M2-2 `service-ops-ui.mjs` dialog suite) with
  `HC_CSP_BROWSERS=chromium,firefox`: 298 PASS, 0 FAIL. WebKit could not be installed on this workstation
  (Playwright host-dependency check).
- `fuzz-60s.txt`: 60 s each, no failure: `FuzzEnablementArgs`, `FuzzParseInstall`, and as regression
  `FuzzWriteArgs`, `FuzzLogsArgs`.
- `make test-release` (release tooling and workflow policy, after the `ci.yml` change): exit 0.

## Probes (`probes/`)
P1–P13 in `docs/m2/M2-3-DESIGN-REVIEW.md` §1 (probes 1–3, systemd 259); probe 4: an `Alias=` naming an
existing vendor unit replaces it at the next reload (led to the alias-collision refusal); probe 5: systemd 255
and 249 output formats; probe 6: older `systemctl cat`/`is-enabled` wording (led to two compatibility fixes).

## Earlier rounds (kept, superseded)
`containers/earlier/` and `browser/earlier-3e749293/`. What they found, all fixed before `5a14c727`:
- Harness: a time-window reload check (first run, 82/1) replaced by a journal cursor; PID 1 logged nothing at
  all during the apply in every later run. Browser-harness defects (systemd normalises `daily`, Fedora's
  `sshd.service`, a locator matching explanatory text, axe injection blocked by the page CSP → evaluated over
  the DevTools protocol instead).
- Product: no *Enable at boot* for installed but not-loaded units; WCAG 2.5.3 `label-content-name-mismatch` on
  the boot buttons; an inaccurate sidebar note and a stale navigation highlight; `Alias=` collisions (probe 4);
  systemd 255 `cat` and 249 `is-enabled` formats (probe 6).

## Not done / unavailable
- No VM evidence; no SELinux-enforcing run; Debian, openSUSE and Arch not run for M2-3; browser workflow only on
  Ubuntu 26.04 and Fedora 44 with Chromium (CI suites also with Firefox); no WebKit; no screen reader.
- `HC_M2_LONG` expiry waits not run (token and step-up expiry are unit-tested).
- A broker kill during an enablement is unit-tested only.
- GitHub CI has not run this branch at the time of writing.
- **Vitest timing:** one unidentified failure in about 25 standalone runs earlier in development, and five
  `findBy…` 1-second timeouts (including tests M2-3 does not touch, such as Legacy navigation) when `make check`
  ran while four fuzzers saturated the CPU. Without concurrent load all later runs passed 73/73. Open,
  load-sensitive test timing.
