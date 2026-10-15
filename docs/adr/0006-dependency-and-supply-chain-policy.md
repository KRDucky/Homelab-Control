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
