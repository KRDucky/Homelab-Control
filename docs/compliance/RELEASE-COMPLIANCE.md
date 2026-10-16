# Release compliance

**Date:** 2026-10-16 (updated after the owner decisions of the same day) · **Branch:** `compliance-audit` ·
Companion to `LICENSE-AUDIT.md` (findings LA-xx), `LICENSE-EXCEPTIONS.md` (EX-xx) and
`RELEASE-PIPELINE.md`. Engineering record, not legal advice.

**Release readiness: NOT READY.** One blocker remains: **LA-13** (publishing and signing are designed but
not approved or enabled, ADR 0009). LA-02 and LA-03 are decided and implemented. Passing CI or automated
scans does not by itself establish compliance.

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
| G11 | Complete corresponding source; binaries rebuild from it byte-for-byte | `scripts/release-bundle.sh`, CI `release-dry-run` | **PASS** (dry run) |
| G12 | Source published with each binary; checksums; signatures; provenance | Publishing workflow (ADR 0009) | **BLOCKED** — LA-13, not approved/enabled |
| G13 | Supported Go toolchain; `govulncheck` clean | CI (Go 1.26.x) | **PASS** (automated); exact pins needed before enabling releases |
| G14 | Contribution policy in force | `CONTRIBUTING.md` (DCO) | **REVIEW REQUIRED** — LA-14 proposed |
| G15 | Manual review of new/changed dependencies since last release | Owner/maintainer | Not started — no release yet |

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

## 4. Remaining work before a first release
1. **Owner:** approve ADR 0009 (or amend), create the OpenPGP key and hardware token, create the protected
   `release` environment. Then a separate PR adds the tag-triggered draft-release workflow, pins exact
   Go/Node/`govulncheck` versions and builds the Arch package from the source archive.
2. **Owner:** approve `CONTRIBUTING.md` (DCO) and enable private vulnerability reporting.
3. Separate PR: remove the bundled Archivo font (LA-06 plan, `LICENSE-AUDIT.md` §5.3).
4. Before any Debian/Ubuntu archive submission: a Debian source package with `debian/changelog` and a
   repacked upstream tarball (`compat/webmin/` excluded), plus man pages.
