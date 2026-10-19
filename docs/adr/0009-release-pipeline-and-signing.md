# ADR-0009: Release pipeline, corresponding source and signing

**Status:** **Accepted, revised 2026-10-19 (owner).** Releases are phased. **Phase 1** (unsigned GitHub
development/prereleases) is the current target. **Phase 2** (keyless attestations) and **Phase 3**
(maintainer signing) are optional and deferred. The revision supersedes the 2026-10-16 decision that
made hardware-backed OpenPGP signatures mandatory. **No release workflow exists yet**, and nothing is
published automatically.
**Date:** 2026-10-16 (accepted), 2026-10-19 (revised)
**Deciders:** Project owner
**Refines:** ADR 0005 item 5. **Finding:** LA-13 (`docs/compliance/LICENSE-AUDIT.md`).

## Context
Releases will contain GPL-3.0-or-later binaries that embed MIT and OFL-1.1 components and statically link
the Go standard library. GPL-3.0 §6 requires the Corresponding Source to accompany (or be offered with)
the object code. The 2026-10-16 design required, before any release:
- Sigstore attestations;
- an OpenPGP maintainer signature made with an offline primary key and a hardware-token subkey;
- signed tags.

On 2026-10-19 the owner **deferred hardware tokens, OpenPGP keys and mandatory signing** to a future
decision, so that development builds can be shared without first setting up key infrastructure.

## Decision (revised 2026-10-19)
Full design: `docs/compliance/RELEASE-PIPELINE.md`.

### Phase 1: unsigned GitHub development/prereleases (current target)
1. **What is published:** GitHub Releases, marked as **prerelease**. The title and notes start with
   **"UNSIGNED DEVELOPMENT BUILD"**, and each says how to verify the files (checksums plus a
   reproducible rebuild) and that no cryptographic signature exists.
2. **Unchanged guarantees:**
   - a complete corresponding-source archive (repository at the tag minus `compat/webmin/`, npm sources
     of everything compiled in, `RELEASE-BUILDINFO`, `BUILDING.md`);
   - binaries and DEB/RPM/Arch packages built only from that archive;
   - archive build equals Git build, and two independent runs are identical;
   - notices, the DEP-5 copyright file, licence metadata, an SPDX 2.3 SBOM and `SHA256SUMS`.
3. **Manual owner approval before anything is public:**
   - the workflow only creates a **draft** prerelease;
   - the owner reviews it and publishes it by hand.
4. **Never automatic:** no job publishes automatically, and no job runs on every push to `main`.
5. **GitHub `release` environment:**
   - Use a protected environment with the owner as required reviewer where the repository's plan
     offers it.
   - Its absence does not block development prereleases. The manual draft → publish step is the
     approval gate in every case.
6. **No signing material:** no private key, signing secret or hardware token is created, requested or
   stored by the project or its CI.

### Phase 2: keyless attestations (optional, deferred)
GitHub artifact attestations (Sigstore build provenance through OIDC; no long-lived key) may be added
later as an enhancement. They need `id-token: write` and `attestations: write` in the build job only.
Users verify with `gh attestation verify <file> --repo KRDucky/Homelab-Control---Webmin-Fork`. No
hardware key or manually generated secret is involved. They are **not required** for Phase 1.

### Phase 3: maintainer signing (optional, deferred to a future owner decision)
Deferred:
- OpenPGP release keys;
- hardware tokens (YubiKey or other smartcards);
- offline key management (primary key, backups, rotation, revocation);
- signed tags;
- package-repository signing (APT `InRelease`, `rpmsign`, Arch `.sig`).

The 2026-10-16 key-management requirements are kept in `RELEASE-PIPELINE.md` §6 as **reference only**,
not as current requirements. Stable or "production" releases, and any distribution through package
repositories, should not happen before the owner revisits Phase 3.

## Options considered (2026-10-19)
| Option | Pros | Cons |
|---|---|---|
| A. Phased: unsigned prereleases now; attestations and signing optional later (accepted) | Releases possible without key infrastructure; reproducibility and checksums still let users verify; signing can be added without redesign | No cryptographic proof of origin: users rely on GitHub account security and their own rebuild |
| B. Keep mandatory attestations + OpenPGP (the 2026-10-16 design) | Strongest assurance | Blocks every release on hardware and key setup the owner has deferred |
| C. Attestations mandatory, OpenPGP deferred | Provenance without key management | Still a prerequisite to build, verify and document now; the owner chose to make it optional |
| D. No releases until signing exists | No unsigned artifacts in circulation | Testers have to build from source |

## Consequences
- Downloads are protected only by HTTPS, GitHub account security and `SHA256SUMS` published alongside
  them. Checksums from the same page detect corruption, not tampering by someone who controls the
  release. The independent check is rebuilding from the source archive (`BUILDING.md`). Release notes
  must say this plainly.
- Phase 1 artifacts must not be described as signed, verified or production-ready.
- Adding Phase 2 or Phase 3 later does not change the artifact set; it only adds attestations or
  `.asc` files.

## 5. LA-13 checklist
**LA-13 is not resolved.** It can be marked resolved only when the Phase 1 workflow is implemented and a
first draft prerelease has been produced and verified against every mandatory item below.

### 5.1 Mandatory for a Phase 1 unsigned development prerelease
| # | Requirement | State (2026-10-19) |
|---|---|---|
| R1 | **Corresponding source** with every release: source archive of the tagged commit, npm sources, `RELEASE-BUILDINFO`, `BUILDING.md`; `compat/webmin/` excluded | Implemented in the dry run (`scripts/release-bundle.sh`) |
| R2 | **Same commit, built from the archive**: binaries and DEB/RPM/Arch packages built only from the source archive; archive build = Git build; two independent runs identical | Implemented in CI (`release-dry-run` a/b, `release-dry-run-arch` a/b, `release-repro`) |
| R3 | **Notices and licence metadata**: `LICENSE.md`, `NOTICE.md`, `THIRD-PARTY-NOTICES.md` in every package and as assets; DEP-5 `copyright`; REUSE-compliant tree | Implemented and checked in CI |
| R4 | **SPDX SBOM** published with the release, **stamped with the release version** | SBOM implemented; version stamping **open** |
| R5 | **SHA-256 checksums** (`SHA256SUMS`) for every asset | Implemented in the dry run |
| R6 | **Exact toolchain pins** in the release workflow: Go patch version (CI uses `1.26.x`), Node.js and npm versions; checked against `RELEASE-BUILDINFO` | **Open** (recorded, not pinned) |
| R7 | **Pinned build/check tools**: `govulncheck` (CI installs `@latest`) | **Open** (spdx-tools 0.8.3, reuse 6.2.0, Playwright 1.63.0 are pinned) |
| R8 | **Release workflow**: triggered by a version tag or a manual run; read-only permissions except the job that creates the **draft prerelease** (`contents: write`); never auto-publishes | **Open** (not written) |
| R9 | **Unsigned labelling**: prerelease flag; "UNSIGNED DEVELOPMENT BUILD" in title and notes; verification instructions; no claim of signatures | **Open** (template to write with R8) |
| R10 | **Manual owner approval**: owner publishes the draft by hand; protected `release` environment used where available | **Open** (process defined; environment optional) |
| R11 | **Verification of the first draft**: owner (or CI) rebuilds from the published source archive and matches `SHA256SUMS`; inventory checks pass on the published packages | **Open** |

### 5.2 Strongly recommended, not a Phase 1 blocker
| # | Item | State |
|---|---|---|
| S1 | Move GitHub Actions off the deprecated Node 20 runtime (GitHub warnings on every run) | Open; separate dependency PR (ADR 0006 review) |

### 5.3 Deferred enhancements (not required for Phase 1)
| # | Item | Phase |
|---|---|---|
| D1 | Sigstore build-provenance attestations and verification instructions | Phase 2 (optional) |
| D2 | OpenPGP signature over `SHA256SUMS` (`SHA256SUMS.asc`) | Phase 3 (deferred) |
| D3 | Hardware token, offline primary key, backups, rotation, revocation, published fingerprint | Phase 3 (deferred) |
| D4 | Signed tags and tag-signature verification in the workflow | Phase 3 (deferred) |
| D5 | Package-repository signing (APT, RPM, Arch) | Phase 3 (deferred; only with package repositories) |

## Action items
1. [x] Owner accepted the release design (2026-10-16).
2. [x] Owner deferred hardware tokens, OpenPGP keys and mandatory signing; phased approach adopted
   (2026-10-19).
3. [ ] PR: Phase 1 release workflow (R4, R6–R9), draft prereleases only.
4. [ ] Owner: create the protected `release` environment if the plan offers it (R10, optional).
5. [ ] First draft prerelease verified (R11); then LA-13 can be closed for Phase 1.
6. [ ] PR: GitHub Actions runtime upgrade (S1).
7. [ ] Future owner decision on Phase 2 and Phase 3.
