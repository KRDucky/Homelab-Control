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
- Backend Go code must not spawn processes or register routes outside `Server.handle` — `backend/internal/guard` tests enforce this. Privileged work belongs in the future broker (ADR 0001).
- Go: standard library only unless ADR 0006 is updated. npm: exact versions, `npm ci --ignore-scripts`.
- Run `make check` before committing; run Go tests as a non-root user.

## Engineering conventions
- Go for new long-running services and typed API implementations where justified; Perl compatibility adapter for existing Webmin modules; React/TypeScript/Vite for UI; SQLite for local job/audit/config state where appropriate.
- Use OpenAPI and JSON Schema for public/internal contracts. Avoid tight coupling between UI, distro adapters and privileged operations.
- Use semantic versioning, migration scripts, reproducible builds and signed release artifacts when release infrastructure exists.
- Maintain `docs/project/09-PROJECT-MEMORY.md` as concise verified project state, not as speculative facts.

## Current milestone
M0 is complete pending owner review (see `docs/STATUS.md`). Next: `docs/M1-PLAN.md`, only after approval.
