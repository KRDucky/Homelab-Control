# Evidence: product commit `ad725d43` (B-1 correction, first attempt) — SUPERSEDED, kept with its failures

Collected 2026-11-05 in disposable Docker containers (not VMs; SELinux disabled), packages built from a clean
worktree of `ad725d43` with Go 1.26.10 (`containers-long/packages.sha256`).

| Run | Result |
|---|---|
| Long matrix (`HC_M2_LONG=1 HC_M2_INTERRUPT=1`), Ubuntu 26.04/24.04/22.04, Fedora 44 | **exit 1 on all four**: smoke 70/1; M2-2 89/0; broker interruption 20/0; the M2-3 scenario failed 2 checks on each: "B-1 graph changed between preview and apply: refused" (the disable was **applied**) and the follow-up "anchored elsewhere: disable succeeded" (400, already disabled) |
| Browser B1–B18 (Chromium), Ubuntu 26.04 and Fedora 44 | 18/18 each, exit 0 |
| Standalone Phase B (`m2-3-interrupt-checks.sh`), Ubuntu 26.04 and Fedora 44 | 20/0, exit 0 each |
| Fresh install (probe 9), all four | nothing counted as unloaded |
| New scenario against the old build `63979c1b` (`repro-old-63979c1b-ubuntu-2604.log`) | exit 1 as expected: the B-1 disables were accepted (200) |

Cause of the failures (finding R17, `docs/m2/M2-3-DESIGN-REVIEW.md` §9): systemd's loaded graph does not contain
Homelab Control's own `--no-reload` changes, so a disable could rely on a puller the broker itself had already
disabled — unsafe. The scenario also enabled all three pullers at once, so they anchored one another. Fixed in
`ae937d8a`; this directory is not acceptance evidence for the final revision.

The earlier container round on `63979c1b` (started 2026-11-04) left no results: its logs were in a temporary
directory that was cleared by a workstation restart before they were copied; it is not reported as passed.
