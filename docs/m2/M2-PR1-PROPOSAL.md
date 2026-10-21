# First M2 implementation PR — proposal

**Title:** "M2-1: durable state, tamper-evident audit, session management and service logs"
**Status:** proposal; needs owner authorization plus decisions D-1, D-6 and D-7 (`OWNER-DECISIONS.md`).
**Contains no privileged write operation.** The host is not modified by this PR.

## 1. Objective
- Close blockers **B2** (durability) and **B4** (first argument-taking broker op, read-only).
- Give users something new:
  - logs for any service;
  - sessions that survive restarts and can be revoked;
  - an audit page.

These are the foundations PR M2-2 needs for the first write.

## 2. Scope (exact)
### Backend
1. **State store (D-1 option A):** new package `internal/state`.
   - Opens `/var/lib/homelab-control/state.db` (configurable).
   - Refuses symlinks and group/world-accessible files or directories.
   - SQLite in WAL mode with `busy_timeout`.
   - A `schema_migrations` table with forward-only, transactional migrations, and an automatic file backup before migrating.
   - Migration 001 creates: `sessions` (SHA-256 IDs only), `login_throttle`, `audit_events` (API events, with sequence and `prev_hash`), `settings`.
2. **Sessions (D-6):** the in-memory store is replaced by the state store.
   - Same limits: 30 min idle, 12 h absolute, 1,024 cap.
   - Fixation defence and per-request account re-check are unchanged.
   - New: list and revoke the caller's **own** sessions.
3. **API audit:** events move from JSONL to `audit_events`.
   - Hash-chained; fsync via the SQLite synchronous setting `FULL`.
   - `hlctl audit export` (JSONL) and `hlctl audit verify`.
   - Existing event types are unchanged, and secrets are still never written.
4. **Broker audit hardening** (stdlib, root-owned file):
   - per-record `seq` and `prev_hash`;
   - `fsync` before the response is sent;
   - `hlctl audit verify --broker` checks the chain.
5. **New broker op `services.unit.logs`.** This is the first argument-taking op, and it is read-only.
   - **Args:** `{unit: string, lines: int}`, strictly decoded.
   - **`unit` grammar:** `^[A-Za-z0-9:_.@\\-]{1,255}\.(service|socket|timer|mount|automount|path|target|swap)$` (the unit types the broker already lists), not starting with `-`.
   - **`lines`:** 1–500.
   - **Existence:** the unit must appear in the broker's own unit list (no caller paths).
   - **Execution:** `journalctl --unit=<unit> --lines=<n> --output=json --no-pager --quiet` through `runner`.
     - The child runs as `homelab-control` with the supplementary group of the journal (`systemd-journal`, resolved at start). If that group is absent the op reports `unavailable`.
     - `journalctl` is added to the runner allow-list (root-owned binary check as today).
     - The runner gains per-op supplementary groups. It is still unprivileged; there is no root child.
   - **Result:** time, priority, message (each ≤ 4 KiB), PID; total ≤ 1 MiB; truncation flagged.
   - **Scope:** `services.logs`, re-checked in the broker.
6. **Routes** (all through `Server.handle`, documented in OpenAPI):

   | Route | Scope |
   |---|---|
   | `GET /api/v1/services/units/{unit}/logs?lines=N` | `services.logs` |
   | `GET /api/v1/audit?before=&limit=` | `audit.read` |
   | `GET /api/v1/auth/sessions` | session only (own) |
   | `POST /api/v1/auth/sessions/{id}/revoke` | session only (own; CSRF/Origin) |

7. **Scopes (D-7):** add `services.logs` and `audit.read`. The `hlctl user add` defaults stay `system.read,services.read,hardware.read` (logs and audit must be granted explicitly).

### Frontend (existing app, no rewrite)
- **Services page:** unit detail panel with a logs view (lines selector, refresh, truncation notice), shown only with `services.logs`.
- **New pages:** Audit (with `audit.read`), and Account → Sessions (list, revoke).
- **Shared components:** `PermissionGate` (hide or explain missing scope) and a basic table/pagination pattern, using design-token CSS variables only.

### Packaging
- **State directory:** `/var/lib/homelab-control` created 0700 `homelab-control` via `tmpfiles.d` and `StateDirectory=` in `homelab-control.service`. This is compatible with `ProtectSystem=strict`, and it also works when the install tests start `hc-api` without systemd.
- **Compliance generators:** `REUSE.toml`, SBOM, third-party notices and DEP-5 regenerated for the new Go modules.

## 3. Prerequisites
- Owner authorization of this proposal.
- **D-1 (option A):**
  - ADR 0006 amendment recording the SQLite driver review: licence, version pin, transitive module list, govulncheck;
  - ADR 0004 moves to Accepted for the state store.
- **D-6, D-7.**

## 4. Tests (required to merge)
| Area | Tests |
|---|---|
| State store | Permission and symlink refusal (F1); migration success, injected failure, backup created, refusal to start on failure (F2); WAL and concurrent readers |
| Sessions | Persistence across restart, timeouts, revocation, fixation, cap (F3); existing auth tests unchanged and passing |
| Audit | Chain verification passes; edited, truncated or reordered records detected (F4); no secrets (existing test extended) |
| `services.unit.logs` | Grammar table incl. injection strings (`-x`, `--output=export`, `;`, `$()`, `\n`, `\x00`, 300 chars); unknown unit; bounds; output caps; scope re-check (F5, F6); **fuzz of the args decoder ≥ 60 s in CI** |
| Runner | Supplementary group set only for ops that declare it; child uid/gid still `homelab-control`; guard tests unchanged (F7) |
| API | Authz 401/403 for every new route; CSRF/Origin on the revoke route; OpenAPI contract |
| UI | Vitest: logs panel, permission gates, sessions list, audit pagination; Playwright: logs, revoke, audit; axe 0 violations |
| Real systemd | CI `systemd-ubuntu` and `systemd-ubuntu-2604`: logs of a known unit returned; unknown unit refused; sessions survive `systemctl restart homelab-control` |
| Packaging | Install matrix unchanged (9 distros). **New upgrade test:** install the published `v0.1.0-dev.1` `.deb`, upgrade to the PR build, verify the state dir, DB, migration and login |
| Release | Release dry run, reproducibility and SBOM checks stay green with the new module(s) |

## 5. Acceptance criteria
1. All tests above pass in CI.
2. Owner VM (Ubuntu 26.04): logs, sessions after restart, revoke, audit verify — evidence in `docs/evidence/m2/pr1/`.
3. `systemd-analyze security` unchanged for both units.
4. No new write capability on the host: the broker registry diff shows only the new read op.

## 6. User-visible result
- Click a service and see its recent logs.
- Stay logged in across restarts and revoke your other sessions.
- See an audit trail of logins, denials and broker calls.

## 7. Documentation
- ADR 0006 amendment and ADR 0004 acceptance.
- ADR 0008 amendment for the first argument-taking op.
- Threat model rows: log disclosure, state DB.
- `STATUS.md`, `DEVELOPMENT.md` (state DB, `hlctl audit`), OpenAPI and project memory.

## 8. Explicit exclusions
- Any privileged write, service start/stop/restart, the plan/apply framework, `root-exec`, step-up.
- PAM, TOTP, admin management of other users' sessions.
- Broker-audit viewer in the UI (verification via `hlctl` only).
- Resource graph, storage, module registry, theme engine, Webmin migration.

## 9. Size and split option
Estimated medium-large: about 2–3k lines including tests. If the owner prefers smaller reviews, split it:
- **PR-1a:** state store, sessions and audit;
- **PR-1b:** `services.unit.logs` and the logs UI.

PR-1b does not depend on the database and could go first if D-1 needs more time.
