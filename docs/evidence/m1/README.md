# M1 evidence (2026-10-16)

Sections 1–2: commit `da41162a` (M1 merge candidate). Section 4: M1 closeout, final product code
`738fa37b` on the owner's workstation VM, plus CI run 38866773032 on the same commit.

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

## 3. Superseded gaps
The gaps listed here on `da41162a` (no axe evidence, no screenshots, no VM run) are closed by section 4.

## 4. Workstation KVM VM — Ubuntu 26.04 (M1 closeout, product code `738fa37b`)
Disposable libvirt guest `hc-test-u2604` on the owner's CachyOS workstation (AMD Ryzen 5 9600X, KVM,
QEMU 11.1.2, libvirt 12.8.0), created by `dev/vm/hc-vm.sh` in the `hc-test` pool on the `default` NAT
network from the GPG-verified Ubuntu 26.04 cloud image (sha256 `8800651811af…32b2`). Guest: Ubuntu
26.04.1 LTS, kernel 7.0.0-34, **systemd 259.5 as PID 1**, cgroup v2, 2 vCPU / 4 GiB. Run by
`dev/vm/run-m1-validation.sh` (2026-10-16 07:29 UTC); full logs in `vm-26.04/`.

| Step | Result | Log |
|---|---|---|
| systemd smoke + M1 checks: install not enabled; hc-api as `homelab-control`, CapEff 0; broker socket 660 `root:homelab-control`, socket-activated, root with CapEff = CAP_SETUID\|CAP_SETGID, NoNewPrivs, other users refused; login, `__Host-` cookie, CSRF, all M1 routes/scopes; real Webmin 2.670 legacy detection; filesystems match the host's `/proc/1/mounts`; broker-down degradation; remove + purge | **40/40** | `vm-26.04/01-systemd-smoke.log` |
| `systemd-analyze security` exposure | hc-api **1.6 OK**, broker **1.5 OK** | same |
| Rollback: remove keeps config and frees port/socket; reinstall reuses accounts, not auto-enabled; purge removes `/etc/homelab-control`; upstream Webmin answers throughout | **13/13** | `vm-26.04/02-rollback.log` |
| Browser e2e (Playwright 1.63.0, Chromium 153) against the packaged stack: navigation, Services, Hardware vs API, Legacy, mobile, history/deep link, broker-stopped degradation, no CSP violations | **25/25** | `vm-26.04/03-browser-axe.log` |
| axe-core 4.14.0, WCAG 2.1 A/AA, on services, hardware, legacy, overview+degraded-nav | **0 violations of any impact** | `vm-26.04/screenshots/axe-report.json` |
| Final removal: purged, no processes, units or listener left | **4/4** | `vm-26.04/04-final-removal.log` |

Screenshots: `vm-26.04/screenshots/m1-0{1..5}-*.png`. Metadata: `vm-26.04/environment.txt`,
`vm-26.04/host-environment.txt`. No credentials or keys appear in the evidence (checked).

**Found by this run and fixed before the final run:**
- *Product:* Overview reported hc-api's systemd sandbox as the host — `/` shown read-only and service-only
  bind mounts listed (seen in the first run's screenshots on `f829cbb8`). Fixed in `738fa37b`: reads
  `/proc/1/mounts`; the sandbox fallback is labelled and claims no read-only state. Three checks added.
- *Harness/tests (not product):* qemu-img format parsing; missing `apt-get update` in fresh cloud images;
  Playwright 1.56 cannot run on Ubuntu 26.04 (≥ 1.61 needed; CI and harness pinned to 1.63.0); Hardware
  e2e assertions assumed a container and now compare the page with the API; axe can no longer silently skip.
- *Host:* ufw dropped libvirt DHCP on `virbr0`; the owner changed ufw so guests on `virbr0` get DHCP (exact rules not recorded here; recommended virbr0-only rules are in `dev/vm/README.md`).

The earlier run on `f829cbb8` (37/37, 13/13, 25/25, 4/4; commit `8bc5af03`) is superseded by this one;
its files were overwritten by the rerun.

**CI on `738fa37b`** (run 38866773032): all 15 jobs green, including the new filesystem checks under real
systemd on Ubuntu 24.04 (runner) and 26.04 (container), and the 9-distro install matrix.

## 5. Still not verified
- Ubuntu 22.04 and other distributions under real systemd in a **VM** (CI containers/runner only).
- Long-running behaviour, upgrades from a previous package version, and multi-host anything.
