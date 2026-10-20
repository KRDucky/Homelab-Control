# Release compliance

**Date:** 2026-10-16; revised 2026-10-19 (signing deferred, ADR 0009) ·
Companion to `LICENSE-AUDIT.md` (findings LA-xx), `LICENSE-EXCEPTIONS.md` (EX-xx) and
`RELEASE-PIPELINE.md`. Engineering record, not legal advice.

**Release readiness: NOT READY.** One blocker remains: **LA-13**. The target is now **Phase 1, unsigned
development/prereleases** (ADR 0009, revised 2026-10-19). The Phase 1 workflow is implemented
(2026-10-20; R4, R6 done, R8/R9 implemented); R10 and R11 need the first draft, which has not been
created. Signing (OpenPGP, hardware tokens) and attestations are **deferred enhancements, not blockers**. LA-02 and LA-03 are decided and implemented. Passing CI or
automated scans does not by itself establish compliance.

## 1. Release gate checklist
| # | Gate | How it is checked | Status |
|---|---|---|---|
| G1 | Packages contain no Webmin, Perl/CGI or `compat/` files | `scripts/package-inventory.sh` (deny-list) in CI for deb, rpm, Arch | **PASS** (automated) |
| G2 | Packages contain only allow-listed paths | Same script (allow-list) | **PASS** (automated) |
| G3 | `LICENSE.md`, `NOTICE.md`, `THIRD-PARTY-NOTICES.md` shipped in every format | Same script | **PASS** (automated) |
| G4 | Third-party notices and DEP-5 `copyright` match shipped dependencies | `gen-third-party-notices.py --check` in the security gate | **PASS** (automated) |
| G5 | SBOM matches inputs and is valid SPDX 2.3 | `gen-sbom.py --check` + `pyspdxtools` in CI | **PASS** (automated) |
| G6 | Project licence grant is unambiguous | Explicit GPL-3.0-or-later grant in `NOTICE.md`/`README.md`; SPDX headers | **PASS** — LA-02 decided |
| G7 | Permissive licence applied exactly where intended | `REUSE.toml` boundaries, `LICENSE` files, DEP-5 stanza | **PASS** — LA-03 decided |
| G8 | Every file has licence and copyright information | `reuse lint` (REUSE 3.3) in the security gate | **PASS** (automated) |
| G9 | Package metadata lists every licence in the payload | DEB (DEP-5), RPM `License:`, Arch `license=()` reviewed | **PASS** |
| G10 | Debian DEP-5 `copyright` in the `.deb`; no lintian copyright/licence tags | `package-inventory.sh` + lintian in CI | **PASS** (automated) — LA-12 |
| G11 | Complete corresponding source; binaries **and** DEB/RPM/Arch packages rebuild from it byte-for-byte | `scripts/release-bundle.sh` (archive vs Git), CI `release-dry-run` a/b, `release-dry-run-arch` a/b, `release-repro` | **PASS** on GitHub `ubuntu-24.04` runners and Arch containers with the recorded toolchain; not tested for other toolchain versions or distributions (`RELEASE-PIPELINE.md` §2) |
| G12 | Source published with each binary; SHA-256 checksums; SBOM; notices; unsigned labelling; manual owner publication | `release.yml` + `release-build.yml`; `scripts/release-tool.py verify` (fail closed) in every CI dry run | **Implemented, not yet exercised for a real release** — LA-13 open until the first draft is published and verified (R10, R11) |
| G12a | Signatures and provenance (OpenPGP, signed tags, Sigstore attestations) | Phase 2/3 (ADR 0009 D1–D5) | **DEFERRED** by owner decision 2026-10-19; not required for Phase 1 |
| G13 | Supported Go toolchain; `govulncheck` clean | CI (Go 1.26.x); release gate with the exact pins | **PASS** (automated); `govulncheck` pinned (v1.9.0, R7); release toolchain pinned exactly (R6: Go 1.26.9, Node.js 22.23.3, npm 10.9.9) |
| G14 | Contribution policy in force | `CONTRIBUTING.md` (DCO), approved 2026-10-16 | **PASS** (policy); CI sign-off check is a follow-up |
| G15 | Manual review of new/changed dependencies since last release | Owner/maintainer | Not started — no release yet |
| G16 | Upstream Webmin tree accurately labelled and never shipped | `gen-webmin-license-inventory.py --check`, `reuse lint`, `verify-upstream.sh`, payload checks; excluded from source archive | **PASS** (with documented coverage limits) |

## 2. Payload verification (what actually ships)
`scripts/package-inventory.sh <package> [inventory-file]` lists the payload with the native tool
(`dpkg-deb -c`, `rpm -qlvp`, `tar --zstd -tvf`) and fails on any Webmin/Perl/CGI/`compat` path, any path
outside the allow-list, a missing notice file, or (for `.deb`) a missing `copyright`. CI uploads each
inventory, and lintian's output for the `.deb`, with the build artifacts.

| Format | Where verified | Result |
|---|---|---|
| `.deb` | Locally (2026-10-16) and in CI | 13 files: `hc-api`, `hlctl`, `hc-broker`, 3 systemd units, sysusers file, `hc-api.env`, `LICENSE.md`, `NOTICE.md`, `README.md`, `THIRD-PARTY-NOTICES.md`, `copyright`. No Webmin. **PASS**. lintian: no copyright/licence tags; remaining tags concern the development package (no changelog, no man pages, static binaries, systemctl in maintainer scripts) |
| `.rpm` | CI, step "Release payload compliance (deb, rpm)" | **PASS** |
| Arch `.pkg.tar.zst` | CI, step "Release payload compliance (Arch)" | **PASS** |

The web UI (React, React DOM, scheduler, Archivo font subsets, MIT design tokens) is embedded in
`hc-api`; its contents were confirmed from the production source map (LICENSE-AUDIT §2 step 4).

## 3. Distributed vs development-only
| Component | Distributed in packages | Licence | Notice shipped |
|---|---|---|---|
| Homelab Control core (Go binaries, UI code, units, configs) | Yes | GPL-3.0-or-later | `LICENSE.md`, `NOTICE.md` |
| Homelab Control design tokens (compiled into the UI bundle) | Yes | MIT | MIT text in `NOTICE.md` |
| Go standard library (compiled into binaries) | Yes | BSD-3-Clause | `THIRD-PARTY-NOTICES.md` |
| react 19.3.1, react-dom 19.3.1, scheduler 0.28.0 | Yes (in UI bundle) | MIT | `THIRD-PARTY-NOTICES.md` |
| @fontsource-variable/archivo 5.3.0 (3 woff2 subsets) | Yes (in UI bundle; removal planned, LA-06) | OFL-1.1 | `THIRD-PARTY-NOTICES.md` |
| SDK, schemas, theme definitions | No (source only) | MIT | `LICENSE` in each directory |
| 243 dev-only npm packages (Vite, TypeScript, axe-core, …) | No | Mixed (see `DEPENDENCY-LICENSES.md`) | Not required |
| `compat/webmin/` (Webmin 2.670 subtree) | No; also excluded from the release source archive | BSD-3-Clause + per-file exceptions | Upstream notices kept in place |
| CI/test tools (Playwright, spdx-tools, reuse, lintian, shellcheck, Perl) | No | Mixed | Not required |

## 4. Remaining work before a first (unsigned) development prerelease
Mandatory (ADR 0009 §5.1):
1. **Release-workflow PR** (R4, R6, R8, R9): implemented 2026-10-20; awaiting owner review and merge.
2. **Owner:** choose the first version number and run the workflow (it only creates a draft); optionally
   protect the `release` environment; publish the draft by hand (R10). See `docs/release/RELEASING.md`.
3. **Verify the first published prerelease:** rebuild from its source archive and match `SHA256SUMS`; run
   inventory checks on the packages (R11). Only then is LA-13 closed for Phase 1.

Done: GitHub Actions moved off the deprecated Node 20 runtime (S1); `govulncheck` pinned (R7); SBOM
version stamping (R4); exact toolchain pins (R6).

Deferred (owner decision 2026-10-19), not required for Phase 1:
- Sigstore attestations (Phase 2);
- OpenPGP signatures, signed tags, hardware tokens, offline key management, package-repository signing
  (Phase 3).

Other follow-ups:
- CI DCO sign-off check;
- private vulnerability reporting and `SECURITY.md`;
- Archivo font removal (LA-06);
- Webmin migration phase 1.

Before any Debian/Ubuntu archive submission: a Debian source package with `debian/changelog`, man pages,
and a repacked upstream tarball without `compat/webmin/`.
