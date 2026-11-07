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
| [0008](0008-broker-design.md) | Privileged broker design: socket activation, peer auth, closed op registry | **Accepted** 2026-10-16 for the 4 read-only M1 ops; Amendment 1 (2026-10-21) `services.unit.logs`; Amendment 2 (2026-10-23, D-4) `root-exec` runner class for reviewed write ops; Amendment 3 (M2-3, **Proposed**) enablement templates; Amendment 4 (M2-6, **Proposed**) hostnamectl/timedatectl templates; Amendment 5 (M4-1b, **Proposed**) read-only package queries |
| [0009](0009-release-pipeline-and-signing.md) | Release pipeline, corresponding source and signing | **Accepted** 2026-10-16, **revised** 2026-10-19: Phase 1 unsigned prereleases; attestations optional; signing deferred. No workflow yet |
| [0010](0010-theme-document-format.md) | Theme document format (`.hltheme` single JSON file) | **Accepted in principle** 2026-10-16; browser-local implementation authorized 2026-10-25 |
| [0011](0011-write-operation-authorization.md) | Write-operation authorization: plan → confirm → execute, step-up, protected units | **Accepted** 2026-10-23 (D-2…D-5, option A); first use: service start/stop/restart (M2-2); M2-3 enablement extension **Proposed**; M2-6 hostname/timezone extension **Proposed** |
| [0012](0012-confined-root-file-listing.md) | Confined-root file listing in unprivileged hc-api (`os.Root`, `files.read`) | **Proposed** 2026-11-07 (file-manager first checkpoint; no broker op) |
| [0013](0013-plugin-isolation-declarative-contributions.md) | Plugin isolation and declarative contributions (`.hlplugin` single JSON file) | **Proposed** 2026-11-07 (M3 plugin SDK first checkpoint; no executable payload) |
| 0014 | Package transaction authorization | Reserved for a later package-transaction write ADR; not written |

Spec 02 also lists ADRs that belong to later milestones and are **not written yet**: package-source
trust (M4), storage resource identifiers (M5), UI theming architecture (M3).
Package-source and key trust is a distinct later ADR, not 0014, and is still not written.
