# ADR-0009: Release pipeline, corresponding source and signing

**Status:** Proposed (2026-10-16). The dry run is implemented and runs in CI; **publishing is not enabled**.
**Date:** 2026-10-16
**Deciders:** Project owner
**Refines:** ADR 0005 item 5. **Finding:** LA-13 (`docs/compliance/LICENSE-AUDIT.md`).

## Context
Releases will contain GPL-3.0-or-later binaries that embed MIT and OFL-1.1 components and statically link
the Go standard library. GPL-3.0 §6 requires the Corresponding Source to accompany (or be offered with)
the object code. No release process existed (LA-13).

## Decision (proposed)
Full design: `docs/compliance/RELEASE-PIPELINE.md`.
1. **Source archive** per release: repository at the tag (excluding `compat/webmin/`, unused by any
   binary), plus `vendor-sources/npm/` with the integrity-checked npm sources of everything compiled into
   the UI bundle, plus `RELEASE-BUILDINFO` (exact toolchain) and `BUILDING.md`.
2. **Built from the archive**: release binaries and packages are built only from the extracted source
   archive; a second build from Git must be byte-identical (`scripts/release-bundle.sh`, CI
   `release-dry-run`).
3. **GitHub Releases** created as **drafts** by a tag-triggered workflow behind a protected `release`
   environment; binaries never published without their source archive in the same release.
4. **Signing:** keyless Sigstore build-provenance attestations from CI **and** an offline maintainer
   OpenPGP signature over `SHA256SUMS` (hardware-token subkey, offline primary, no keys in CI).

## Options considered
| Option | Pros | Cons |
|---|---|---|
| A. Attestations + offline OpenPGP (proposed) | Two independent trust roots; no secret in CI; works for distro tooling | Manual step per release; hardware token needed |
| B. Sigstore attestations only | No key management | Trust rests on GitHub/OIDC alone; distro repos expect OpenPGP |
| C. OpenPGP key in GitHub secrets | Fully automated | A CI or workflow compromise can sign anything; key exfiltration risk |
| D. Written offer for source instead of shipping it | Smaller downloads | Ongoing 3-year obligation; easy to break; GitHub makes shipping source trivial |

## Consequences
- Releases are reproducible by third parties with the recorded toolchain.
- The maintainer must reproduce and sign each release; releases cannot be fully automatic.
- Before enabling: pin exact Go/Node versions and `govulncheck`; build the Arch package from the archive.

## Action items
1. [ ] Owner approves or amends this ADR.
2. [ ] Owner creates the OpenPGP key/hardware token; fingerprint in `docs/security/RELEASE-KEYS.md`.
3. [ ] Protected `release` environment; tag-triggered draft-release workflow (separate PR).
