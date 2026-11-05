# Evidence: product commit `c0f620de` (final candidate: `services.files`, search path, drop-in precedence)

Collected 2026-11-05 by Claude Code in disposable Docker containers (not VMs; SELinux disabled; workstation
kernel shared). Raw logs first written outside `/tmp` (`.dev/m2-3-logs/rc0f620de/`), copied here unchanged.
Packages built locally from a clean worktree of `c0f620de` with Go 1.26.10 (`packages.sha256`); the KVM procedure
uses the CI build instead. **Implementation evidence, not owner acceptance.** Design review §12.

Product commits of this round: `24367e8e` (`services.files`, `UnitPath` check, `/lib` order, drop-in hierarchy and
masks) and `c0f620de` (specific bounds message; environment guard). Test files: those of the PR head that
contains these results (`m2_3_enablement.py` and `m2_3_browser.py` corrected after the first runs, below).

| Run on `c0f620de` (`HC_M2_LONG=1 HC_M2_INTERRUPT=1`) | 26.04 | 24.04 | 22.04 | 24.04 + netplan | Fedora 44 |
|---|---|---|---|---|---|
| Smoke + M1 (`containers/`) | 71/0 | 71/0 | 71/0 | 71/0 | 71/0 |
| M2-2, long waits | 89/0 | 89/0 | 89/0 | 89/0 | 89/0 |
| M2-3, long waits (incl. `services.files` checks) | 144/0 | 143/0 | 144/0 | 143/0 | 143/0 |
| Broker SIGKILL | 20/0 | 20/0 | 20/0 | 20/0 | 20/0 |
| Browser B1–B20, Chromium (`browser/`) | 20/20 | not run | not run | 20/20 | 20/20 |
| Reboot reproduction (`running-review/`) | all refused except S1's own alias enable | same | same | same | same |
| Plan-only enable survey (`enable-survey/`) | 4 allowed / 16 protected | 4 / 16 | 4 / 7 | 4 / 14 | 7 / 18 |

- **First runs, kept (test-design errors, not product failures):** `containers-first-run/` failed one check on
  all five images — the new "its service logs still readable" check asked for the logs of a fixture unit that the
  logs endpoint answers 404 for (the `services.files` refusals themselves passed); `browser-first-run/` failed B20
  for the same reason (contents 403 correct, logs 404). Both checks now use `homelab-control.service`.
- The enable survey's decisions equal those of `00fb7f2b` and `eb2fc2d4` for every unit (`../review-eb2fc2d4/enable-survey/`).
- **Precedence** (`probe12/`, `systemd-docs/`): probe 12 on 249, 255, 255+netplan, 259, Fedora 259 (identical
  results; it prints the manager's `UnitPath`, identical on all five except `/lib/systemd/system` on 22.04);
  documentation: the load-path table of `systemd.unit.xml` at tags v249, v255, v259 (fetched from
  github.com/systemd/systemd) and the systemd 262 man page on the workstation (load path, drop-in rules). Probe
  12's type-level drop-ins change every unit's `Description`; the applied drop-in is read from `DropInPaths`.
- Local gates: `make check` exit 0 with govulncheck, ShellCheck, REUSE and spdx-tools present (nothing skipped) —
  `make-check.log` (Vitest 73/73 inside); Go tests as uid 1000 — `go-tests-nonroot.txt`.
- The final CI run, artifact hashes and merge-ref equivalence are recorded in `M2-3-KVM-ACCEPTANCE-PROCEDURE.md`.
