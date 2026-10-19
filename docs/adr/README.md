# Architecture decision records

| ADR | Title | Status |
|---|---|---|
| [0001](0001-privilege-boundary.md) | Privilege boundary: unprivileged API, typed root broker | **Accepted** 2026-10-16 (direction); not authorization for write ops |
| [0002](0002-api-session-model.md) | API authentication, session and CSRF model | Accepted for M0 |
| [0003](0003-legacy-module-bridge.md) | Legacy Webmin module bridge and migration | **Accepted** 2026-10-16 (direction); not authorization for write ops |
| [0004](0004-secrets-storage.md) | Configuration, state database and secrets storage | Proposed |
| [0005](0005-release-and-license-strategy.md) | Release and license strategy | **Accepted** 2026-10-16 (GPL-3.0-or-later core, MIT SDK/schemas/themes/tokens); release mechanics in 0009 |
| [0006](0006-dependency-and-supply-chain-policy.md) | Dependency and supply-chain policy | Accepted for M0 |
| [0007](0007-webmin-fork-maintenance.md) | Webmin fork maintenance and upstream sync | Accepted for M0 |
| [0008](0008-broker-design.md) | Privileged broker design: socket activation, peer auth, closed op registry | **Accepted** 2026-10-16 for the 4 existing read-only M1 ops only |
| [0009](0009-release-pipeline-and-signing.md) | Release pipeline, corresponding source and signing | **Accepted** 2026-10-16, **revised** 2026-10-19: Phase 1 unsigned prereleases; attestations optional; signing deferred. No workflow yet |
| [0010](0010-theme-document-format.md) | Theme document format (`.hltheme` single JSON file) | **Accepted in principle** 2026-10-16; implementation not authorized |

Spec 02 also lists ADRs that belong to later milestones and are **not written yet**: plugin isolation
(M3), package-source trust (M4), storage resource identifiers (M5), UI theming architecture (M3).
