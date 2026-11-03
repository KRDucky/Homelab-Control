# CLAUDE.md — Homelab Control

You are the lead architect and implementation engineer for Homelab Control, an open-source fork and modernization of Webmin for homelab Linux administration.

## Mandatory reading order
1. `docs/project/00-PROJECT-SETUP.md`
2. `docs/project/01-PROJECT-CONTEXT.md`
3. `docs/project/02-ARCHITECTURE.md`
4. `docs/project/03-SECURITY-AND-OPERATIONS.md`
5. `docs/project/04-ROADMAP.md`
6. `docs/project/05-FEATURE-CATALOG.md`
7. `docs/project/06-THEME-AND-PLUGIN-SDK.md`
8. `docs/project/07-DISTRO-PACKAGING.md`
9. `docs/project/08-TESTING-AND-ACCEPTANCE.md`
10. `docs/project/09-PROJECT-MEMORY.md`

Then: `docs/adr/README.md`, `docs/audit/SPEC-CONTRADICTIONS.md`, `docs/dev/DEVELOPMENT.md`.

## Non-negotiable rules
- Fork Webmin with its license, attribution, and upstream provenance preserved. Audit licenses of reused and new dependencies before choosing the final distribution license.
- Preserve useful upstream functionality during incremental migration. Do not rewrite the entire Perl backend first.
- Build a cohesive React/TypeScript UI and versioned API. Existing Webmin modules may be bridged temporarily; never expose unrestricted legacy root access through new APIs.
- No arbitrary shell execution or unaudited root-level plugin execution. Server-side RBAC/ACL, CSRF defenses, input validation, structured command arguments, safe path handling, audit logging and operation locking are mandatory.
- Every write action has preflight validation, a dry-run/plan when feasible, an explicit authorization step, and verifiable outcomes. Never claim transactional rollback for non-atomic disk operations.
- Never use volatile `/dev/sdX` as a persistent drive identity; prefer WWN, serial, by-id, UUID and topology corroboration.
- Never infer a physical drive bay as verified; record provenance/confidence of location mapping.
- No destructive testing on real host disks. Use disposable VMs, loop devices, virtual disks and fixtures.
- All modules detect capabilities and distro support, gracefully explain missing dependencies, and offer approved installation paths.
- Third-party repositories require explicit approval and key/provenance inspection. Never execute curl-pipe-shell installers.
- Themes must be declarative and non-executable by default. Plugins must declare permissions, dependencies, compatibility and lifecycle hooks.
- No fabricated data in production views. All examples and mockups must be labeled.
- Implement milestone-by-milestone with tests, documentation, and honest status reports. Do not continue to the next milestone without approval.

## Repository-specific rules (from M0)
- `compat/webmin/` is a pristine `git subtree --squash` of upstream; any change there must be listed in `compat/PATCHES.md`. Sync with `scripts/sync-upstream.sh`, verify with `scripts/verify-upstream.sh`.
- Only `backend/internal/broker/runner` may execute programs; `hc-api`/`hlctl` must not link broker internals; every API route goes through `Server.handle` and is documented in OpenAPI with its scope — `backend/internal/guard` tests enforce all of this. Privileged work happens only in broker ops (ADR 0001, ADR 0008); new ops need review and must stay read-only unless the owner has authorized that specific write and it follows the accepted write-operation framework (ADR 0011: broker-issued plan → confirm → execute, step-up, protected-unit policy, operation journal). So far only systemd service start/stop/restart (M2-2) is authorized; every further write keeps its own authorization gate and needs an ADR 0011 review.
- Go: standard library only unless ADR 0006 is updated. npm: exact versions, `npm ci --ignore-scripts`.
- Run `make check` before committing; run Go tests as a non-root user.

## Engineering conventions
- Go for new long-running services and typed API implementations where justified; Perl compatibility adapter for existing Webmin modules; React/TypeScript/Vite for UI; SQLite for local job/audit/config state where appropriate.
- Use OpenAPI and JSON Schema for public/internal contracts. Avoid tight coupling between UI, distro adapters and privileged operations.
- Use semantic versioning, migration scripts, reproducible builds and signed release artifacts when release infrastructure exists.
- Maintain `docs/project/09-PROJECT-MEMORY.md` as concise verified project state, not as speculative facts.

## Current milestone
M1 and the Phase 1 release are complete. M2 PR-1 (state, sessions, audit, read-only service logs) was authorized 2026-10-21. M2-2 (systemd service start, stop and restart: plan → confirm → execute, step-up, `root-exec`, protected-unit policy; ADR 0011) was authorized 2026-10-23 and merged 2026-10-26 (PR #28, merge commit `a3447efa`). M2-3 and later milestones, and every other privileged write (enable/disable, mask, unit-file edits, daemon-reload, packages, storage, containers, users and groups), are **not** authorized; each needs its own owner authorization and an ADR 0011 review. Acceptance items E2/E4 were closed by the owner on 2026-10-29 after combined-main Ubuntu, Fedora and Arch KVM validation (`d812995e`; CI 40666815912). Production `services.operate` remains ungranted by default and requires a separate deployment decision. Historical merge gaps and validation caveats remain recorded in `docs/m2/M2-2-ACCEPTANCE.md` and `docs/evidence/m2/vm-final-main-d812995e/README.md`.

# Append to root CLAUDE.md — File Services and Proxy Expansion

Read `docs/addons/01-FILE-SERVICES-ARCHITECTURE.md` through `docs/addons/09-PROJECT-MEMORY-ADDENDUM.md` before implementing shares, remote mounts, proxies, certificates, or identity features. The existing root `CLAUDE.md`, ADRs, `docs/STATUS.md`, and `docs/M1-PLAN.md` retain precedence on current milestone and privilege boundaries.

**Rules:** Implement typed adapters to native OS services; never embed a second network server into `hc-api`. Discovery/read-only inventory before writes. Every privileged change requires a narrow broker operation, RBAC, target revalidation, backup, dry-run plan, explicit confirmation, service-native validation, audit and recovery guidance. Never use a generic command runner. Never rewrite unmanaged configs. Never auto-open firewall ports, expose admin services publicly, or enable anonymous/guest access by default. Secrets never appear in URLs, logs, process argv, UI exports or Git. Mounts must be bounded, cancellable, and not hang host overview. Support Ubuntu, Debian, Fedora, openSUSE, Arch via capability detection and distribution-native packages; never silently add repos or run curl|sh. Upstream source repos are for study/attribution, not wholesale vendoring. Respect licenses and keep `compat/webmin/` pristine. Use disposable VMs, loopback block devices, and isolated networks for integration tests; never test destructive actions on real disks. Update ADRs and project memory only with verified facts. Do not skip M1 approval or claim features exist because specs were added.

## UI design system (HCIS)
HCIS (Homelab Control Interface System, “Adaptive Glass Linux Admin”) is the owner-approved design direction for all UI work. Before building or changing any UI, read `docs/ui/hcis/README.md`, then `docs/ui/hcis/BRAND-BOOK.md` and the relevant `docs/ui/hcis/components/<Name>.md`. Use the semantic tokens in `docs/ui/hcis/tokens/`; treat `docs/ui/hcis/components/reference/` as reference code to port, not to import. Planned/Proposed components are designs, not shipped features. Adopting HCIS in `frontend/app` happens in separately approved PRs; the open decisions (fonts vs LA-06, density names in docs/ui/06, token-schema additions) are listed in `docs/ui/hcis/README.md`.

## UI continuation authorization (2026-10-25)

The owner authorized completing the remaining frontend (HCIS shared components, navigation palette, browser-local theme import/export and Theme Studio) so Claude Opus can focus on backend/module work, and requested a user Theme Development Kit. Current implementation/handoff: `docs/ui/11-IMPLEMENTED-UI-AND-TDK.md`. TDK: `sdk/theme/README.md`. Runtime schema: `schemas/theme/v1.json` (the older draft is historical). This scope adds no privileged host operation, API permission or server settings store. Future backend milestones keep their own existing authorization gates.
