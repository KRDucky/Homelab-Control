# Evidence: product commit `ae937d8a` (current; B-1 correction with the own-change overlay)

Collected 2026-11-05 by Claude Code in disposable Docker containers (not VMs; SELinux disabled; workstation
kernel shared). Packages built from a clean worktree of `ae937d8a6aa918456dfa49968f271acfc33603a4` with Go 1.26.10
(`containers-long/packages.sha256`). **Implementation evidence, not owner acceptance.** The KVM procedure uses
GitHub CI's packages instead.

| Run (directory) | Ubuntu 26.04.1 (259) | Ubuntu 24.04.5 (255) | Ubuntu 22.04.5 (249) | Fedora 44 (259, `.rpm`) |
|---|---|---|---|---|
| Smoke + M1 (`containers-long/`) | 71 / 0 | 71 / 0 | 71 / 0 | 71 / 0 |
| M2-2, `HC_M2_LONG=1` | 89 / 0 | 89 / 0 | 89 / 0 | 89 / 0 |
| M2-3 scenario, `HC_M2_LONG=1`, incl. B-1 6e and cleanup invariants | 130 / 0 | 129 / 0 | 130 / 0 | 129 / 0 |
| Broker SIGKILL (`HC_M2_INTERRUPT=1`) | 20 / 0 | 20 / 0 | 20 / 0 | 20 / 0 |
| Standalone Phase B (`phase-b/`, the KVM procedure's invocation) | 20 / 0 | not run | not run | 20 / 0 |
| Browser B1–B18, Chromium (`browser/`) | 18 / 18 | not run | not run | 18 / 18 |
| Fresh install, nothing counted as unloaded (`probe9/`) | yes | yes | yes | yes |
| E4 API / broker | 1.6 / 1.5 | 1.6 / 1.5 | 1.6 / 1.5 | 1.6 / 1.5 |

- `containers-long/` ran the test files of `bd7f70fa` (which only renames a variable in scenario 6e) against the
  `ae937d8a` packages. The first run with the `ae937d8a` test files (`containers-long-cleanup-crash/`) passed
  every scenario check (121–122 each, including all 18 B-1 checks and the long waits) but **crashed in cleanup**
  (the variable shadowed the cleanup snapshot), so its cleanup invariants were not verified; exit 1 on all four.
  It is kept, not counted.
- Workstation unchanged (`containers-long/host-{before,after}.txt` identical).
- Local gates on the final tree: `make check` exit 0 (Go 1.26.10; govulncheck v1.9.0 "No vulnerabilities found",
  reuse 6.2.0 REUSE compliant, spdx-tools 0.8.3, ShellCheck 0.11.0; nothing skipped) — `make-check.log`; Go
  tests as uid 1000: 292 tests + 122 subtests, 0 failed — `go-tests-nonroot.txt`; Vitest 73/73; CI browser
  suites 298/0 (Chromium, Firefox) — `ci-browser-e2e-chromium-firefox.log`.
- Probes: `../probes/probe10-*` (B-1 without Homelab Control) and `../probes/probe9-*`.
- GitHub CI: `ci/` (added after the run completes).
