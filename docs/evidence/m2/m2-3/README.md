# M2-3 evidence (boot-time enable/disable, unit views, module registry)

Collected 2026-11-04 by Claude Code under the owner's M2-3 development and isolated-testing authorization.
**This is implementation evidence, not owner acceptance.** Logs were written outside the repository first and
copied here unchanged (ANSI colour codes stripped from `make` logs). They contain no passwords, cookies, plan
tokens or fixture secrets (`grep` for `hcp1.` and the fixture values; the scenario also checks this itself).

## Where things ran
- **Workstation:** CachyOS, kernel 7.3.1, Docker; Go tests and fuzzing as uid 1000 (non-root).
- **Disposable systemd containers** built from `dev/disposable/images/*.Dockerfile` by
  `dev/disposable/docker-systemd.sh`: `--privileged --cgroupns=private` (the host cgroup tree is **not**
  bind-mounted; `/proc/self/cgroup` in the container is `0::/init.scope`), own network namespace, udev and
  update timers masked, removed after each run. **These are containers, not VMs**: no separate kernel boot,
  the workstation kernel is shared. No KVM VM was started or modified; the retained Ubuntu, Fedora and Arch
  acceptance VMs were not touched.
- **Packages** built locally (`make deb`, `make rpm`) from clean trees. Binaries of the final round were built
  with the workstation Go 1.27.3, not the pinned release toolchain (Go 1.26.10, used for `make check`).

## Final round: product commit `3e749293`
`containers/3e749293/packages.sha256` lists the package hashes.

| Check | Ubuntu 26.04.1, systemd 259.9 (`.deb`) | Fedora 44, systemd 259.10 (`.rpm`) |
|---|---|---|
| Package smoke + M1 checks (`systemd-smoke.sh`) | 70 passed, 0 failed | 70 passed, 0 failed |
| M2-2 regression scenario (`m2_service_ops.py`, short mode) | 86 / 0 | 86 / 0 |
| M2-3 scenario (`m2_3_enablement.py`) | 83 / 0 | 82 / 0 (the Debian-only `sshd.service` alias check does not apply) |
| Real-backend browser workflow (`tests/e2e/m2_3_browser.py`, Chromium, B1–B18) | 18 / 18 | 18 / 18 |
| E4: `systemd-analyze security` API / broker | 1.6 / 1.5 | 1.6 / 1.5 |
| Broker bounding set | `cap_setgid cap_setuid` | `cap_setgid cap_setuid` |

Files: `containers/3e749293/{ubuntu-26.04,fedora-44}.log`, `browser/{ubuntu-26.04,fedora-44}/` (results,
run log, screenshots of phone and laptop layouts, Midnight and Daybreak themes, dialogs, detail panel and the
disabled-module page).

**E4 comparison:** identical to the M2-2 closeout (API 1.6 / broker 1.5 on all three KVM guests, see
`../vm-final-main-d812995e/`), and no new capability. Caveat: these values come from containers, which can
differ from a VM for some properties; VM E4 evidence for M2-3 does not exist.

## Earlier rounds (kept, superseded)
- `containers/earlier/ubuntu-26.04-f1085ad0-run1-harness-defect.log`: 82/1. The failed check counted journal
  lines in a one-second window by time; it was a harness defect. The corrected check (journal cursor around the
  single apply, PID 1 messages only) found **no** PID 1 message at all during the apply in every later run.
- `containers/earlier/*-f1085ad0*.log`, `*-87749bb9.log`: all pass after the harness fix.
- The first browser runs found three product defects, all fixed before `3e749293`: no *Enable at boot* for
  installed but not-loaded units; WCAG 2.5.3 `label-content-name-mismatch` on the boot buttons; and a sidebar
  note claiming that everything except start/stop/restart was read-only, plus a stale *Overview* highlight on the
  disabled-module page. Their test-harness defects (timer text normalised by systemd, Fedora's `sshd.service`, a
  locator that matched explanatory text) were fixed as well.

## Local gates
- `make-check-go1.26.10.log`: `make check` exit 0 with `GOTOOLCHAIN=go1.26.10`; in that run govulncheck, reuse,
  pyspdxtools and shellcheck were not installed and reported SKIPPED.
- `make-security-all-tools.log`: the security gate again with govulncheck v1.9.0, reuse 6.2.0, spdx-tools
  0.8.3 (the versions CI pins) and ShellCheck 0.11.0 (container image): exit 0, "No vulnerabilities found",
  "REUSE compliant". It first found that `probes/probe1.out` quotes an upstream `SPDX-License-Identifier`
  line; `REUSE.toml` now overrides the licence of those captured outputs.
- With the workstation's own Go 1.27.3, `make check` stops at the notices check because that Go package ships
  no `LICENSE` file (`/usr/lib/go/LICENSE` missing): an environment issue, not a code defect.
- `go-tests-nonroot.txt`: `go test -count=1 -v ./...` as uid 1000: 263 tests and 81 subtests passed, 0 failed.
- `vitest.txt`: 73 of 73.
- `fuzz-60s.txt`: 60 s each, no failure: `FuzzEnablementArgs` (2.47 M execs), `FuzzParseInstall` (8.66 M),
  and as regression `FuzzWriteArgs` and `FuzzLogsArgs`.
- **Vitest timing:** one unidentified failure in about 25 standalone runs, and five failures (all `findBy…`
  1-second timeouts, including tests M2-3 does not touch, such as Legacy navigation) when `make check` ran
  while four fuzzers saturated the CPU (`make-check` run not kept). Without concurrent load: 73/73 in every
  later run. Recorded as an open, load-sensitive test-timing issue.

## Not done / unavailable
- No VM evidence (the owner assigned VM testing to another agent; this run stayed on containers).
- Debian, openSUSE and Arch were not run for M2-3; Firefox and WebKit were not run; no screen reader.
- `HC_M2_LONG` expiry waits were not run for M2-2 or M2-3 (token and step-up expiry are unit-tested).
- An interrupted enablement (broker killed between intent and outcome) is unit-tested only.
- GitHub CI has not run this branch at the time of writing.
