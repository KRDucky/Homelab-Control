# M1 evidence — final commit `da41162a` (2026-10-16)

All results below were produced on commit `da41162a4ecd9ba0732abdd04c44fb332ecac3c5`.

## Important: earlier M1 evidence was never committed
The previous session's M1 report (Go 94/94, systemd 37/37 ×2, browser+axe 22/22, …, collected on
`d1077e4a`) cited this README and log files that were **never committed to any branch** and no longer
exist. Those numbers cannot be re-checked and are superseded by this file.

## 1. Local runs — disposable cloud sandbox
Ubuntu 24.04.5 LTS, **container sandbox, not a VM, systemd not PID 1**. Go 1.24.7, Node 22.22.0,
shellcheck 0.9.0 (same version as CI). Tests run as the unprivileged user `claude` unless noted.

| Check | Command | Result | Log |
|---|---|---|---|
| Lint, unit tests, security gate | `make check` (non-root) | pass; Vitest 15/15; govulncheck **skipped** (not installable here — see CI) | `make-check.txt` |
| Go tests, unprivileged | `go test -count=1 -v ./...` | 95/95 pass, 0 skip | `go-test-nonroot.txt` |
| Reproducible build | `scripts/repro-check.sh` | identical hc-api / hc-broker / hlctl hashes | `repro-check.txt` |
| Live API security checks | `tests/integration/run-hc-e2e.sh` | 15/15; browser part **not run** (Playwright Chromium download blocked) | `hc-api-live-checks.txt` |
| Package install without systemd (root) | `tests/packaging/install-test.sh <deb>` | 14/14 | `install-test-ubuntu2404-nosystemd.txt` |

## 2. GitHub Actions — run 38852873505 on `da41162a`
https://github.com/KRDucky/Homelab-Control---Webmin-Fork/actions/runs/38852873505 — **all 15 jobs passed**
(2026-10-16 04:52–04:56 UTC). Each script exits non-zero on any failed check, so a passing job means every
check in it passed. Per-check counts are in the job logs on GitHub; they could not be downloaded from the
sandbox (Actions log storage is not reachable from it).

| Job | Environment | What it verifies |
|---|---|---|
| check | ubuntu-24.04 runner, **Go 1.26.x** | lint, Go tests (non-root), Vitest, security gate **incl. govulncheck**, repro build, deb+rpm build |
| arch-package | archlinux container | makepkg as unprivileged builder |
| install-matrix ×9 | containers, no systemd | install → run broker+API → 14 checks → remove, on **Debian stable, Debian oldstable, Ubuntu 22.04, 24.04, 26.04, Fedora, openSUSE Tumbleweed, openSUSE Leap, Arch** |
| systemd-ubuntu | ubuntu-24.04 runner VM, systemd PID 1 | package + hardened units + M1 checks incl. real upstream Webmin |
| systemd-ubuntu-2604 | debootstrapped Ubuntu 26.04 container (systemd 259.5, PID 1) on the runner | same checks as systemd-ubuntu |
| browser-e2e | ubuntu-24.04 runner | unprivileged hc-api + Playwright browser e2e |
| upstream-perl-tests | ubuntu-24.04 runner | Webmin 2.670 `prove -lr t` |

## 3. Not re-run on the final commit
- `tests/e2e/run-m1-e2e.sh` (browser + axe WCAG scan against the packaged stack in a systemd container):
  needs Docker Hub, which the sandbox cannot reach, and it is not in CI. **No current axe evidence.**
- No screenshots were produced in this session.
- No VM run of any kind (no hypervisor; Vagrant file untested).
