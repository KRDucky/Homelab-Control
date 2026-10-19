# Release pipeline and signing (design — not enabled)

**Status:** design **accepted** by the owner (ADR 0009, 2026-10-16). **Publishing is not enabled**: the
prerequisites in ADR 0009 §5 are release blockers (LA-13). The repository has no tag-triggered workflow
and CI runs with `contents: read`. What exists and runs today is the **dry run** (§2).

## 1. What a release publishes (GitHub Releases)
| File | Content |
|---|---|
| `homelab-control-<v>-source.tar.gz` | Complete corresponding source: repository at the tag (minus `compat/webmin/`, which no binary uses), `vendor-sources/npm/` (published sources of everything compiled into the UI bundle, integrity-checked), `RELEASE-BUILDINFO`, `BUILDING.md` |
| `homelab-control-<v>-linux-<arch>.tar.gz` | `hc-api`, `hlctl`, `hc-broker` + `LICENSE.md`, `NOTICE.md`, `THIRD-PARTY-NOTICES.md` |
| `homelab-control_<v>_<arch>.deb`, `homelab-control-<v>-….rpm`, `homelab-control-<v>-….pkg.tar.zst` | Packages (each carries the licence and notice files; the `.deb` also carries the DEP-5 `copyright`) |
| `LICENSE.md`, `NOTICE.md`, `THIRD-PARTY-NOTICES.md` | Licence texts and notices, also as separate assets |
| `homelab-control-<v>.spdx.json` | SPDX 2.3 SBOM |
| `RELEASE-BUILDINFO` | Commit, `SOURCE_DATE_EPOCH`, exact Go/Node/npm versions, build commands |
| `SHA256SUMS`, `SHA256SUMS.asc` | Checksums of every file above; detached OpenPGP signature (§4) |
| Sigstore attestations | SLSA build provenance for every file (§4), stored by GitHub, verifiable with `gh attestation verify` |

Binaries are published **only together with** the source archive of the same commit, in the same release.
For as long as binaries are offered, the matching source stays in the same release (GPL-3.0 §6(d)).

## 2. Dry run (implemented, runs in CI on every PR)
`scripts/release-bundle.sh <version> <out>`, CI jobs `release-dry-run` (runs **a** and **b** on two
independent runners), `release-dry-run-arch` (a, b) and `release-repro`:
1. Writes the source archive from the committed tree and fetches the npm sources, verifying each
   against the lockfile's sha512 integrity.
2. Extracts the archive into an empty directory and builds binaries, `.deb` and `.rpm` **only from it**.
3. Builds the same commit from a Git worktree; binaries **and** `.deb`/`.rpm` must be byte-identical.
4. Checks package payloads (`scripts/package-inventory.sh`) and writes `SHA256SUMS`.
5. `release-dry-run-arch`: in a fresh Arch container, extracts the same source archive, takes the binaries
   built from it and runs `packaging/arch/build-pkg.sh` from the archive.
6. `release-repro`: compares every file of run a with run b (source archive, binaries tarball, `.deb`,
   `.rpm`, Arch package, SBOM, notices, `RELEASE-BUILDINFO`).
The result is uploaded as 7-day CI artifacts, not as a release.

### Reproducibility: what is and is not demonstrated
| Claim | Evidence | Status |
|---|---|---|
| Binaries built from the source archive are byte-identical to binaries built from Git at the same commit | `release-bundle.sh` step 3, every CI run and locally | **Demonstrated** (same machine, same toolchain) |
| `.deb` and `.rpm` from the archive tree = from the Git tree | step 3b, CI and locally | **Demonstrated** (same machine) |
| All release files identical across two complete runs | Locally: two runs with separate Go build caches, 9/9 files identical incl. `.rpm` (2026-10-16). CI: `release-repro`, see PR #5 | **Demonstrated** on GitHub's `ubuntu-24.04` runners and one local container — not on other distributions or toolchain versions |
| Arch package reproducible | `release-repro` compares the two Arch builds | Demonstrated only if `release-repro` is green; same container image and package set required (`.BUILDINFO` lists installed packages) |
| Reproducible with a *different* Go or Node patch version, or on another distribution | — | **Not demonstrated and not expected**: Go embeds its version; use the toolchain in `RELEASE-BUILDINFO` |
| Independent third-party rebuild on their own hardware | — | **Not demonstrated** (owner reproduction is part of the signing step, ADR 0009) |

Fixed for this: RPM file mtimes and build host were nondeterministic (two builds differed); now
`clamp_mtime_to_source_date_epoch` and a fixed `_buildhost` make them identical. The Arch build used a
random temporary directory that `makepkg` records in `.BUILDINFO`; it now uses a fixed path.

## 3. Publishing workflow (to add after approval)
Trigger: push of a **signed** tag `vX.Y.Z` made by the maintainer. Sketch:
```yaml
on: { push: { tags: ['v*.*.*'] } }
permissions: { contents: read }
jobs:
  build:            # same steps as release-dry-run, plus the Arch package job
    permissions: { contents: read, id-token: write, attestations: write }
    steps: [checkout tag, verify tag signature against docs/security/RELEASE-KEYS.md,
            pin exact Go/Node versions, scripts/release-bundle.sh "${TAG#v}" dist/release,
            actions/attest-build-provenance on dist/release/*, upload artifact]
  publish:
    needs: build
    environment: release          # required reviewer: the owner; no secrets stored
    permissions: { contents: write }
    steps: [download artifact, create a DRAFT GitHub release with all files]
```
The release stays a **draft** until the maintainer has (a) rebuilt from the source archive locally and
matched `SHA256SUMS`, and (b) attached `SHA256SUMS.asc` (§4). Only then is it published.
Everything needed before enabling is listed as prerequisites P1–P10 in ADR 0009 §5 (exact toolchain pins,
pinned `govulncheck`, Actions runtime upgrade, signed tags, protected environment, attestations, offline
signing, versioned SBOM). The Arch package is already built from the archive in the dry run.

## 4. Signing strategy
Two independent layers, so that neither GitHub alone nor one stolen key can produce a trusted release:

1. **Build provenance (keyless).** GitHub artifact attestations (Sigstore, OIDC-bound to this repository
   and workflow, recorded in a public transparency log). Shows *which workflow run on which commit*
   produced each file. No long-lived key exists to manage or leak.
2. **Maintainer signature (offline key).** The maintainer signs `SHA256SUMS` with an OpenPGP key after
   independently reproducing the build. Shows that *the maintainer approved these exact bytes*.
   Git tags are signed with the same key.

Distribution-repository signing (APT `Release`/`InRelease`, `rpmsign`, Arch `.sig`) comes only when
package repositories exist; it would use a separate subkey under the same rules.

### Key-management requirements
- Ed25519 OpenPGP **primary key generated and kept offline** (air-gapped machine or live system), used
  only to certify subkeys and sign revocations.
- **Signing subkey on a hardware token** (OpenPGP smartcard/YubiKey) with touch-to-sign; the private key
  never exists on a networked disk. Expiry ≤ 2 years, extended deliberately.
- **No release signing key in GitHub secrets or CI.** CI has only the keyless OIDC identity.
- Encrypted offline backups of the primary key in two physical locations; a **revocation certificate**
  generated at creation and stored separately.
- Fingerprint published in `docs/security/RELEASE-KEYS.md`, in the README, in every release note, and on
  a public keyserver; verification instructions in `BUILDING.md`.
- **Rotation:** new subkey before expiry, announced in a signed commit. **Compromise:** publish the
  revocation, re-sign current releases with a new key, document in `SECURITY.md`.
- Second maintainer (when one exists) holds their own key; releases list who signed.

## 5. Decisions (2026-10-16) and remaining owner actions
Accepted: GitHub Releases; Sigstore attestations plus hardware-backed OpenPGP maintainer signatures;
offline primary key with a signing subkey on a hardware token; `compat/webmin/` excluded from the source
archive. Remaining owner actions (not delegable): create the key and token, publish the fingerprint,
create the protected `release` environment, approve the release-workflow PR and the first version number.
