# Evidence: product commit `d9946a4a` (current, final for this round)

Collected 2026-11-05 by Claude Code in disposable Docker containers (not VMs; SELinux disabled; workstation
kernel shared). Packages built from a clean worktree of `d9946a4a8e1953eee81ab3f7989d3ce8d151f077` with
Go 1.26.10 (`packages.sha256`); the KVM procedure uses GitHub CI's packages instead. **Implementation evidence,
not owner acceptance.** Test files are those of `d9946a4a`.

| Run (directory) | Ubuntu 26.04.1 (259) | Ubuntu 24.04.5 (255) | Ubuntu 22.04.5 (249) | Fedora 44 (259, `.rpm`) |
|---|---|---|---|---|
| Smoke + M1 (`containers-long/`) | 71 / 0 | 71 / 0 | 71 / 0 | 71 / 0 |
| M2-2, `HC_M2_LONG=1` | 89 / 0 | 89 / 0 | 89 / 0 | 89 / 0 |
| M2-3 scenario, `HC_M2_LONG=1`: unloaded-change gate 6d, B-1 6e, cleanup invariants | 130 / 0 | 129 / 0 | 130 / 0 | 129 / 0 |
| Broker SIGKILL (`HC_M2_INTERRUPT=1`) | 20 / 0 | 20 / 0 | 20 / 0 | 20 / 0 |
| Standalone Phase B (`phase-b/`, the KVM procedure's invocation) | 20 / 0 | not run | not run | 20 / 0 |
| Browser B1–B18, Chromium (`browser/`) | 18 / 18 | not run | not run | 18 / 18 |
| Fresh install: nothing counted as unloaded (`probe9/`) | yes | yes | yes | yes |
| Disable survey, plan only (`survey/`): enabled services allowed / refused (protected name) | 1 / 2 | 1 / 1 | 1 / 3 | 4 / 12 |
| Survey: `Wants=network-online.target` fixture, only puller, wait-online enabled | refused (B-1) | refused | refused | refused |
| E4 API / broker | 1.6 / 1.5 | 1.6 / 1.5 | 1.6 / 1.5 | 1.6 / 1.5 |

- All runs exit 0. The survey never applies a change to a host unit; the wait-online unit it enables for one
  measurement is restored (`restored: is-enabled disabled (was disabled)` in each log). Plan durations in the
  survey: 1–227 ms (refusals by protected name take about 1 ms).
- Workstation unchanged (`host-{before,after}.txt` identical).
- Local gates on the final tree: `make check` exit 0 (Go 1.26.10; govulncheck v1.9.0 "No vulnerabilities
  found", reuse 6.2.0, spdx-tools 0.8.3, ShellCheck 0.11.0; nothing skipped) — `make-check.log`; Go tests as
  uid 1000: 294 tests + 122 subtests, 0 failed — `go-tests-nonroot.txt`; Vitest 73/73; CI browser suites
  298/0 (Chromium, Firefox) — `ci-browser-e2e-chromium-firefox.log`.
- Earlier rounds of this review: `../review-ae937d8a/` (B-1 with overlay; the same checks, the run before the
  client-deadline fix), `../review-ad725d43/` (failing first attempt), `../review-3bec1294/`, `../review-82c24a74/`.
- GitHub CI: `ci/` (job inventory and artifact hashes, added when the run completes).
