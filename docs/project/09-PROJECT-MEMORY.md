# Project Memory — Verified State and Decisions

> This is a project-local working memory file for Claude Code, **not** a claim that ChatGPT or Claude has stored memory externally. Update only after verifying changes. Keep it short and factual.

## Current state
- Status: **M0 complete, awaiting owner review.** Branch `m0-foundation`; nothing pushed by Claude.
- Product name: Homelab Control. Repo: `KRDucky/Homelab-Control---Webmin-Fork`.
- Upstream baseline: Webmin **2.670** (`3a2b2b8671c0669c129f5275b3dd1853106ae02b`) at `compat/webmin/` via `git subtree --squash`; tree verified identical; no fork patches.
- Implemented: unprivileged Go `hc-api` + `hlctl` (stdlib only), local accounts, sessions/CSRF/RBAC, read-only host overview, React/TS shell, Makefile/CI, Debian dev package, disposable-env scripts.
- Verified (2026-10-15, disposable Ubuntu 24.04 container): reproducible build; Go 44/44, Vitest 8/8; live API 15/15; Playwright 9/9; Webmin 2.670 login 9/9; upstream `prove` 4161 tests pass; `.deb` install/run/remove/purge.
- Not verified: any VM (no hypervisor), systemd start of the unit, govulncheck, Fedora/openSUSE/Arch packages.

## Decisions (M0)
- Privilege boundary: unprivileged API + future typed root broker; no process execution in API (ADR 0001, guard-tested).
- Sessions: own accounts, PBKDF2-SHA256 600k, `__Host-` SameSite=Strict cookie, Origin + CSRF token (ADR 0002).
- No generic Webmin bridge/RPC; coexistence + per-op adapters (ADR 0003).
- Secrets owned by broker in root-only files; SQLite deferred to M2 (ADR 0004).
- Go stdlib-only; npm exact pins with `--ignore-scripts` (ADR 0006). Subtree fork maintenance (ADR 0007).
- Rejected: reverse-proxying Webmin CGIs; JSON→foreign_call bridge; running new API as root; JWT sessions.

## Decisions requiring confirmation
- ADR 0005 licensing: GPL-3.0-or-later core (existing LICENSE.md), MIT for SDK/schemas/tokens, BSD-3 for upstream fixes.
- Approve ADR 0001/0003 before broker work.
- Cockpit track shown in UI-CONCEPT.png: confirm out of scope.
- Bundle a pruned `compat/webmin` in packages, or rely on upstream Webmin packages.
- PAM group → scope mapping for M1.
- Long-term multi-host architecture and marketplace trust policy (unchanged).

## Milestone log
| Date | Milestone | Verified results | Open blockers |
|---|---|---|---|
| 2026-10-15 | Planning | Specifications drafted; no build claimed | M0 not started |
| 2026-10-15 | M0 | See "Current state"; evidence in `docs/evidence/` | Owner approvals above; VM run on owner hardware |

## How to update
Append dated verified decisions and test results. Record alternatives rejected and why. Never replace unknowns with guesses or present planned work as shipped.
