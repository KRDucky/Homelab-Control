# Packaging plan

| Target | Format | M0 status | Next step |
|---|---|---|---|
| Debian stable / Ubuntu LTS | `.deb` (`homelab-control`, replaces `homelab-control-api`) | M1: built; **installed and run under real systemd (PID 1) on Ubuntu 22.04 and 24.04** disposable containers; install test without systemd on both. Debian itself: CI matrix only (mirror unreachable here). Ubuntu 26.04 LTS: CI install test and real-systemd (259.5) container job pass on `da41162a` (run 38852873505). | lintian; source package (M13). |
| Fedora | binary `.rpm` (`packaging/fedora/build-rpm.sh`) | M1: **built** (rpmbuild on Ubuntu); install test only in CI (`fedora:latest`), not yet run | Source RPM/mock (M13). |
| openSUSE Leap/Tumbleweed | same binary `.rpm` (zypper) | M1: depends on `/usr/sbin/useradd` (works with openSUSE `shadow`); CI-only, not yet run | OBS build (M13). |
| Arch / CachyOS | `.pkg.tar.zst` (`packaging/arch/build-pkg.sh`, makepkg) | M1: CI-only (no Arch environment reachable here), not yet run | Run on owner's CachyOS host; AUR/source PKGBUILD (M13). |

## Package contents (all formats)
`/usr/bin/hc-api`, `/usr/bin/hlctl`, `/usr/lib/homelab-control/hc-broker`, three systemd units
(`homelab-control.service`, `homelab-control-broker.socket`, `homelab-control-broker.service`; hardened,
**not enabled on install**), sysusers entry for `homelab-control`, `/etc/homelab-control/hc-api.env` (conffile), license and notice files.
Webmin is **not** bundled in the M0 package; upstream Webmin is installed separately from upstream or
distro packages. Bundling `compat/webmin` (with module pruning per `MODULE-INVENTORY.md`) is an M1/M2
decision.

## Coexistence with an existing Webmin
Detected read-only (`/etc/webmin/miniserv.conf` existence, `/etc/webmin/version`). The package never
writes under `/etc/webmin` and uses a different port (9180 default behind TLS proxy, 9443 suggested for
direct TLS) so both can run side by side. Migration of Webmin users/config is out of scope until a
migration tool with backup and rollback exists.

## Release artifacts (from M13; partially from M1)
Tagged source tarball, SHA256SUMS, SPDX SBOM, NOTICE/LICENSE, signed tags/artifacts, changelog,
rollback/uninstall guide (`docs/dev/ROLLBACK.md`). Built only in CI from tags (ADR 0005/0006).
