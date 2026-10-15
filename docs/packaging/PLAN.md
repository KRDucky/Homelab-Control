# Packaging plan

| Target | Format | M0 status | Next step |
|---|---|---|---|
| Debian stable / Ubuntu LTS | `.deb` (`homelab-control-api`) | **Built and install/remove/purge tested** in an Ubuntu 24.04 container (no systemd running there, so the unit was installed but not started). Unsigned dev package. | Run under systemd in the Vagrant VM (M1); lintian; proper `debian/` source package (M13). |
| Fedora | `.rpm` | Draft spec, untested | Build in a Fedora VM/mock (M1–M4). |
| openSUSE Leap/Tumbleweed | `.rpm` (zypper) | Notes only | OBS build (M13). |
| Arch / CachyOS | PKGBUILD → `.pkg.tar.zst` | Draft, untested | `makepkg` on the owner's CachyOS host (M1); Tier 2 validation for CachyOS. |

## Package contents (all formats)
`/usr/bin/hc-api`, `/usr/bin/hlctl`, systemd unit (hardened, **not enabled on install**), sysusers
entry for `homelab-control`, `/etc/homelab-control/hc-api.env` (conffile), license and notice files.
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
