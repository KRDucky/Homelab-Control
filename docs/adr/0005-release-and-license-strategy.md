# ADR-0005: Release and license strategy

**Status:** **Accepted** (2026-10-16, owner) for items 1–4 and 6, after the licence audit (`docs/compliance/LICENSE-AUDIT.md`). Item 5 (release mechanics and signing) is refined in ADR 0009 (**accepted** 2026-10-16); no release is published until its prerequisites (§5 there) are met.
> **Owner decisions recorded 2026-10-16:** LA-02 core is GPL-3.0-or-later; LA-03 MIT for the public SDK, schemas, theme definitions and reusable design tokens, with exact boundaries in `REUSE.toml`; third-party code is never relicensed. Implemented in PR #5 (explicit grant in `NOTICE.md`/`README.md`, SPDX headers, `LICENSES/`, `REUSE.toml`, package metadata, SBOM).
**Date:** 2026-10-15
**Deciders:** Project owner (Ian)

## Context
- The repository was created with a GPL-3.0 `LICENSE.md`; `CLAUDE.md` says the final license is chosen
  after auditing upstream and dependencies.
- Upstream Webmin's top-level license is BSD-3-Clause, but the tree also contains GPL-2.0-or-later
  (dhcpd, samba/smbhash.pl), GPL (openslp, Faenza icons in filemin) and LGPL (QRCode) files
  (audit §2). Any distribution of the full `compat/webmin` tree must already honour the GPL for those
  files.
- Shipped UI dependencies are MIT (React) and OFL-1.1 (Archivo font); the Go backend is stdlib-only
  (BSD-3). Dev tooling includes MPL-2.0/Apache-2.0/MIT/ISC packages that are not shipped
  (`docs/evidence/license-inventory.txt`).
- The plugin/theme ecosystem needs third parties to publish manifests, schemas and themes under licenses
  of their choosing.

## Decision (recommended)
1. **Homelab Control core** (backend, frontend, packaging, scripts): **GPL-3.0-or-later**, keeping the
   existing `LICENSE.md`. This is compatible with every license found in the upstream tree and in the
   shipped dependencies, and makes the combined release a straightforward GPL-3.0 work.
2. **`compat/webmin/`**: keeps upstream licensing per file (BSD-3 overall, retaining `LICENCE`); our
   fixes to upstream files are contributed under **BSD-3** so they can go back upstream.
3. **SDK, JSON Schemas, OpenAPI, design tokens and example themes/plugins** (`sdk/`, `schemas/`,
   `themes/`, `frontend/design-system/`): **MIT**, so plugin and theme authors are not forced into the
   GPL by using our interfaces. Each of those directories will carry its own `LICENSE` once approved.
4. Add SPDX headers to new source files and a REUSE-style `LICENSES/` directory in M1.
5. **Releases:** semantic versions `vMAJOR.MINOR.PATCH`; tagged source tarball + checksums + SBOM
   (SPDX JSON) + `NOTICE.md`; signed tags and artifacts once a signing key is created. The release
   pipeline builds only from the tagged Git commit in CI (the 2018 upstream build-server backdoor shows
   why local build directories are not trusted). Upstream Webmin version is recorded in each release's
   notes.
6. **Naming:** "Homelab Control" is the product name; "Webmin" is used only to state provenance
   (BSD-3 clause 3: no endorsement implied).

## Options Considered
| Option | Pros | Cons |
|---|---|---|
| A. GPL-3.0-or-later core + MIT SDK (recommended) | Matches existing repo license; compatible with upstream GPL files; ecosystem-friendly | Fixes to new code can't flow upstream to BSD Webmin without relicensing by authors |
| B. BSD-3 everywhere (match upstream headline) | Max upstream compatibility | Inaccurate for the combined distribution (GPL files inside); would require excluding those modules |
| C. Apache-2.0 core | Patent grant | Incompatible with GPL-2.0-only code if any appears; still must ship GPL parts |
| D. AGPL-3.0 core | Network copyleft | Discourages integrators; little benefit for self-hosted single-node tool |

## Consequences
- Plugins that only use the MIT SDK and talk to the API over HTTP/IPC can be any license; plugins that
  link GPL core code are GPL.
- Packaging installs `LICENSE.md`, `NOTICE.md` (grant + MIT text) and `THIRD-PARTY-NOTICES.md`; the `.deb`
  also installs a DEP-5 `copyright`. `compat/webmin/` is not shipped, so its `LICENCE` is not either
  (enforced by `scripts/package-inventory.sh`).

## Action Items
1. [x] **Owner approval** of items 1–3 (2026-10-16).
2. [x] `LICENSE` (MIT) in `sdk/`, `schemas/`, `themes/`, `frontend/design-system/`; boundaries in `REUSE.toml`
   (also the theme schema and example themes under `docs/ui/`).
3. [x] SPDX headers on source files, `LICENSES/`, `reuse lint` in the security gate.
4. [x] SBOM generation and checks in CI; release dry run (`scripts/release-bundle.sh`). Publishing: ADR 0009.
