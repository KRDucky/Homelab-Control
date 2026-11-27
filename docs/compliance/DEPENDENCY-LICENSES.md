# Dependency licenses

**Snapshot:** `frontend/app/package-lock.json` through 2026-10-23; Go modules and allowlist through 2026-11-12.
**Authoritative, per-version record:** the SPDX SBOM `docs/compliance/sbom/homelab-control.spdx.json`
(regenerate with `scripts/gen-sbom.py`; CI fails if it is stale). `scripts/license-report.sh` prints the
same split from the lockfile.

## 1. Redistributed (in binaries and packages)
| Component | Version | License | How it ships | Verified by |
|---|---|---|---|---|
| Go standard library | CI and release toolchain 1.27.4 (`go.mod` minimum 1.26.0) | BSD-3-Clause | Statically linked into `hc-api`, `hlctl`, `hc-broker` | `backend/go.mod`; `go version -m` |
| modernc.org/sqlite | 1.60.1 | BSD-3-Clause | Statically linked into `hc-api`, `hlctl` | Go dependency allowlist; `go version -m` |
| modernc.org/libc | 1.77.1 | BSD-3-Clause | Statically linked into `hc-api`, `hlctl` | Go dependency allowlist; `go version -m` |
| modernc.org/mathutil | 1.7.1 | BSD-3-Clause | Statically linked into `hc-api`, `hlctl` | Go dependency allowlist; `go version -m` |
| modernc.org/memory | 1.12.1 | BSD-3-Clause | Statically linked into `hc-api`, `hlctl` | Go dependency allowlist; `go version -m` |
| golang.org/x/sys | 0.48.0 | BSD-3-Clause | Statically linked into `hc-api`, `hlctl` | Go dependency allowlist; `go version -m` |
| github.com/google/uuid | 1.6.0 | BSD-3-Clause | Statically linked into `hc-api`, `hlctl` | Go dependency allowlist; `go version -m` |
| github.com/dustin/go-humanize | 1.0.1 | MIT | Statically linked into `hc-api`, `hlctl` | Go dependency allowlist; `go version -m` |
| github.com/remyoudompheng/bigfft | v0.0.0-20230129092748-24d4a6f8daec | BSD-3-Clause | Statically linked into `hc-api`, `hlctl` | Go dependency allowlist; `go version -m` |
| github.com/coreos/go-oidc/v3 | 3.21.0 | Apache-2.0 | Statically linked into `hc-api`, `hlctl` | Go dependency allowlist; `go version -m` |
| github.com/go-jose/go-jose/v4 | 4.1.7 | Apache-2.0 | Statically linked into `hc-api`, `hlctl` | Go dependency allowlist; `go version -m` |
| golang.org/x/oauth2 | 0.37.0 | BSD-3-Clause | Statically linked into `hc-api`, `hlctl` | Go dependency allowlist; `go version -m` |
| react | 19.3.1 | MIT | JS bundle embedded in `hc-api` | Production source map |
| react-dom | 19.3.1 | MIT | JS bundle embedded in `hc-api` | Production source map |
| scheduler | 0.28.0 | MIT | JS bundle embedded in `hc-api` (react-dom dependency) | Production source map |
| @fontsource-variable/inter | 5.3.0 (Inter 4.001) | OFL-1.1 (no Reserved Font Name) | Seven variable `.woff2` subsets + CSS embedded in `hc-api` | Production build output |
| @fontsource-variable/jetbrains-mono | 5.3.0 (JetBrains Mono 2.211) | OFL-1.1 (no Reserved Font Name) | Six variable `.woff2` subsets + CSS embedded in `hc-api` | Production build output |
| Rust crates of `hc-audit-retention` (101 linked; ADR 0006 amendment 4, proposed) | exact versions and checksums in `backend/audit-retention/rust-deps-allowlist.json` | 84 MIT (or MIT among alternatives, MIT used); 15 Unicode-3.0 (ICU: `icu_*`, `zerovec`, `yoke`, …); `foldhash` Zlib; `ryu` Apache-2.0 OR BSL-1.0 (Apache-2.0 used); bundled SQLite public domain | Statically linked into `hc-audit-retention` | `scripts/check-rust-deps.py` (lockfile, licences, `cargo tree`) |

Nothing else reaches the UI bundle: the source map lists only these frontend packages plus our own `src/`
files, and the bundle contains no Vite runtime (module-preload polyfill disabled in `vite.config.ts`). Go
module graph contents and whether each module is linked into a binary are checked against
`backend/go-deps-allowlist.json`. Required notices: `THIRD-PARTY-NOTICES.md`.

## 2. Development and test only (npm, not redistributed)
243 unique packages (245 lockfile entries; `lru-cache@11.5.3` appears three times).

| License | Count | Packages (non-MIT, named) |
|---|---|---|
| MIT | 184 | — |
| Apache-2.0 | 18 | typescript, @eslint/* (5), @humanfs/* (3), @humanwhocodes/* (2), aria-query, baseline-browser-mapping, detect-libc, eslint-visitor-keys (2 versions), expect-type, xml-name-validator |
| MPL-2.0 | 13 | axe-core 4.14.0 (accessibility tests), lightningcss 1.33.0 + 11 platform binaries (CSS processing in Vite) |
| ISC | 11 | electron-to-chromium, flatted, glob-parent, isexe, lru-cache 5.1.1, picocolors, saxes, semver (2), which, yallist |
| BSD-2-Clause | 8 | entities, eslint-scope, espree, esrecurse, estraverse, esutils, uri-js, webidl-conversions |
| BSD-3-Clause | 3 | esquery, source-map-js, tough-cookie |
| BlueOak-1.0.0 | 2 | lru-cache 11.5.3, minimatch |
| MIT-0 | 2 | @csstools/color-helpers, @csstools/css-syntax-patches-for-csstree |
| CC-BY-4.0 | 1 | caniuse-lite (browser-support data used by build tooling) |
| CC0-1.0 | 1 | mdn-data |

Direct dev dependencies: @eslint/js, @testing-library/dom, @testing-library/react, @types/react,
@types/react-dom, @vitejs/plugin-react, axe-core, eslint, eslint-plugin-react-hooks, globals, jsdom,
prettier, typescript, typescript-eslint, vite, vitest. All are installed with `npm ci --ignore-scripts`.

**Assessment:** no strong-copyleft or source-available package; every entry has a license field. MPL-2.0
(file-level copyleft) and CC-BY-4.0 (attribution) would only create obligations if those files were
redistributed; they are not. Output produced by build tools (lightningcss-processed CSS, TypeScript
output) is our own code, not a copy of the tool.

## 3. Build, CI and test tools (not redistributed)
| Tool | License | Use |
|---|---|---|
| Go toolchain | BSD-3-Clause | Compiles the backend |
| Node.js 26 / npm | MIT / Artistic-2.0 | Frontend build and tests |
| govulncheck v1.9.0 | BSD-3-Clause | Security gate (CI) |
| shellcheck 0.9 | GPL-3.0 | Shell lint (CI) |
| spdx-tools 0.8.3 | Apache-2.0 | SBOM validation (CI) |
| reuse 6.2.0 | GPL-3.0-or-later | REUSE licensing lint (CI security gate) |
| lintian (Ubuntu 24.04 archive) | GPL-2.0-or-later | `.deb` copyright/licence check (CI) |
| Playwright 1.63.0 (+ Chromium, Firefox, WebKit builds) | Apache-2.0 (Chromium: BSD-3-Clause and others; Firefox: MPL-2.0; WebKit: LGPL-2.1/BSD) | Browser e2e and theme CSSOM/CSP probe (CI, VM) |
| dpkg-deb, rpmbuild, makepkg | GPL-2.0-or-later | Package builders |
| debootstrap, libvirt/QEMU (owner workstation) | MIT; LGPL/GPL | Disposable test environments |

Tool licenses do not extend to the artifacts they produce.

## 4. Go modules

`backend/go.mod` has direct and indirect third-party modules. Exact versions, SPDX identifiers, checksums,
and link status are recorded in `backend/go-deps-allowlist.json`; `scripts/security-check.sh` rejects
unreviewed versions and verifies the linked set. Eleven allowlisted modules are linked into `hc-api` and
`hlctl` (none into `hc-broker`) and listed in §1. `github.com/mattn/go-isatty` and
`github.com/ncruces/go-strftime` are present in the module graph but are not linked into a binary. Full
license texts and inherited notices for distributed modules are in `THIRD-PARTY-NOTICES.md`; the
per-version SPDX record is in the generated SBOM.

## 5. Adding or reusing open-source code

Before adding a library, copying or adapting upstream code, or bundling an SDK/plugin dependency, review the
exact version and its transitive dependencies against the repository's `GPL-3.0-or-later` licensing and
distribution model. Record the license identifier and the compatibility rationale; an SPDX label alone is
not a compatibility review. Stop for owner review when the license is missing, ambiguous, has additional
conditions, or the obligations are unclear. Do not add code with incompatible terms.

For every accepted addition, regenerate and validate the version-specific SPDX SBOM, update this dependency
inventory and `THIRD-PARTY-NOTICES.md` with required copyright, license text, attribution and upstream
notices, and add/update REUSE metadata as applicable. Keep the records in these repository paths so they
are published with the GitHub source and release artifacts. The project license is in [`../../LICENSE.md`](../../LICENSE.md);
the current audit and exceptions are [`LICENSE-AUDIT.md`](LICENSE-AUDIT.md) and
[`LICENSE-EXCEPTIONS.md`](LICENSE-EXCEPTIONS.md). Run the repository's license/SBOM checks before merging.
