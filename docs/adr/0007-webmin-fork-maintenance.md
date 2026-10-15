# ADR-0007: Webmin fork maintenance and upstream sync

**Status:** Accepted for M0
**Date:** 2026-10-15
**Deciders:** Project owner

## Context
We need upstream provenance, a reproducible way to absorb upstream security fixes, and visibility of any
fork-specific changes, while keeping our own code in a normal monorepo.

## Decision
- Upstream lives at `compat/webmin/` as a **`git subtree --squash`** of a release tag. The squash commit
  records `git-subtree-split: <upstream commit>`; `compat/webmin.lock` records tag, commit and tree hash.
- Baseline: **2.670** (`3a2b2b8671c0669c129f5275b3dd1853106ae02b`). Move to 2.680 when tagged.
- Fork changes inside `compat/webmin/` are discouraged; when unavoidable they are separate commits listed
  in `compat/PATCHES.md`, and security fixes are reported upstream first (security@webmin.com).
- Sync = `scripts/sync-upstream.sh <tag>` on a branch → review the upstream diff for security-relevant
  changes → re-verify patches → run `make check` + baseline login test → merge. Full checklist:
  `docs/upstream/SYNC.md`.

## Options Considered
| Option | Pros | Cons |
|---|---|---|
| Subtree --squash of tags (chosen) | Small repo (37 MB .git); provenance in commit trailers; normal merges for patches | Upstream history not in our repo (one fetch away) |
| GitHub fork of webmin/webmin as the product repo | Full history | Mixes 25 years of upstream history with a new monorepo; awkward layout |
| Git submodule | Clean separation | Patches need a second fork repo; contributors trip over submodules |
| Vendored tarball | Simple | Weak provenance; manual diffing |

## Consequences
Upstream syncs are routine merges; divergence is visible in one table.
