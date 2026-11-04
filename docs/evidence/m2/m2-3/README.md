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

## Final round: product commit `5a14c727`
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
