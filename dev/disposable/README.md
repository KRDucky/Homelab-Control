# Disposable development environment

Two supported disposable environments. **Never run these scripts on a production host**: the
Webmin baseline installs a root-run upstream Webmin and the scripts refuse to run if `/etc/webmin`
already exists.

## 1. Disposable container (what M0 was verified in)
Any throwaway Debian/Ubuntu container or cloud sandbox with Perl, Go ≥ 1.24, Node ≥ 22, curl and
(optionally) Python Playwright:

```bash
make build
sudo WEBMIN_PASS='choose-a-throwaway-password' dev/disposable/webmin-baseline.sh
WEBMIN_USER=admin WEBMIN_PASS='…' tests/integration/webmin-baseline-login.sh
# Homelab Control API + browser e2e, as an unprivileged user:
HC_LEGACY_URL=http://127.0.0.1:10000/ tests/integration/run-hc-e2e.sh
```
M0 evidence (Ubuntu 24.04 container, 2026-10-15) is in `docs/evidence/`.

## 2. Disposable VM (recommended on your workstation)
See `../vm/README.md` — libvirt (qemu:///system) harness with the `hc-test` pool, Ubuntu 26.04/24.04 cloud images (replaces the M0 Vagrantfile, which was never executed)
(no hypervisor was available in the build environment).

## Storage testing rule
Storage work (M5+) uses loop devices, sparse files and VM virtual disks only. None of these scripts
create, partition or format anything.
