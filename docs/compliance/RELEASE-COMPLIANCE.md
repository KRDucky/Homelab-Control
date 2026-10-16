# Release compliance

**Date:** 2026-10-16 · **Branch:** `compliance-audit` · Companion to `LICENSE-AUDIT.md` (findings LA-xx) and
`LICENSE-EXCEPTIONS.md` (EX-xx). Engineering record, not legal advice.

**Release readiness: NOT READY.** Blocked by LA-02 and LA-03 (owner decisions) and LA-13 (no release
process). Passing CI or automated scans does not by itself establish compliance.

## 1. Release gate checklist
| # | Gate | How it is checked | Status |
|---|---|---|---|
| G1 | Packages contain no Webmin, Perl/CGI or `compat/` files | `scripts/package-inventory.sh` (deny-list) in CI for deb, rpm, Arch | **PASS** (automated) |
| G2 | Packages contain only allow-listed paths | Same script (allow-list) | **PASS** (automated) |
| G3 | `LICENSE.md`, `NOTICE.md`, `THIRD-PARTY-NOTICES.md` shipped in every format | Same script | **PASS** (automated) |
| G4 | Third-party notices match shipped dependencies | `gen-third-party-notices.py --check` in `security-check.sh` | **PASS** (automated) |
| G5 | SBOM matches inputs and is valid SPDX 2.3 | `gen-sbom.py --check` + `pyspdxtools` in CI | **PASS** (automated) |
| G6 | Project license grant is unambiguous (`-only` vs `-or-later`) | Manual | **REVIEW REQUIRED** — LA-02 |
| G7 | Permissive licence applied where intended (SDK, schemas, themes, design tokens) | Manual | **REVIEW REQUIRED** — LA-03 |
| G8 | Package metadata lists every licence in the payload | Manual review of DEB/RPM/Arch metadata | **PASS** except GPL version (LA-02) |
| G9 | Corresponding source published with each binary; checksums; signatures | Release job | **BLOCKED** — LA-13 |
| G10 | Supported Go toolchain; `govulncheck` clean | CI (Go 1.26.x) | **PASS** (automated) |
| G11 | Debian DEP-5 `copyright` file | Manual | **REVIEW REQUIRED** — LA-12 (needed for distro archives only) |
| G12 | Manual review of new/changed dependencies since last release | Owner/maintainer | Not started — no release yet |

## 2. Payload verification (what actually ships)
`scripts/package-inventory.sh <package> [inventory-file]` lists the payload with the native tool
(`dpkg-deb -c`, `rpm -qlvp`, `tar --zstd -tvf`) and fails on any Webmin/Perl/CGI/`compat` path, any path
outside the allow-list, or a missing notice file. CI uploads each inventory with the build artifacts.

| Format | Where verified | Result |
|---|---|---|
| `.deb` | Locally, `homelab-control_0.0.0+git.15a3e6bc_amd64.deb`; and CI | 12 files: `hc-api`, `hlctl`, `hc-broker`, 3 systemd units, sysusers file, `hc-api.env`, `LICENSE.md`, `NOTICE.md`, `README.md`, `THIRD-PARTY-NOTICES.md`. No Webmin. **PASS** |
| `.rpm` | CI run 38876319969, step "Release payload compliance (deb, rpm)" | **PASS** |
| Arch `.pkg.tar.zst` | CI run 38876319969, step "Release payload compliance (Arch)" | **PASS** |

The web UI (React, React DOM, scheduler, Archivo font subsets) is embedded in `hc-api`; its contents
were confirmed from the production source map (LICENSE-AUDIT §2 step 4).

## 3. Distributed vs development-only
| Component | Distributed in packages | Licence | Notice shipped |
|---|---|---|---|
| Homelab Control code (Go binaries, UI bundle, units, configs) | Yes | GPL-3.0 (version per LA-02) | `LICENSE.md` |
| Go standard library (compiled into binaries) | Yes | BSD-3-Clause | `THIRD-PARTY-NOTICES.md` |
| react 19.3.1, react-dom 19.3.1, scheduler 0.28.0 | Yes (in UI bundle) | MIT | `THIRD-PARTY-NOTICES.md` |
| @fontsource-variable/archivo 5.3.0 (3 woff2 subsets) | Yes (in UI bundle) | OFL-1.1 | `THIRD-PARTY-NOTICES.md` |
| 243 dev-only npm packages (Vite, TypeScript, axe-core, …) | No | Mixed (see `DEPENDENCY-LICENSES.md`) | Not required |
| `compat/webmin/` (Webmin 2.670 subtree) | No (source repository only) | BSD-3-Clause + per-file exceptions | Upstream notices kept in place |
| CI/test tools (Playwright, spdx-tools, shellcheck, Perl) | No | Mixed | Not required |

## 4. Remediation order (proposed)
1. **Owner decides LA-02** (GPL-3.0-only or -or-later). Then: one grant statement in README/NOTICE, align
   package metadata. *Small, unblocks LA-04.*
2. **Owner decides LA-03** (MIT scope). Then: `LICENSE` files in `sdk/`, `schemas/`, `themes/`,
   `frontend/design-system/`; update NOTICE and SBOM.
3. **LA-04:** SPDX headers + `LICENSES/` (REUSE layout) with a CI check.
4. **LA-13:** release job — tagged source archive, `SHA256SUMS`, SBOM, notices, signed tag/artifacts.
   Needs owner decisions on signing key and hosting.
5. **LA-12:** DEP-5 `copyright` in the `.deb` (before any distro-archive submission).
6. **LA-06, LA-07, LA-14:** policy items — font choice, fixture provenance line, contribution policy.

Items 1–2 need only owner decisions; 3, 5 and the fixture line in 6 are small changes this branch can make
once approved; 4 is a new CI release pipeline and needs explicit approval.
