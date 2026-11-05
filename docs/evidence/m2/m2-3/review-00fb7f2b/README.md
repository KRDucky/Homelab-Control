# Evidence: product commit `00fb7f2b` (B-2: runtime unit configuration, P-8 option 3)

Collected 2026-11-05 by Claude Code in disposable Docker containers (not VMs; SELinux disabled; workstation
kernel shared; logs kept outside `/tmp` first). Packages built from a clean worktree of
`00fb7f2b66d62c5fe492ceca3fabb8076890cddb` with Go 1.26.10 (`packages.sha256`). **Implementation evidence, not
owner acceptance.** `ubuntu-2404-netplan` is the image representative of the GitHub runner (netplan 1.1.2,
networkd renderer, cloud-init style configuration; its own network namespace).

**Reproduction of B-2** (`repro/`): the previous build `d9946a4a` in `ubuntu-2404-netplan` fails exactly like CI
run 41978888955 (`m2-3-enablement: 72 passed, 54 failed`, same three netplan entries); the CI excerpt is
`repro/ci-41978888955-systemd-ubuntu-excerpt.txt`. Probe 11: `../probes/probe11-netplan-runtime-ubuntu-2404-netplan.out`.

| Run (directory) | Ubuntu 26.04 | 24.04 | 22.04 | 24.04 + netplan | Fedora 44 |
|---|---|---|---|---|---|
| Smoke + M1 (`containers-long/`, test files `b0a91f5d`) | 71/0 | 71/0 | 71/0 | 71/0 | 71/0 |
| M2-2, `HC_M2_LONG=1` | 89/0 | 89/0 | 89/0 | 89/0 | 89/0 |
| M2-3, `HC_M2_LONG=1` (B-1 6e, runtime 6f, cleanup invariants) | 141/0 | 140/0 | 141/0 | 140/0 | 140/0 |
| Broker SIGKILL | 20/0 | 20/0 | 20/0 | 20/0 | 20/0 |
| Browser B1–B18, Chromium (`browser/`) | 18/18 | not run | not run | 18/18 | 18/18 |
| Standalone Phase B (`phase-b/`) | 20/0 | not run | not run | not run | 20/0 |
| Fresh install, nothing counted as unloaded (`probe9/`) | yes | yes | yes | yes (netplan's 3 runtime entries listed, not blocking) | yes |
| Plan-only disable survey (`survey/`) | ran | ran | ran | ran | ran |

- `containers-long/` used the `00fb7f2b` packages with the test files of `b0a91f5d`. The first run with the
  test files of `00fb7f2b` (`containers-first-run/`) failed 4 scenario checks on all five images, all
  **test-design** errors, kept as a record: a check still expecting the removed blanket refusal, and runtime
  masks applied to units in `/etc/systemd/system`, which take precedence over `/run`, so the masks had no
  effect and the product correctly allowed the changes. `b0a91f5d` moves the mask fixtures to `/usr/lib` and
  asserts each mask is effective.
- Workstation unchanged (`containers-long/host-{before,after}.txt` identical, including `ip -br addr`).
- Local gates on the final tree: `make check` exit 0 (Go 1.26.10; govulncheck v1.9.0, reuse 6.2.0, spdx-tools
  0.8.3, ShellCheck 0.11.0; nothing skipped) — `make-check.log`; Go tests as uid 1000: 300 tests + 135
  subtests, 0 failed — `go-tests-nonroot.txt`; Vitest 73/73; CI browser suites 298/0 — `ci-browser-e2e-chromium-firefox.log`.
- GitHub CI: `ci/` (added when the run completes).
