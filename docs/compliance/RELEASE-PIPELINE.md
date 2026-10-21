# Release pipeline (Phase 1 implemented; no release published yet)

**Status:** phased design **accepted and revised** by the owner (ADR 0009, 2026-10-16; revised 2026-10-19).
**Phase 1** is the current target: **unsigned** GitHub development/prereleases, created as drafts and
published only by the owner. Phase 2 (keyless attestations) is optional, and Phase 3 (OpenPGP and hardware-token
signing) is deferred. **The Phase 1 workflow is implemented** (2026-10-20, owner-authorized):
`.github/workflows/release.yml` (manual, draft only) and the reusable `.github/workflows/release-build.yml`,
which also runs as the CI dry run (§2). **No release or draft has been created yet.** LA-13 stays open
until a first draft has been produced, published by the owner and verified (ADR 0009 §5.1, R10–R11).
Owner runbook: `docs/release/RELEASING.md`.

## 1. What a Phase 1 prerelease publishes (GitHub Releases, marked prerelease, unsigned)
| File | Content |
|---|---|
| `homelab-control-<v>-source.tar.gz` | Complete corresponding source: repository at the tag (minus `compat/webmin/`, which no binary uses), `vendor-sources/npm/` (published sources of everything compiled into the UI bundle, integrity-checked), `RELEASE-BUILDINFO`, `BUILDING.md` |
| `homelab-control-<v>-linux-<arch>.tar.gz` | `hc-api`, `hlctl`, `hc-broker` + `LICENSE.md`, `NOTICE.md`, `THIRD-PARTY-NOTICES.md` |
| `homelab-control_<v>_<arch>.deb`, `homelab-control-<v>-….rpm`, `homelab-control-<v>-….pkg.tar.zst` | Packages (each carries the licence and notice files; the `.deb` also carries the DEP-5 `copyright`) |
| `LICENSE.md`, `NOTICE.md`, `THIRD-PARTY-NOTICES.md` | Licence texts and notices, also as separate assets |
| `homelab-control-<v>.spdx.json` | SPDX 2.3 SBOM |
| `RELEASE-BUILDINFO` | Commit, `SOURCE_DATE_EPOCH`, exact Go/Node/npm versions, build commands |
| `SHA256SUMS` | SHA-256 checksums of every file above |
| Release notes | Begin with **"UNSIGNED DEVELOPMENT BUILD"**; state that there is no cryptographic signature, that checksums on the same page detect corruption but not tampering by whoever controls the release, and how to verify by rebuilding from the source archive (`BUILDING.md`) |

Not in Phase 1: `SHA256SUMS.asc` (OpenPGP, Phase 3) and Sigstore attestations (Phase 2). Both can be
added later without changing the files above.

Binaries are published **only together with** the source archive of the same commit, in the same release.
For as long as binaries are offered, the matching source stays in the same release (GPL-3.0 §6(d)).

## 2. Dry run (implemented, runs in CI on every PR)
`scripts/release-bundle.sh <version> <out>` inside the reusable `release-build.yml`, called by CI as
`release-dry-run` with version `0.0.0-dryrun.<commit>` and the exact release toolchain
(`packaging/release/toolchain.env`). Jobs `build` (runs **a** and **b** on two independent runners),
`arch` (a, b) and `verify`:
1. Writes the source archive from the committed tree and fetches the npm sources, verifying each
   against the lockfile's sha512 integrity.
2. Extracts the archive into an empty directory and builds binaries, `.deb` and `.rpm` **only from it**.
3. Builds the same commit from a Git worktree; binaries **and** `.deb`/`.rpm` must be byte-identical.
4. Checks package payloads (`scripts/package-inventory.sh`) and writes `SHA256SUMS`.
5. `release-dry-run-arch`: in a fresh Arch container, extracts the same source archive, takes the binaries
   built from it and runs `packaging/arch/build-pkg.sh` from the archive.
6. `verify`: compares every file of run a with run b (source archive, binaries tarball, `.deb`,
   `.rpm`, Arch package, SBOM, notices, `RELEASE-BUILDINFO`); assembles the candidate; runs
   `scripts/release-tool.py verify` (every mandatory deliverable, fail closed), the negative tests and
   the version-ordering tests; renders the release notes.
The result is uploaded as 7-day CI artifacts, not as a release.

### Reproducibility: what is and is not demonstrated
| Claim | Evidence | Status |
|---|---|---|
| Binaries built from the source archive are byte-identical to binaries built from Git at the same commit | `release-bundle.sh` step 3, every CI run and locally | **Demonstrated** (same machine, same toolchain) |
| `.deb` and `.rpm` from the archive tree = from the Git tree | step 3b, CI and locally | **Demonstrated** (same machine) |
| All release files identical across two complete runs | Locally: two runs with separate Go build caches, 9/9 files identical incl. `.rpm` (2026-10-16). CI: `release-repro`, see PR #5 | **Demonstrated** on GitHub's `ubuntu-24.04` runners and one local container — not on other distributions or toolchain versions |
| Arch package reproducible | `release-repro` compares the two Arch builds | Demonstrated only if `release-repro` is green; same container image and package set required (`.BUILDINFO` lists installed packages) |
| Reproducible with a *different* Go or Node patch version, or on another distribution | — | **Not demonstrated and not expected**: Go embeds its version; use the toolchain in `RELEASE-BUILDINFO` |
| Independent third-party rebuild on their own hardware | — | **Not demonstrated** (the first-draft verification, ADR 0009 R11, is the planned first instance) |

Fixed for this: RPM file mtimes and build host were nondeterministic (two builds differed); now
`clamp_mtime_to_source_date_epoch` and a fixed `_buildhost` make them identical. The Arch build used a
random temporary directory that `makepkg` records in `.BUILDINFO`; it now uses a fixed path.

## 3. Phase 1 release workflow (implemented 2026-10-20)
Implemented as `.github/workflows/release.yml`; details, failure handling and the owner procedure are in
`docs/release/RELEASING.md`. It differs from the 2026-10-19 sketch below in one deliberate choice,
documented there and open to owner change: the trigger is **only a manual run** with the version as
input (`X.Y.Z-dev.N` / `X.Y.Z-rc.N`), not a pushed tag, so no tag exists before the owner publishes the
draft; GitHub creates the tag on publication. 2026-10-19 sketch (kept for reference):
```yaml
on:
  push: { tags: ['v*.*.*-*'] }        # prerelease-style tags only
  workflow_dispatch:
permissions: { contents: read }
jobs:
  build:                              # same steps as release-dry-run (+ Arch job), exact toolchain pins
    steps: [checkout tag, pin exact Go/Node/npm, scripts/release-bundle.sh "${TAG#v}" dist/release,
            stamp SBOM with the version, upload artifact]
  draft:
    needs: build
    environment: release              # protected (owner = required reviewer) where the plan offers it
    permissions: { contents: write }  # only this job can write
    steps: [download artifact, gh release create --draft --prerelease
            --title "UNSIGNED DEVELOPMENT BUILD <version>" --notes-file <template> dist/release/*]
```
Nothing is published automatically: the job creates a **draft prerelease**. The owner reviews it and
publishes it by hand. If no protected environment is available, the draft → publish step is still
the approval gate. Before the first public prerelease, the owner (or a CI job) rebuilds from the
draft's source archive and matches `SHA256SUMS` (ADR 0009 R11). Requirements: ADR 0009 §5.1 (R1–R11).

## 4. Phases
1. **Phase 1, unsigned development/prereleases (current target).** Reproducible builds, the
   corresponding-source archive, notices, licence metadata, the SPDX SBOM and SHA-256 checksums are all
   mandatory. Releases are clearly labelled unsigned. Publication is manual, by the owner. No keys or
   signing secrets exist.
2. **Phase 2, keyless attestations (optional enhancement).** GitHub artifact attestations (Sigstore
   build provenance through OIDC, recorded in a public transparency log) for each release file. They
   show which workflow run on which commit produced each file. Needs `id-token: write` and
   `attestations: write` in the build job only. Verification:
   `gh attestation verify <file> --repo KRDucky/Homelab-Control---Webmin-Fork`. No hardware key or
   manually generated secret is involved.
3. **Phase 3, maintainer signing (deferred to a future owner decision).** An OpenPGP signature over
   `SHA256SUMS`, signed tags, and package-repository signing (APT `InRelease`, `rpmsign`, Arch `.sig`).
   No private keys are created or requested until the owner decides to start this phase. Stable releases
   and package repositories should wait for that decision.

## 5. Decisions
- 2026-10-16: GitHub Releases accepted; `compat/webmin/` excluded from the source archive.
- 2026-10-19: hardware tokens, OpenPGP keys and mandatory signing **deferred**; phased approach
  (Phase 1 unsigned prereleases, Phase 2 optional attestations, Phase 3 deferred signing).

- 2026-10-20: owner authorized implementation of the Phase 1 workflow (draft only, no publication).

Remaining owner actions for Phase 1: review and merge the release-workflow PR; choose the first version
number and run the workflow; optionally protect the `release` environment; publish the draft by hand
and verify it (R11). No key material is needed.

## 6. Reference only: maintainer key management (Phase 3, deferred)
Kept so that a future Phase 3 decision starts from the reviewed 2026-10-16 design. **None of this is a
current requirement, and no key has been or will be created until the owner decides.**
- Ed25519 OpenPGP primary key generated and kept offline; signing subkey on a hardware token with
  touch-to-sign; expiry ≤ 2 years.
- No release signing key in GitHub secrets or CI.
- Encrypted offline backups in two physical locations; revocation certificate stored separately.
- Fingerprint published in `docs/security/RELEASE-KEYS.md`, the README and release notes.
- Rotation before expiry; on compromise, publish the revocation, re-sign current releases and document
  in `SECURITY.md`.
