# ADR-0016: Independent project; Webmin removed

**Status:** Accepted (owner, 2026-11-25)
**Date:** 2026-11-25
**Deciders:** Project owner
**Supersedes:** ADR 0003 (legacy Webmin module bridge), ADR 0007 (Webmin fork maintenance)

## Context
Homelab Control started from a Webmin source tree kept at `compat/webmin/` as a squashed subtree. Shipped
code never used it (`docs/compliance/WEBMIN-INDEPENDENCE.md`): packages contained no Webmin code, and the
tree served only maintenance checks, Webmin's own Perl test suite in CI, and real-Webmin tests of the
optional "Legacy Webmin" detection page. The owner stated on 2026-11-25 that Homelab Control is not a fork
of Webmin and that Webmin must be removed from the repository completely.

## Decision
- Homelab Control is an independent project. It is not a fork of Webmin and contains no Webmin code or
  assets.
- `compat/` (the Webmin subtree, `webmin.lock`, `PATCHES.md`) is removed, together with the scripts and CI
  that maintained it: `scripts/sync-upstream.sh`, `scripts/verify-upstream.sh`,
  `scripts/gen-webmin-license-inventory.py`, the `upstream-perl-tests` job, `docs/upstream/`, the Webmin
  licence inventory and the `LicenseRef-Webmin-*` licence texts.
- The Legacy Webmin feature is removed: the `legacy.webmin.status` broker op, `GET /api/v1/legacy/webmin`,
  the `legacy` capability, the overview's `legacy_webmin` section and the session's `legacy_url`. The
  `hc-api -legacy-url` flag is still accepted but ignored (with a log line), so an existing
  `/etc/default/hc-api` that sets it does not stop the service.
- The test harnesses no longer install Webmin. The package-payload check that rejects Webmin, Perl and CGI
  paths stays.
- The Git history is rewritten so that no commit contains `compat/` (owner, 2026-11-25). Commit hashes
  before the rewrite no longer exist; evidence records that cite them keep the old hash as text.

## Consequences
- Accepted M1 functionality (the Legacy Webmin page) is withdrawn by owner decision.
- No Webmin licence obligations remain in the repository or in any release artifact.
- Earlier release artifacts (for example `v0.1.0-dev.1`) are unchanged; their notices described the tree
  as it was then.
