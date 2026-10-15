# Initial Claude Code Project Prompt

You are implementing **Homelab Control**, a homelab-first Linux administration platform forked from Webmin.

Read `CLAUDE.md` and every referenced project specification in full. Treat them as the source of truth, and identify contradictions before writing code.

## Assignment: Milestone M0 only — fork, audit and development foundation
1. Inspect the current official Webmin source, license, build system, module/theme interfaces, authentication/authorization architecture, package integration and security advisories. Use actual upstream source, not assumptions.
2. Recommend a pinned upstream baseline and document how to sync security fixes while preserving fork-specific changes.
3. Establish a fork-friendly repository layout with separate legacy compatibility, new API, frontend, SDK, plugin, theme, packaging and test workspaces.
4. Produce architecture decision records for the privilege boundary, API/session model, legacy module bridge, secrets storage, and release/license strategy.
5. Create a minimal modern UI shell and a read-only host overview through an authenticated endpoint, **only if** the audit establishes a safe integration path.
6. Add automated formatting/linting, unit tests, basic security checks and a reproducible local build.
7. Configure a disposable VM development environment and test the baseline startup and login flow. Never run destructive commands against production disks.
8. Document current capabilities, missing dependencies, build/run steps, packaging plans, and safe rollback to upstream Webmin.

## Acceptance
- Source fork retains license/attribution and upstream provenance.
- Working local development instructions and test evidence.
- Clear and reviewed privilege architecture; no arbitrary root execution API.
- An honest inventory of reused Webmin modules and those requiring replacement.
- A prioritized M1 implementation plan with risks and dependencies.

## Reporting
Provide changed files, commit hashes if available, test commands/results, screenshots if built, known limitations, security risks, and decisions needing approval. **Stop after M0**; do not attempt the entire roadmap or enable destructive storage management.
