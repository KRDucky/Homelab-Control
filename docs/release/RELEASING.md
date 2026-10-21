# Releasing: Phase 1 unsigned development prereleases

**Applies to:** ADR 0009 Phase 1 (revised 2026-10-19). Engineering runbook for the project owner.
Releases are **unsigned GitHub prereleases**, prepared as **drafts** by the `release` workflow and
**published only by the owner, by hand**. No signing keys or secrets exist or are needed.
Sigstore attestations (Phase 2) and OpenPGP signing (Phase 3) are not part of this process.

## 1. What the workflow does
`.github/workflows/release.yml` runs only when started by hand (Actions → **release** → **Run
workflow**, branch **main**). It takes one input, the version.

| Job | Permissions | What it does | Fails when |
|---|---|---|---|
| `preflight` | `contents: read`, `actions: read` | Must run on `main`; validates the version; the tag and any published release for it must not exist; the `ci` workflow must have **succeeded on `main` for exactly this commit** | Not on `main`, invalid version, tag or release exists, no green `ci` run for the commit yet |
| `gate` | `contents: read` | `make check` with the exact release toolchain (lint, Go and frontend tests, release tooling tests, security gate incl. `govulncheck`, REUSE, SBOM, notices, Webmin inventory) | Any check fails, toolchain differs from the pins |
| `build` (`release-build.yml`) | `contents: read` | Two independent builds (a, b) with `scripts/release-bundle.sh`, the Arch package from the source archive in fresh Arch containers, byte-for-byte comparison of a and b, assembly of the candidate, **fail-closed verification** of every mandatory deliverable, negative tests, version-ordering tests, release notes | Any build, reproducibility, verification or test step fails |
| `draft` | `contents: write` (only job) | Re-checks the candidate; checks again (now able to see drafts) that **no release, draft or tag** exists for the version; creates the **draft prerelease** titled `[INCOMPLETE - do not publish] …`; downloads **every uploaded asset back** and checks it against `SHA256SUMS`; then retitles it `UNSIGNED DEVELOPMENT BUILD vX.Y.Z-…`. It stays a draft | Duplicate version, upload error, downloaded assets differ, the release is not a draft prerelease |

The workflow **never** publishes, creates or moves tags, deletes or overwrites releases or assets, or
uses any secret other than the job's `GITHUB_TOKEN`. Only one release run can be active at a time
(`concurrency: release`, never cancelled half-way). The same `release-build.yml` runs on every PR
and push as the `ci` job **release-dry-run** (version `0.0.0-dryrun.<commit>`), so the release path
is exercised continuously. `tests/release/test_workflow_policy.py` enforces these properties
(manual trigger only, write access in `draft` only, `--draft --prerelease`, no publish/delete/tag
commands, inputs only through environment variables, actions pinned to the `ci.yml` SHAs).

## 2. Versions and tags
- Version input: SemVer prerelease **`X.Y.Z-dev.N`** or **`X.Y.Z-rc.N`**, without `v` (ADR 0005 uses
  `vMAJOR.MINOR.PATCH`; `RELEASE-PIPELINE.md` §3 defines the `-dev.N` / `-rc.N` prerelease forms).
  Leading zeros, other suffixes and stable versions (`X.Y.Z`) are rejected: Phase 1 publishes
  prereleases only.
- Tag: `vX.Y.Z-dev.N`, created by GitHub **when the owner publishes** the draft, pointing at the
  commit the draft was built from (`target_commitish` = full SHA).
- Each version can be used once. The workflow refuses a version whose tag, release or draft exists.
- Package versions sort before the final release: `.deb` `0.1.0~dev.1`, `.rpm` `0.1.0-0.m1.0_1_0_dev_1`,
  Arch `0.1.0dev.1` (`packaging/common/version-map.sh`; checked with `dpkg`, `rpm` and `vercmp`).
- The `.deb` asset is named `homelab-control_X.Y.Z-dev.N_amd64.deb` (the package inside has the
  Debian version `X.Y.Z~dev.N`); a `~` in an asset name could be changed by GitHub.

*Not defined by an earlier owner decision, chosen here and open to change:* the version is a workflow
input rather than a pushed tag, so that no tag exists before the owner publishes; the build commit is
the `main` head at the moment the workflow is started.

## 3. Release files (all verified before the draft is created)
`homelab-control-<v>-source.tar.gz`, `homelab-control-<v>-linux-amd64.tar.gz`,
`homelab-control_<v>_amd64.deb`, one `.rpm`, one `.pkg.tar.zst`, `LICENSE.md`, `NOTICE.md`,
`THIRD-PARTY-NOTICES.md`, `homelab-control-<v>.spdx.json`, `RELEASE-BUILDINFO`, `SHA256SUMS`.

`scripts/release-tool.py verify` checks, and fails on any deviation:
- exactly this file set (nothing missing, nothing extra), asset-safe names, non-empty regular files;
- `SHA256SUMS` lists every other file once, and every hash matches;
- `RELEASE-BUILDINFO`: the version, the commit, and Go/Node.js/npm equal to `packaging/release/toolchain.env` (R6);
- SBOM: SPDX 2.3, stamped with the version, commit and exact Go version (R4), accepted by `pyspdxtools`;
- source archive: complete (npm sources, `RELEASE-BUILDINFO`, `BUILDING.md`, notices, lockfile,
  toolchain pins), **no `compat/webmin/`**, no `.git`, no paths or links outside the tree; its notices
  and `RELEASE-BUILDINFO` identical to the separate assets;
- binaries tarball: exactly the three binaries plus the three notice files;
- packages: `scripts/package-inventory.sh` (no Webmin/Perl/CGI paths, allow-listed paths only, notice
  files present, DEP-5 `copyright` in the `.deb`); `.deb` version as above.

`tests/release/negative-tests.sh` then mutates copies of the real candidate (16 cases: missing or
extra files, corrupted package, wrong version/commit/toolchain, unstamped or invalid SBOM, Webmin file or
missing npm sources in the source archive, extra file in the binaries tarball, wrong package type) and
requires each to fail verification.

## 4. Owner procedure
1. **Before the first run (optional):** in *Settings → Environments*, create or edit `release` and add
   yourself as a required reviewer. The draft job then waits for your approval. If you skip this,
   GitHub creates an unprotected `release` environment on the first run; the manual publish step
   below remains the approval gate (ADR 0009 R10).
2. Wait until the `ci` run on `main` for the commit you want to release is green.
3. Actions → **release** → **Run workflow** → branch `main` → version, e.g. `0.1.0-dev.1`.
4. When it succeeds, open the draft linked in the run summary (*Releases* shows drafts to writers).
   Check: title `UNSIGNED DEVELOPMENT BUILD v…`, *Set as a pre-release* ticked, the notes, the commit,
   the 11 files.
   Check that the draft's tag reads `vX.Y.Z-…`, not `untagged-…` (see §5), and that its target is the
   commit from the run's preflight notice.
5. **Publish by hand** (*Edit* → *Publish release*). This creates the tag. Keep *Set as a pre-release*.
6. **Verify the published release (R11)**: download all files; `sha256sum -c SHA256SUMS`; rebuild from
   the source archive per `BUILDING.md` with the toolchain in `RELEASE-BUILDINFO` and compare the
   binaries; run `scripts/package-inventory.sh` on the three packages. Record the result in
   `docs/STATUS.md`. Only then can LA-13 be closed for Phase 1.

## 5. Failures and recovery
Nothing is ever published by a failed run. What can be left behind, and what to do:

| Situation | Left behind | Recovery |
|---|---|---|
| Fails in `preflight`, `gate` or `build` | Nothing on GitHub (workflow artifacts only) | Fix the cause, run again with the same version |
| Version, tag, release or draft already exists | Nothing new | Choose the next version (`-dev.N+1`), or delete the old **draft** yourself if it is unwanted |
| The draft's tag reads `untagged-…` (first release only: the retitling step did not resend the tag name, fixed afterwards) | A complete draft with the wrong tag | When publishing: *Choose a tag* → type `vX.Y.Z-…` → *Create new tag on publish*; target = the commit from the preflight notice |
| `draft` fails or is interrupted after creating the draft (upload error, mismatch, cancelled, runner lost) | A **draft** titled `[INCOMPLETE - do not publish] …` | Do **not** publish it. Delete the draft by hand (*Releases* → draft → delete), then run again with the same version, or use a new version. The workflow never deletes it for you |
| Insufficient permissions (e.g. Actions token read-only by organisation policy) | Nothing, or an INCOMPLETE draft if it failed during upload | Allow `contents: write` for workflows in repository settings, delete any INCOMPLETE draft, run again |
| Environment approval rejected or timed out | Nothing (draft job did not run) | Run again |

Drafts never create tags, so a deleted draft leaves nothing behind.

## 6. What this does not provide
- No cryptographic signature or attestation of any file (ADR 0009: Phase 2 optional, Phase 3 deferred).
  `SHA256SUMS` detects corruption, not tampering by someone who controls the release.
- Reproducibility is demonstrated on GitHub `ubuntu-24.04` runners and fresh `archlinux:latest`
  containers with the pinned toolchain only (`RELEASE-PIPELINE.md` §2).
- No package repositories, no stable releases (both wait for the Phase 3 decision).
