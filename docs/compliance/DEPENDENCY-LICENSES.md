# Dependency licenses

**Snapshot:** 2026-10-16, `frontend/app/package-lock.json` (lockfileVersion 3) and `backend/go.mod`.
**Authoritative, per-version record:** the SPDX SBOM `docs/compliance/sbom/homelab-control.spdx.json`
(regenerate with `scripts/gen-sbom.py`; CI fails if it is stale). `scripts/license-report.sh` prints the
same split from the lockfile.

## 1. Redistributed (in binaries and packages)
| Component | Version | License | How it ships | Verified by |
|---|---|---|---|---|
| Go standard library | CI toolchain 1.26.x (`go.mod` minimum 1.24) | BSD-3-Clause | Statically linked into `hc-api`, `hlctl`, `hc-broker` | `go.mod` has no `require`; `go version -m` |
| react | 19.3.1 | MIT | JS bundle embedded in `hc-api` | Production source map |
| react-dom | 19.3.1 | MIT | JS bundle embedded in `hc-api` | Production source map |
| scheduler | 0.28.0 | MIT | JS bundle embedded in `hc-api` (react-dom dependency) | Production source map |
| @fontsource-variable/archivo | 5.3.0 | OFL-1.1 (no Reserved Font Name) | Three `.woff2` subsets + CSS embedded in `hc-api` | Production build output |

Nothing else reaches the bundle: the source map lists only these packages plus our own `src/` files, the
bundle contains no Vite runtime (module-preload polyfill disabled in `vite.config.ts`), and the Go module
graph is empty. Notices: `THIRD-PARTY-NOTICES.md`.

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
| Node.js 22 / npm | MIT / Artistic-2.0 | Frontend build and tests |
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
None. `backend/go.mod` declares no `require`; ADR 0006 keeps the backend standard-library-only, enforced by
`scripts/security-check.sh` ("Go dependency policy").
