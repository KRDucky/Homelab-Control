# Distribution Support and Packaging

| Target | Format | Package manager | Test priority |
|---|---|---|---|
| Debian stable | `.deb` | APT | Tier 1 |
| Ubuntu LTS | `.deb` | APT | Tier 1 |
| Fedora current supported | `.rpm` | DNF | Tier 1 |
| openSUSE Leap/Tumbleweed | `.rpm` | zypper | Tier 1 |
| Arch Linux | `.pkg.tar.zst` / PKGBUILD | pacman | Tier 1 |
| Derivatives (CachyOS, etc.) | Compatible package | distro native | Tier 2, validated separately |

**zypper is a package manager, not a separate package file format**; openSUSE uses RPM.

## Release artifacts
Tagged source archive, Git repository, checksums, SBOM, license notices, signed release artifacts/repo metadata where supported, changelog, migration notes, rollback and uninstall guide. Maintain distro-specific dependency lists and systemd units; avoid a universal installer that bypasses package management.

## Repository policy
Official distro repo first, then curated vendor repositories with provenance/signature and explicit user consent. Show package origin and planned repo modifications. On Arch, distinguish official repos from AUR builds and warn that AUR PKGBUILDs are third-party scripts.

## Build and CI
Reproducible builds in isolated builders; package install/upgrade/remove tests; fresh and migrated Webmin state; unit/integration/e2e tests per distro; package-signing infrastructure and provenance attestation for public releases. Provide source-build instructions even if binary packages are not yet published.

## Installer behavior
Never overwrite unmanaged configs without explicit approval. Detect existing Webmin installation and explain fork coexistence/migration limitations. Backup configuration and database before migration. Respect distro firewall, service and filesystem conventions.
