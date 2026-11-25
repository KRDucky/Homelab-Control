# Distribution Support and Packaging

| Target | Format | Package manager | Test priority |
|---|---|---|---|
| Debian stable | `.deb` | APT | Tier 1 |
| Ubuntu LTS | `.deb` | APT | Tier 1 |
| Fedora current supported | `.rpm` | DNF | Later. Not a release gate. After the Debian/Ubuntu production release |
| openSUSE Leap/Tumbleweed | `.rpm` | zypper | Later. Not a release gate. After the Debian/Ubuntu production release |
| Arch Linux | `.pkg.tar.zst` / PKGBUILD | pacman | Later. Not a release gate. After the Debian/Ubuntu production release |
| Derivatives (CachyOS, etc.) | Compatible package | distro native | Tier 2, validated separately |

**zypper is a package manager, not a separate package file format**; openSUSE uses RPM.

Owner 2026-11-20: ignore SELinux for this release. The Debian package already ships the AppArmor profile `hc-storage-probe`. Do not add another profile unless a Debian or Ubuntu run shows that one is not enough.

## Release artifacts
Tagged source archive, Git repository, checksums, SBOM, license notices, signed release artifacts/repo metadata where supported, changelog, migration notes, rollback and uninstall guide. Maintain distro-specific dependency lists and systemd units; avoid a universal installer that bypasses package management.

## Repository policy
Official distro repo first, then curated vendor repositories with provenance/signature and explicit user consent. Show package origin and planned repo modifications. On Arch, distinguish official repos from AUR builds and warn that AUR PKGBUILDs are third-party scripts.

## VM verification gate for functioning release targets

Before claiming a distribution release is fully functioning or supported, install the real built artifact on a disposable VM for that distribution/version and verify startup, UI/API operations, integration with that distro's native tools, upgrade/removal where relevant, permission denials and recovery. Archive commit, artifact hash, exact OS version, commands and results. Containers, package-build CI and parser fixtures supplement but do not replace real VM evidence. A read-only or design checkpoint is not a fully functioning release. Follow the central gate in `08-TESTING-AND-ACCEPTANCE.md`.

Initial release scope remains Debian/Ubuntu `.deb` only. Other package ecosystems are deferred until the fully functioning Debian/Ubuntu release is stable; entries in the platform table are **longer-term targets**, not current release gates.

## Build and CI
Reproducible builds in isolated builders; package install/upgrade/remove tests; fresh and upgraded installs; unit/integration/e2e tests per distro; package-signing infrastructure and provenance attestation for public releases. Provide source-build instructions even if binary packages are not yet published.

## Installer behavior
Never overwrite unmanaged configs without explicit approval. Backup configuration and database before migration. Respect distro firewall, service and filesystem conventions.
