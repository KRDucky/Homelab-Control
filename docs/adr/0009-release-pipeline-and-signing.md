# ADR-0009: Release pipeline, corresponding source and signing

**Status:** **Accepted, revised 2026-10-19 (owner).** Releases are phased. **Phase 1** (unsigned GitHub
development/prereleases) is the current target. **Phase 2** (keyless attestations) and **Phase 3**
(maintainer signing) are optional and deferred. The revision supersedes the 2026-10-16 decision that
made hardware-backed OpenPGP signatures mandatory. The Phase 1 workflow (`.github/workflows/release.yml`,
implemented 2026-10-20, owner-authorized) only creates **drafts**; nothing is published automatically,
and no release has been made yet.
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
Full design: `docs/compliance/RELEASE-PIPELINE.md`. Owner runbook: `docs/release/RELEASING.md`.

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
**LA-13 is resolved for Phase 1 (2026-10-21).** The first unsigned development prerelease, v0.1.0-dev.1 (https://github.com/KRDucky/Homelab-Control---Webmin-Fork/releases/tag/v0.1.0-dev.1), tag → `ec34b21e`, release run 39562079197, was produced by the workflow, published by the owner and verified after publication against every mandatory item below. Phase 2 and Phase 3 items (§5.3) remain deferred and are not part of this closure.

### 5.1 Mandatory for a Phase 1 unsigned development prerelease
| # | Requirement | State (2026-10-20) |
|---|---|---|
| R1 | **Corresponding source** with every release: source archive of the tagged commit, npm sources, `RELEASE-BUILDINFO`, `BUILDING.md`; `compat/webmin/` excluded | Implemented in the dry run (`scripts/release-bundle.sh`) |
| R2 | **Same commit, built from the archive**: binaries and DEB/RPM/Arch packages built only from the source archive; archive build = Git build; two independent runs identical | Implemented in CI (`release-dry-run` a/b, `release-dry-run-arch` a/b, `release-repro`) |
| R3 | **Notices and licence metadata**: `LICENSE.md`, `NOTICE.md`, `THIRD-PARTY-NOTICES.md` in every package and as assets; DEP-5 `copyright`; REUSE-compliant tree | Implemented and checked in CI |
| R4 | **SPDX SBOM** published with the release, **stamped with the release version** | **Done**: `release-tool.py stamp-sbom` (version, commit, exact Go version, release-specific namespace; deterministic), called by `release-bundle.sh`; `verify` requires the stamp and `pyspdxtools` validity. Verified in the CI dry run (version `0.0.0-dryrun.<commit>`) |
| R5 | **SHA-256 checksums** (`SHA256SUMS`) for every asset | Implemented in the dry run |
| R6 | **Exact toolchain pins** in the release workflow: Go patch version, Node.js and npm versions; checked against `RELEASE-BUILDINFO` | **Done**: `packaging/release/toolchain.env` (Go 1.26.10 since 2026-10-21 for GO-2026-6809, previously 1.26.9; Node.js 22.23.3, npm 10.9.9; CI uses the same Go version) used by `release-build.yml` (release and CI dry run) and `release.yml`'s gate; installed versions checked before building and against `RELEASE-BUILDINFO` after. Verified in the CI dry run |
| R7 | **Pinned build/check tools**: `govulncheck` and the other CI tools | **Done in CI**: `govulncheck` v1.9.0 (was `@latest`), spdx-tools 0.8.3, reuse 6.2.0, Playwright 1.63.0, Actions pinned by SHA. The release workflow must reuse the same pins |
| R8 | **Release workflow**: triggered by a version tag or a manual run; read-only permissions except the job that creates the **draft prerelease** (`contents: write`); never auto-publishes | **Complete**: `release.yml` ran on 2026-10-21 (run 39562079197, all 8 jobs green): manual run on `main`, `contents: write` in the `draft` job only, draft prerelease created and its 11 uploaded files re-downloaded and checked against `SHA256SUMS`; nothing published, no tag created. One defect found: the retitling PATCH did not resend `tag_name`, so the draft's tag read `untagged-…`; fixed in the release-closeout PR (tag resent and checked) |
| R9 | **Unsigned labelling**: prerelease flag; "UNSIGNED DEVELOPMENT BUILD" in title and notes; verification instructions; no claim of signatures | **Complete**: the v0.1.0-dev.1 draft is marked prerelease, titled `UNSIGNED DEVELOPMENT BUILD v0.1.0-dev.1`, and its notes open with the UNSIGNED banner, the no-signature statement, provenance links and verification steps (checked 2026-10-21) |
| R10 | **Manual owner approval**: owner publishes the draft by hand; protected `release` environment used where available | **Complete**: the workflow left v0.1.0-dev.1 as a draft; the owner set the tag and published it by hand on 2026-10-21 (published 08:07:42Z). The `draft` job ran in environment `release`; whether it is protected was not verifiable from this session (optional per §Decision 5) |
| R11 | **Verification of the first draft**: owner (or CI) rebuilds from the published source archive and matches `SHA256SUMS`; inventory checks pass on the published packages | **Complete** (2026-10-21): all 11 files downloaded anonymously from the public release; `sha256sum -c SHA256SUMS` OK and identical to the pre-publication draft; `release-tool.py verify --require-tools` OK (SBOM, notices, build info, no Webmin, package inventories); tag `v0.1.0-dev.1` = `ec34b21e`. **Independent rebuild outside GitHub** from the published source archive (Go 1.26.10 built from the go1.26.10 source tag, Node.js 22.23.3, npm 10.9.9, Ubuntu 24.04 container): `hc-api`, `hc-broker`, `hlctl` and the `.deb` byte-identical; the `.rpm` identical when the build host has no `homelab-control` group (see `BUILDING.md`); Arch package not rebuilt outside CI (two-container CI comparison only) |

### 5.2 Strongly recommended, not a Phase 1 blocker
| # | Item | State |
|---|---|---|
| S1 | Move GitHub Actions off the deprecated Node 20 runtime | **Done**: official `actions/*` node24 releases, SHA-pinned (CI modernization PR) |

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
3. [x] PR: Phase 1 release workflow (R4, R6–R9), draft prereleases only (owner-authorized 2026-10-20).
4. [~] Owner: protect the `release` environment if the plan offers it (R10, optional; not verified).
5. [x] First prerelease v0.1.0-dev.1 published by the owner and verified (R11); LA-13 closed for Phase 1 (2026-10-21).
6. [x] PR: GitHub Actions runtime upgrade (S1) and `govulncheck` pin (R7).
7. [ ] Future owner decision on Phase 2 and Phase 3.
