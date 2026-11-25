# Packaging plan

| Target | Format | M0 status | Next step |
|---|---|---|---|
| Debian stable / Ubuntu LTS | `.deb` (`homelab-control`, replaces `homelab-control-api`) | M1: built; **installed and run under real systemd (PID 1) on Ubuntu 22.04 and 24.04** disposable containers; install test without systemd on both. Debian itself: CI matrix only (mirror unreachable here). Ubuntu 26.04 LTS: CI install test and real-systemd (259.5) container job pass on `da41162a` (run 38852873505) and `738fa37b` (run 38866773032); full M1 suite passes in a KVM VM on the owner's workstation (`docs/evidence/m1/vm-26.04/`). | lintian; source package (M14). |
| Fedora | binary `.rpm` (`packaging/fedora/build-rpm.sh`) | M1: **built** (rpmbuild on Ubuntu); install test only in CI (`fedora:latest`), not yet run | Source RPM/mock (M14). |
| openSUSE Leap/Tumbleweed | same binary `.rpm` (zypper) | M1: depends on `/usr/sbin/useradd` (works with openSUSE `shadow`); CI-only, not yet run | OBS build (M14). |
| Arch / CachyOS | `.pkg.tar.zst` (`packaging/arch/build-pkg.sh`, makepkg) | M1: CI-only (no Arch environment reachable here), not yet run | Run on owner's CachyOS host; AUR/source PKGBUILD (M14). |

2026-11-22: the first release ships the Debian/Ubuntu `.deb` only. The Fedora, openSUSE and Arch rows stay as later work. CI does not build or install them.

## Package contents (all formats)
`/usr/bin/hc-api`, `/usr/bin/hlctl`, `/usr/lib/homelab-control/hc-broker`, four systemd units
(`homelab-control.service`, `homelab-control-broker.socket`, `homelab-control-broker.service`,
and unused closed `homelab-control-storage-helper.service` with no `[Install]`/enable
step — do not `systemctl enable` it; hardened,
**not enabled on install**), sysusers entry for `homelab-control`, `/etc/homelab-control/hc-api.env` (conffile), license and notice files.
The package contains no Webmin code (ADR 0016). It uses its own port (9180 default behind a TLS proxy,
9443 suggested for direct TLS) and never writes another administration tool's configuration.

## Release artifacts (from M14; partially from M1)
Tagged source tarball, SHA256SUMS, SPDX SBOM, NOTICE/LICENSE, changelog (signed tags/artifacts deferred: ADR 0009 Phase 3),
uninstall guide (`docs/dev/REMOVAL.md`). Built only in CI from tags (ADR 0005/0006).
