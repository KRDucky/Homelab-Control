# M2 readiness assessment

**Date:** 2026-10-21 · **Baseline:** `main` at `31f39778` (PR #10), CI run 39570975685 20/20 ·
**Status:** planning document. **M2 implementation is not authorized**; no privileged write exists.

Labels used below: **[Verified]** = checked in the repository on this date; **[Approved]** = owner
decision already recorded; **[Documented]** = written in a spec/ADR but not implemented; **[Blocker]** =
must be resolved before the first privileged write; **[Recommendation]** = new, needs owner approval
(see `OWNER-DECISIONS.md`).

## 1. Verdict
- **Ready to start M2 now:** the first implementation PR (`M2-PR1-PROPOSAL.md`). It adds durable state, a
  hardened audit trail and a read-only service-log viewer, and contains **no privileged write**. It
  depends on decisions D-1 (persistence engine) and D-6 (persistent sessions).
- **Not ready yet: the first privileged write** (service start/stop/restart, PR M2-2). Four security
  blockers (§5) must be designed, approved and implemented first. The design is proposed in
  `M2-SECURITY-ACCEPTANCE.md` and decisions D-2…D-5.
- Release infrastructure, licensing and CI place no constraints on M2: LA-13 is closed for Phase 1,
  and signing stays deferred.

## 2. Current implementation baseline [Verified]
| Area | What exists | Where |
|---|---|---|
| API | Unprivileged `hc-api` (Go stdlib only). 9 routes, all registered through `Server.handle` with a declared scope (guard test). The only non-GET routes are login and logout | `backend/internal/api/server.go` |
| Auth | Local accounts (`users.json` 0600, PBKDF2 600k), in-memory sessions (SHA-256 IDs, 30 min idle / 12 h absolute), Origin + per-session CSRF token, throttling | `internal/auth` (ADR 0002) |
| Scopes | `system.read`, `services.read`, `hardware.read`; unknown scopes rejected at load | `internal/auth/users.go` |
| Broker | Root `hc-broker`, socket-activated, `SO_PEERCRED`, strict framing, closed registry of **4 read-only, argument-free ops** (`broker.info`, `legacy.webmin.status`, `hardware.dmi.identity`, `services.systemd.list`) | `internal/broker/*` (ADR 0008) |
| Program execution | Only `internal/broker/runner` (guard test). Allow-list: `systemctl` only. Fixed argv, argument charset check, scrubbed env; **children always run as `homelab-control`, never root** | `runner.go` |
| Audit | `hc-api` JSONL (auth/authz events) and broker JSONL (peer, actor, op, outcome); 0600; no hash chain, no fsync policy, no query API | `internal/audit` |
| Persistence | `users.json` and two audit logs only. **No database**, no job/operation state, sessions lost on restart | ADR 0004 "Proposed" |
| Hardware/storage read | Native sysfs inventory: PCI, USB, block devices with ranked identity (`wwn` > `serial` > `by-id`, kernel names labelled volatile), sensors; DMI serials via broker | `internal/hwinfo`, `hostinfo` |
| UI | React 19 + Vite, own minimal History-API router, pages Overview / Services / Hardware / Legacy, capability-aware navigation, `useData` fetch hook, shared `Section`/`Sources`/`StateDot` components; strict CSP; **no write controls, no dialogs, no forms besides login** | `frontend/app/src` |
| Tests | 98 Go test functions (`go test`, non-root), 15 Vitest tests, Python/Playwright e2e (3 browsers + CSP probe), install matrix on 9 distros, real systemd on Ubuntu 24.04 and 26.04, upstream Perl tests, release dry run; owner KVM VM harness (`dev/vm/`, `hc-test-` namespace, verified on Ubuntu 26.04) | CI `ci.yml` (20 jobs) |
| Webmin | `compat/webmin/` pristine reference subtree; never executed or proxied by the new stack | ADR 0003 |

## 3. Approved architecture (not to be re-decided) [Approved]
- **ADR 0001:** unprivileged API plus typed root broker; no generic exec/file/RPC operation, ever.
- **ADR 0002:** own accounts, server-side sessions, Origin + CSRF token, scope per route.
- **ADR 0003:** coexistence with Webmin, with no proxying and no generic bridge. Per-operation adapters, native Go first.
- **ADR 0004** is still **Proposed**: SQLite (WAL) for non-secret state, with the driver choice deferred to M2. Secrets live in root-owned broker files.
- **ADR 0005:** GPL-3.0-or-later core; MIT for the SDK, schemas, themes and tokens.
- **ADR 0006:** stdlib-only Go; any module needs this ADR updated.
- **ADR 0008:** accepted only for the 4 existing ops. Every new op needs review.
- **ADR 0009:** phased releases.
- **ADR 0010:** `.hltheme`.
- **ADR 0001/0003/0008 owner notes (2026-10-16):** acceptance is **not** authorization for privileged writes. Writes need a separate approval with confirmation, locking, audit and rollback design.
- **2026-10-21 product decisions (this review):**
  - modules are independently useful;
  - coordinated cross-module workflows are optional conveniences;
  - the listed technology scope;
  - the Physical Drive Layout Designer.
  These are design authorizations, not implementation.

## 4. Documented M2 scope [Documented]
What the repository actually assigns to M2. Nothing beyond this is attributed to existing specs.

| Source | M2 item |
|---|---|
| `docs/project/04-ROADMAP.md` | "M2 — Core API, RBAC, resource graph, jobs/audit"; gate: "Versioned schemas, security tests, operation state persistence" |
| ADR 0001 action item 4 | Locking/journaled operation state and write-op confirmation |
| ADR 0002 action item 3; "Revisit" | Persisted sessions and per-session revocation UI; WebAuthn/TOTP to be revisited in M2 |
| ADR 0003 action item 3 | Parity-test harness against `compat/webmin` parsers |
| ADR 0004 action item 3 | SQLite driver decision and schema migrations |
| ADR 0008 "Revisit in M2" | Write-op confirmation tokens, operation locks, persistent job state; whether D-Bus replaces `systemctl` parsing |
| `docs/audit/SPEC-CONTRADICTIONS.md` #12 | Full, versioned scope catalogue |
| `docs/STATUS.md` | Four security items that block M2 (§5) |
| `docs/addons/07-API-SCHEMAS-AND-UX.md` | Plan/job API sketch (`POST /plans` → preview; `POST /jobs` with plan ID + confirmation token; revision checks, idempotency key, lock domain, durable audit) |

**Not in the documented M2 scope:**
- service start/stop and user/group or host administration (feature catalogue items without a milestone);
- storage management (M5–M9);
- the theme/plugin SDK (M3);
- the software center (M4).

The owner's candidate sequence (M2.1–M2.5) is compared with this in `M2-ROADMAP.md` §1.

## 5. Outstanding blockers before the first privileged write [Blocker]
| # | Blocker (from `docs/STATUS.md`) | Why | Proposed resolution |
|---|---|---|---|
| B1 | **Broker trust:** a compromised `hc-api` can call every registered op with any claimed scope | Today safe only because ops are read-only and argument-free | Write ops must be narrow, target-revalidated, policy-limited (protected-unit list), single-use plan tokens minted by the broker, broker-side locks and rate limits, and an audit the API cannot alter. Honest limit: this bounds the damage of an API compromise, it does not prevent it (D-2) |
| B2 | **Audit durability:** JSONL plus in-memory sessions and throttle | Writes need a durable, ordered record that survives restarts | Broker audit with sequence numbers, hash chain and `fsync` before an op returns; operation journal (intent → result) so interrupted ops are reported after restart; API state in SQLite (D-1) |
| B3 | **Step-up authentication:** no re-authentication before a privileged action; no MFA | A stolen or idle session should not be able to stop services | Password re-entry within a short window for disruptive ops (D-3); TOTP later |
| B4 | **Argument-taking broker ops:** none exist | First op with caller input needs a reviewed validation design | Strict typed args, per-field regex and length limits, existence revalidation in the broker, `--` separation in argv, fuzz tests. Exercised first by a **read-only** op in PR-1 (`services.unit.logs`) |

New gap found in this review:

| # | Gap | Why | Proposed resolution |
|---|---|---|---|
| B5 | **Root child processes:** the runner only runs children as `homelab-control` | `systemctl start/stop/restart` needs root (or polkit) | New runner privilege class `root-exec`: fixed argv templates, allow-listed binaries, root child, same caps and timeouts (D-4) |

## 6. Security risks for M2 (summary; details and tests in `M2-SECURITY-ACCEPTANCE.md`)
| Risk | Level | Mitigation |
|---|---|---|
| Lockout or self-harm by stopping critical units (`sshd`, network, `dbus`, Homelab Control itself) | High | Built-in protected-unit policy enforced **in the broker**; disruptive-op confirmation shows impact |
| Argument injection into `systemctl`/`journalctl` | High | Unit-name grammar check, `--` separator, existence check, fixed argv, no shell; fuzz tests |
| CSRF or confused-deputy on new POST routes | Medium | Existing Origin + token layers apply to every non-GET route; plan token bound to session user |
| Replay or race (TOCTOU) between plan and execution | Medium | Single-use, short-lived broker-minted tokens bound to actor, op, target and observed state; revalidation at execute; per-target locks |
| Log disclosure (journal lines may contain secrets) | Medium | Separate scope `services.logs`, line and byte caps, no persistence of log content, audit of reads |
| Dependency risk from SQLite driver | Medium | Pure-Go driver pinned, govulncheck, licence review, reproducibility CI (D-1) |
| Upgrade/migration failure of `state.db` | Medium | Versioned migrations, backup before migrate, package upgrade test |

## 7. Dependencies
- **D-1 SQLite driver.** Needs an ADR 0006 amendment.
- **D-3 step-up.** The API owns the password check, so no broker change is needed.
- **D-4 `root-exec`.** Needs an ADR 0008 amendment. This and D-2 are captured in the proposed ADR 0011 (write operations).
- **Owner VM** (`dev/vm/`). Needed for real-systemd evidence of the first write op on Ubuntu 26.04, plus at least one non-Debian distro.
- **No external services and no new host packages.** M2 uses `systemctl` and `journalctl`, already present on every supported distro.

## 8. Webmin independence [Verified approach]
- Everything proposed for M2 is native:
  - systemd through `systemctl`/`journalctl` argv templates;
  - storage inventory through sysfs and `/proc/self/mountinfo`;
  - layouts in the product's own state.
- No Webmin RPC, CGI proxy or code copy is needed.
- The ADR 0003 parity harness has no M2 consumer (no Webmin parser is being replaced in M2), so it is recommended for deferral to the first milestone that replaces a Webmin parser (D-8).

## 9. Decisions requiring owner approval
Full register: `OWNER-DECISIONS.md`.
- **Needed for PR-1:** D-1 (persistence engine), D-6 (persistent sessions), D-7 (scope catalogue).
- **Needed before PR M2-2:** D-2 (write-op model), D-3 (step-up), D-4 (`root-exec`), D-5 (protected units).
- **Needed for roadmap shape:** D-8 (milestone order), D-9 (host administration in M2), D-10 (built-in modules vs plugins).
