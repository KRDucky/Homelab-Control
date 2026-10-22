# ADR-0006: Dependency and supply-chain policy

**Status:** Accepted for M0
**Date:** 2026-10-15
**Deciders:** Project owner

## Context
Upstream Webmin shipped a backdoor inserted on a compromised build server (CVE-2019-15231). The spec
requires provenance, dependency/license scans and signed artifacts, and forbids curl-pipe-shell and
silent third-party repositories.

## Decision
- **Go:** standard library only in M0 (`go.mod` has no `require`). Adding a module requires: a stated
  need, license check, maintenance/advisory review, pinned version in `go.sum`, and `govulncheck` in CI.
  `scripts/security-check.sh` fails if `go.mod` gains requirements without this ADR being updated.
- **npm:** exact versions in `package.json`, committed `package-lock.json`, installs with
  `npm ci --ignore-scripts` (no install-time scripts). Production dependencies are kept minimal
  (React, React DOM, font). `npm audit --omit=dev --audit-level=high` gates CI.
- **Reproducible builds:** `-trimpath -buildvcs=false -buildid=`, `CGO_ENABLED=0`, `SOURCE_DATE_EPOCH`
  from the commit; `scripts/repro-check.sh` builds twice from clean worktrees and compares hashes.
- **Upstream Webmin:** imported only from tagged releases via `git subtree --squash`; tree hash pinned in
  `compat/webmin.lock` and verified by `scripts/verify-upstream.sh`.
- **CI:** GitHub Actions pinned by full commit SHA once the workflow stabilises (M1), minimal
  `permissions:`; releases built only in CI from tags.
- **Host packages:** the product never adds repositories or runs vendor install scripts without explicit
  consent showing origin and key fingerprint (spec 06/07).

## Consequences
Slower to adopt libraries; much smaller audit surface. Revisit when the broker needs D-Bus or SQLite.

## Amendment 1 (2026-10-21): reviewed exception for the SQLite state store

**Decision (owner, D-1 option A):** `hc-api` and `hlctl` may use `modernc.org/sqlite`, a pure-Go SQLite driver. This is the only exception to "standard library only". `CGO_ENABLED=0` and reproducible builds stay as they are.

**Review recorded on 2026-10-21:**

| Item | Result |
|---|---|
| Need | ADR 0004 assigns non-secret state (sessions, audit, later jobs) to SQLite in M2. Option C (hand-written journals) would be done twice; option B (cgo) breaks the static, reproducible build. |
| Version | `modernc.org/sqlite v1.60.1` (tagged 2026-09-29), which bundles SQLite 3.53.4. It needs Go ≥ 1.26.0, so `go.mod` moves to `go 1.26.0`. CI and releases already use Go 1.26.10. |
| Module graph | 10 modules, listed with exact versions, licences and `go.sum` hashes in `backend/go-deps-allowlist.json`. 8 are linked into `hc-api` and `hlctl`: sqlite, libc, mathutil, memory, x/sys, google/uuid, go-humanize, bigfft. `go-isatty` and `go-strftime` are in the module graph only. |
| Licences | BSD-3-Clause (modernc, x/sys, uuid, bigfft), MIT (go-humanize). Bundled notices cover SQLite (public domain), musl libc (MIT), Go-derived code (BSD-3-Clause), go-netdb and nixpkgs (MIT), and mmap-go (BSD-3-Clause). All are compatible with GPL-3.0-or-later, and there is no copyleft or field-of-use restriction. |
| Provenance | Every module was fetched through `proxy.golang.org` and checked against `sum.golang.org`; `go.sum` is committed. Release source archives carry the module zips under `vendor-sources/go/download`, and the archive build runs offline from them (`GOPROXY=file://…`). |
| Vulnerabilities | `govulncheck v1.9.0` on go1.26.10 reports "No vulnerabilities found" (2026-10-21). CI runs it on every change. |
| Privilege | **`hc-broker`, the root process, links no third-party module.** This is enforced by `scripts/check-go-deps.py`. Only the unprivileged `hc-api` and the CLI link the driver. |

**Enforcement:** `scripts/check-go-deps.py`, run in `make security` and CI, fails when any of the following happens:
- `go.mod` requires a module, or a version, that is not in the allow-list;
- a `go.sum` hash changes;
- the set of linked modules changes;
- `hc-broker` links any third-party module.

`scripts/gen-sbom.py` and `scripts/gen-third-party-notices.py` list each module and its licence and notice files. `scripts/release-tool.py verify` fails if a module's source is missing from the release source archive.

**Changing the list:** adding or upgrading a module repeats this review and updates the allow-list in the same PR. Dependabot-style automatic bumps are not accepted.
