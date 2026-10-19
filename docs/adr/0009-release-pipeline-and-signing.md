# ADR-0009: Release pipeline, corresponding source and signing

**Status:** **Accepted** (2026-10-16, owner) as the release design. **Publishing is not enabled**: the
prerequisites in §5 are release blockers (LA-13), and enabling the tag-triggered workflow needs its own
reviewed PR. This acceptance does not authorize publishing a release.
**Date:** 2026-10-16
**Deciders:** Project owner
**Refines:** ADR 0005 item 5. **Finding:** LA-13 (`docs/compliance/LICENSE-AUDIT.md`).

## Context
Releases will contain GPL-3.0-or-later binaries that embed MIT and OFL-1.1 components and statically link
the Go standard library. GPL-3.0 §6 requires the Corresponding Source to accompany (or be offered with)
the object code. No release process existed (LA-13).

## Decision (accepted 2026-10-16)
Full design: `docs/compliance/RELEASE-PIPELINE.md`.
1. **Distribution:** GitHub Releases, created as **drafts** by a tag-triggered workflow behind a protected
   `release` environment that requires the owner's approval. Binaries are never published without the
   source archive of the same commit in the same release.
2. **Source archive** per release: repository at the tag **excluding `compat/webmin/`** (owner-approved: no
   released artifact needs it, and it stays available in Git at the tag), plus `vendor-sources/npm/` with
   the integrity-checked npm sources of everything compiled into the UI bundle, `RELEASE-BUILDINFO` (exact
   toolchain) and `BUILDING.md`.
3. **Built from the archive:** binaries and all packages (DEB, RPM, Arch) are built from the extracted
   source archive; binaries and packages must be byte-identical to a build from Git and across two
   independent runners.
4. **Signing:** keyless **Sigstore build-provenance attestations** from CI **and** an independently
   verified **OpenPGP maintainer signature** over `SHA256SUMS`, made after the maintainer reproduces the
   build. Key architecture: **offline primary key**, **signing subkey on a hardware token**, no signing key
   in CI. Key generation, fingerprint publication and the `release` environment are owner-controlled
   actions; no tool or agent handles the private keys.

## Options considered
| Option | Pros | Cons |
|---|---|---|
| A. Attestations + offline OpenPGP (accepted) | Two independent trust roots; no secret in CI; works for distro tooling | Manual step per release; hardware token needed |
| B. Sigstore attestations only | No key management | Trust rests on GitHub/OIDC alone; distro repos expect OpenPGP |
| C. OpenPGP key in GitHub secrets | Fully automated | A CI or workflow compromise can sign anything; key exfiltration risk |
| D. Written offer for source instead of shipping it | Smaller downloads | Ongoing 3-year obligation; easy to break; GitHub makes shipping source trivial |

## 5. Release prerequisites (each is a release blocker until done)
| # | Requirement | State (2026-10-16) | Owner / where |
|---|---|---|---|
| P1 | **Exact toolchain pins** in the release workflow: Go patch version (CI uses `1.26.x`), Node.js patch version (CI uses `22`), npm version; recorded in `RELEASE-BUILDINFO` and checked against it | Versions are recorded, **not pinned** | Release-workflow PR |
| P2 | **Pinned build/check tools**: `govulncheck` (CI installs `@latest`), Playwright, spdx-tools, reuse, lintian source/version noted | spdx-tools 0.8.3, reuse 6.2.0, Playwright 1.63.0 pinned; **govulncheck unpinned** | Release-workflow PR |
| P3 | **GitHub Actions** pinned by full SHA (done) **and moved off the deprecated Node 20 runtime** (checkout, setup-go, setup-node, setup-python, upload/download-artifact currently trigger GitHub's Node 20 deprecation warning) | SHA-pinned; **runtime upgrade pending** | Separate dependency PR (ADR 0006 review) |
| P4 | **Signed tags**: release tags signed with the maintainer key; the workflow verifies the signature against the published fingerprint before building | Not implemented | Owner key + workflow PR |
| P5 | **Protected `release` environment** with the owner as required reviewer; publish job only in that environment; `contents: write` only there | Not configured | Owner (repository settings) |
| P6 | **Sigstore attestations** (`actions/attest-build-provenance`, `id-token: write`, `attestations: write` in the build job only) for every release file, plus verification instructions (`gh attestation verify <file> --repo KRDucky/Homelab-Control---Webmin-Fork`) in `BUILDING.md` | Designed, not implemented | Workflow PR |
| P7 | **Offline OpenPGP signing**: key generated offline, subkey on hardware token, fingerprint in `docs/security/RELEASE-KEYS.md`, README and release notes; documented rotation (before expiry), encrypted offline backups in two locations, revocation certificate stored separately, compromise procedure in `SECURITY.md` | Requirements written (`RELEASE-PIPELINE.md` §4) | Owner |
| P8 | **Complete release set**: source archive, binaries tarball, DEB, RPM, Arch package, SBOM, notices, DEP-5 copyright (in the .deb), `RELEASE-BUILDINFO`, `SHA256SUMS`(+`.asc`) | All except signatures/attestations produced by the dry run | Done in dry run; signing per P6/P7 |
| P9 | **Same-commit guarantee**: binaries and packages built only from the source archive of the tagged commit; archive build = Git build; two independent runs identical | **Implemented in CI** (`release-dry-run` a/b, `release-dry-run-arch` a/b, `release-repro`) | Keep in the release workflow |
| P10 | SBOM stamped with the release version (it currently describes the repository state) | Not implemented | Workflow PR |

## Consequences
- Releases are reproducible by third parties with the recorded toolchain (once P1 pins it).
- The maintainer must reproduce and sign each release; releases cannot be fully automatic.

## Action items
1. [x] Owner accepted the design (2026-10-16).
2. [ ] Owner: OpenPGP key and hardware token; publish the fingerprint (P4, P7).
3. [ ] Owner: protected `release` environment (P5).
4. [ ] PR: release workflow implementing P1, P2, P4, P6, P10 (draft releases only).
5. [ ] PR: GitHub Actions runtime upgrade (P3).
