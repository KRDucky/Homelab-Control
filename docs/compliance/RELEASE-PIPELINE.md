# Release pipeline and signing (design — not enabled)

**Status:** proposed for owner approval (ADR 0009). LA-13. **Nothing in this document publishes anything
yet.** The repository has no tag-triggered workflow and CI runs with `contents: read`. What exists and
runs today is the **dry run** (§2).

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
`scripts/release-bundle.sh <version> <out>`, CI job `release-dry-run`:
1. Writes the source archive from the committed tree and fetches the npm sources, verifying each
   against the lockfile's sha512 integrity.
2. Extracts the archive into an empty directory and builds binaries, `.deb` and `.rpm` **only from it**.
3. Builds the same commit from a Git worktree; the binaries must be byte-identical. This proves the
   archive is complete and the build reproducible.
4. Checks package payloads (`scripts/package-inventory.sh`) and writes `SHA256SUMS`.
The result is uploaded as a 7-day CI artifact, not as a release. Two local runs on the same commit gave
identical `SHA256SUMS` for every file (2026-10-16).

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
Changes needed before enabling: exact Go/Node versions pinned in the workflow (CI uses `1.26.x` today),
`govulncheck` pinned to a version (currently `@latest`), and the Arch package built from the archive.

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

## 5. Owner decisions needed before enabling
1. Approve this design (ADR 0009) or choose differently (e.g. Sigstore-only, no OpenPGP).
2. Create the OpenPGP key and hardware token per §4 and publish the fingerprint.
3. Create the protected `release` environment with the owner as required reviewer.
4. Approve the first version number and whether `compat/webmin/` stays out of the source archive.
