# M0 test evidence

Environment: disposable Ubuntu 24.04.5 cloud container (2 vCPU, 7.8 GiB, kernel 6.18.44), Go 1.24.7,
Node 22.22.0, Perl 5.38.2, Playwright Chromium. No hypervisor (no KVM) and no container-registry access
were available, so **no VM was used**; the container itself was the disposable environment and was
discarded after the session. Commit under test: `05adffd2`.

| File | What it shows | Result |
|---|---|---|
| `reproducible-build.txt` | Two clean-worktree builds of 05adffd2, SHA-256 compared | identical |
| `go-tests.txt` | `go test -v -cover ./...` as unprivileged user | 44 passed, 0 failed, 0 skipped |
| `frontend-tests.txt` | Vitest | 8 passed |
| `webmin-2.670-prove.txt` | Upstream Webmin `prove -lr t` on the pinned tree | 26 files, 4161 tests, PASS (1 file skipped by upstream: VM-only) |
| `webmin-2.670-baseline-login.txt` | `dev/disposable/webmin-baseline.sh` + `tests/integration/webmin-baseline-login.sh` | 9 passed, 0 failed |
| `hc-e2e.txt` | `tests/integration/run-hc-e2e.sh`: live API security checks + Playwright | 15 + 9 passed, 0 failed |
| `deb-lifecycle.txt` | Dev `.deb` install, modes, service-user run, refusal paths, remove, purge | as expected (systemd not running in container) |
| `license-inventory.txt` | Shipped vs dev npm licenses, Go modules | shipped: MIT ×3, OFL-1.1 ×1; Go: stdlib only |
| `screenshots/` | Login, login error, overview (desktop dark, mobile, light), legacy Webmin login | real data from the container |

Not run here: `govulncheck` (Go proxy blocked by network policy; runs in CI), the Vagrant VM, systemd
start of the unit, Fedora/openSUSE/Arch packages.
