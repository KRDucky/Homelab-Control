# M2 owner decision register

**Date:** 2026-10-21. Each entry is a decision **not** already settled by an accepted ADR or recorded
owner decision.

Already settled, and not asked again:
- the broker model (ADR 0001/0008);
- no Webmin bridge or proxy (ADR 0003);
- secrets in root-owned broker files (ADR 0004 secrets section);
- licensing (ADR 0005);
- stdlib-first (ADR 0006);
- release phases (ADR 0009);
- independent modules plus optional coordinated workflows, the technology scope, and the drive layout designer (owner, 2026-10-21).

| ID | Needed before | Status |
|---|---|---|
| D-1 | PR-1 | **Approved 2026-10-21 (option A)** |
| D-6 | PR-1 | **Approved 2026-10-21** |
| D-7 | PR-1 | **Approved 2026-10-21** |
| D-2, D-3, D-4, D-5 | PR M2-2 (first write) | **Approved 2026-10-23 (option A each)** |
| D-8, D-9, D-10 | Roadmap shape | Open |
| D-11 | Before any third-party library or tool adapter (M3+) | Open (policy only) |
| D-12 | PR M2-5 (drive locator) | Open (HBA Viewer source to be provided) |

---

## D-1 Persistence engine for non-secret state
**Decision:** how to provide the SQLite state store that ADR 0004 assigns to M2, given ADR 0006 (stdlib only).

| Option | Pros | Cons / risks |
|---|---|---|
| A. `modernc.org/sqlite` (pure Go) | `CGO_ENABLED=0` and reproducible builds unchanged; real SQL, WAL, migrations; BSD-3 | Large transitive module set (modernc libc translation); larger binary; dependency review and govulncheck coverage needed |
| B. `mattn/go-sqlite3` (cgo) | Most used; uses real SQLite C | Requires cgo and a C toolchain per distro; breaks the static, `CGO_ENABLED=0` reproducible build verified in Phase 1 |
| C. No SQL: stdlib append-only journals (JSONL + fsync) and JSON snapshot files | No dependency | Queries (audit search, jobs, layouts, resource history) re-implemented by hand; this would likely be replaced later, so the work is done twice |

**Owner decision 2026-10-21: option A approved.** CGO-disabled reproducible builds preserved; ADR 0006 amended for the reviewed exception; dependency and transitive modules pinned and audited; licensing, SBOM and scanning requirements kept; secure state storage, transactional migrations and migration backups.

**Recommended: A.**
- Amend ADR 0006 with a recorded review: licence check, pinned version, `go.sum`, govulncheck, and inclusion in the SBOM and notices generators.
- The DB is owned by `hc-api` at `/var/lib/homelab-control/state.db`, mode 0600.
- The broker's authoritative audit and operation journal stay stdlib files owned by root (see D-2), so a compromised API cannot rewrite history.

**Roadmap impact:**
- A: PR-1 as proposed.
- C: PR-1 shrinks, but every later persistence feature (layouts, jobs, audit search) costs more.
- B: reopens the reproducibility work.

## D-2 Write-operation authorization model (proposed ADR 0011)
**Decision:** how a privileged write is requested, confirmed and executed.

**Proposed model (plan → confirm → execute):**
1. The UI asks the API for a plan.
2. The API calls `<op>.plan` on the broker. The broker:
   - validates arguments;
   - resolves the target (for example, the unit's canonical ID and current state);
   - applies the policy (protected units);
   - returns a preview, the risk class (reversible / disruptive / destructive) and a **plan token**.
3. The plan token is minted by the broker: an HMAC with a per-boot random key, never leaving root memory. It is bound to actor, op, canonical target, an observed-state digest and an expiry (120 s). It is single-use.
4. The user confirms. For disruptive ops the API also requires step-up (D-3).
5. The API calls `<op>.apply` with the token.
6. The broker then:
   - verifies the token and consumes it;
   - takes the target lock;
   - **revalidates** the target and state;
   - writes an intent journal record (fsync);
   - executes;
   - records the outcome (fsync);
   - returns.

| Option | Pros | Cons |
|---|---|---|
| A. Broker-minted plan tokens + revalidation + locks + journal (above) | Replay-proof; TOCTOU bounded; audit outside API control | More moving parts; honest limit: a compromised API can still plan and apply allowed ops, so policy and narrow ops carry the real safety |
| B. API-only confirmation (token in API session) | Simpler | Broker trusts API completely; no replay or TOCTOU guarantees at the root boundary |
| C. polkit interactive authorization | OS-native | Not headless-friendly; distro-divergent; no web UX |

**Recommended: A**, recorded as ADR 0011 before PR M2-2.

**Owner decision 2026-10-23: option A approved.** Tokens minted and verified inside the broker,
120 s, single use, bound to operation, canonical target, actor, session and observed state; target
lock, revalidation, durable intent before execution, outcome after. The threat-model limitation (a
compromised API can still request operations the broker's policy allows) is documented in ADR 0011.

**Roadmap impact:** PR M2-2 implements the generic plan/apply framework once; later write ops reuse it.

## D-3 Step-up authentication for disruptive operations
| Option | Pros | Cons |
|---|---|---|
| A. Password re-entry, valid 5 minutes per session, required for disruptive and destructive classes | No new dependency; uses existing PBKDF2 path and throttling | Protects against session theft or an unattended browser, not against an API compromise |
| B. TOTP (stdlib HMAC-SHA1, RFC 6238) enrollment via `hlctl` | Real second factor | Enrollment UX and recovery codes; more scope for M2 |
| C. None (confirmation dialog only) | Simplest | B3 stays open |

**Recommended:** A in PR M2-2. B as a follow-up PR, in M2 if wanted.

**Owner decision 2026-10-23: option A approved.** Password re-entry, 5 minutes, bound to the session
and actor, existing throttling preserved, never sent to the broker or logged, cleared on revocation
or expiry. Required for stop and restart, and for start when dependencies make it disruptive.

## D-4 How the broker runs root-level service commands
| Option | Pros | Cons |
|---|---|---|
| A. New runner class `root-exec`: allow-listed binary, argv template fixed in code (e.g. `systemctl [start\|stop\|restart] -- <validated unit>`), child runs as root, same timeout, output caps and scrubbed env | Small change, auditable, no dependency | First root child processes; argv templates must stay fixed |
| B. systemd D-Bus API (`org.freedesktop.systemd1`) | Typed API, no parsing | Needs a D-Bus client: a third-party module, or a hand-written protocol implementation |
| C. polkit rule letting `homelab-control` manage units | No root child | polkit not always installed; per-distro rules; grants the API's uid the power directly (defeats the broker) |

**Recommended: A.** It amends ADR 0008 (closed registry; argv templates reviewed per op). B can be revisited if parsing `systemctl` becomes a problem.

**Owner decision 2026-10-23: option A approved** (`root-exec`; ADR 0008 Amendment 2). Fixed
`systemctl` argv with the validated unit as a separate argument; no generic root execution API.

## D-5 Protected-unit policy for service operations
| Option | Pros | Cons |
|---|---|---|
| A. Built-in deny-list enforced by the broker, not configurable in M2. Covers: Homelab Control's own units; `ssh`/`sshd`; `dbus*`; `systemd-*`; `getty@`/`serial-getty@`; `user@`; network managers (`NetworkManager`, `systemd-networkd`, `wpa_supplicant`, `networking`); display managers; and any unit with `RefuseManualStop=yes`. Stop is refused for these; restart is refused unless listed as restart-safe (none in M2-2) | Prevents lockout and self-termination | Admins cannot manage these units from the UI in M2 |
| B. Deny-list plus owner-editable allow-overrides in `/etc/homelab-control/policy.json` (root-owned) | Flexible | Policy file becomes another root-trust input |
| C. No policy, confirmation only | Simplest | Lockout risk |

**Recommended:** A now; B later with its own review.

**Owner decision 2026-10-23: option A approved.** Built-in, broker-enforced, no user-editable
overrides in M2-2; aliases, templates, alternate names and dependencies must not bypass it.

## D-6 Persistent sessions
ADR 0002 schedules "persisted sessions and per-session revocation UI" for M2, but the move from memory to disk changes behaviour.
- **Owner decision 2026-10-21: approved.** Hashes only; existing idle, absolute and count limits; view and revoke own sessions; fixation protections and per-request checks kept.
- **Recommended: yes.** Store only SHA-256(session ID) in `state.db`, keeping the existing timeouts; add a session list and revoke-other-sessions in the UI. Throttle state is persisted too.
- **Alternative:** keep in-memory sessions; restarts log everyone out (availability only).
- **Impact:** part of PR-1.

## D-7 Scope catalogue v1
**Owner decision 2026-10-21: approved**, compatible with existing scopes; logs and audit access are **not** granted automatically to existing accounts.

**Recommended convention** `<module>.<level>`. Levels:
- `read`: inventory and status.
- `logs`: may include sensitive content.
- `operate`: reversible or disruptive runtime actions, e.g. start, stop, restart.
- `configure`: persistent configuration, e.g. enable or disable, edit.
- `destroy`: irreversible data loss, e.g. wipe, destroy pool.

Examples:
- **M2:** `services.read`, `services.logs`, `services.operate`, `services.configure`, `storage.read`, `layout.manage`, `audit.read`, `sessions.manage`.
- **Later:** `storage.plan`, `storage.execute` and `storage.destroy` per technology, `docker.operate`, `vm.operate`, `shares.configure`.

Existing scope names stay valid. Specs 03/addon-03 names (`storage.plan`, `storage.execute`, `shares.*`) map onto this.
- **Alternative:** one coarse `admin` scope. Rejected: it defeats the least-privilege intent of spec 03.

## D-8 Milestone order after M2
`docs/project/04-ROADMAP.md` orders M3 Theme/Plugin SDK → M4 Software Center → M5 disk inventory and bay map → … → M11 containers/VMs.

**Recommended, to deliver usable modules sooner:**
- (a) M2 as in `M2-ROADMAP.md`. This pulls **drive locator Stage A** and the **read-only storage inventory** forward from M5; both are non-privileged.
- (b) Defer the Theme/Plugin SDK (old M3) until after the first storage and container modules. Modules ship as built-in first-party modules (D-10).
- (c) Defer the ADR 0003 parity harness to the first milestone that replaces a Webmin parser.
- (d) Adopt the module milestone sequence in `M2-ROADMAP.md` §4.

**Alternative:** keep 04-ROADMAP as written. Theme/plugin work comes before any module; useful functions arrive later.

**Impact:** 04-ROADMAP would be revised in a follow-up docs PR once approved. It is unchanged here.

## D-9 Host administration in M2
- **Recommended:** hostname and timezone as an **optional** M2 slice (PR M2-6). These are low-risk, through `hostnamectl`/`timedatectl`, and they exercise the write framework on a second module.
- Users and groups move to their own milestone: lockout, sudo and UID/GID ownership risks, and they are tied to identity work (PAM is still deferred).
- **Alternative:** no host administration in M2.

## D-10 Module packaging: built-in modules vs plugins
- **Recommended:** first-party modules are **compiled into** `hc-api`/`hc-broker` as independent packages behind a module registry: capability detection, enable/disable, scopes, routes, broker ops.
- Third-party plugins wait for the Plugin SDK (spec 06). This keeps one reviewed broker binary and avoids runtime code loading, which ADR 0001 forbids for privileged code anyway.
- **Alternative:** design the plugin SDK first, so modules are plugins from day one. This is slower and adds a privileged-plugin review problem before any module exists.

## D-11 Third-party integration libraries (policy for later milestones)
Not needed for M2; recorded so it is not decided ad hoc.
- **Docker:** Engine API over the Unix socket with stdlib `net/http`; no SDK.
- **libvirt:**
  - `virsh` argv templates with XML/`--print-xml` parsing (stdlib), or
  - the pure-Go `digitalocean/go-libvirt` (Apache-2.0; ADR 0006 review).
- **ZFS:** `zpool`/`zfs` JSON (`-j`, OpenZFS ≥ 2.3) or `-Hp` output.
- **Broadcom `storcli`:** proprietary; invoke only if the user installed it, never bundle.

**Recommended:**
- the stdlib approach for Docker;
- `virsh` first for libvirt, re-evaluated at the virtualization milestone;
- `storcli` optional.

## D-12 Source of the "HBA Viewer" concept and reuse terms
**Owner direction (2026-10-21):** reuse the existing HBA Viewer concept inside Homelab Control rather than
building a separate drive-location system.

**Status:** pending.
- **Correction:** the owner clarified that the HBA Viewer and the Linux Storage Manager are **distinct projects**. The Storage Manager is not the intended source.
- **Needed:** the HBA Viewer's location (repository or folder) and the reuse terms: concept-only re-implementation (recommended), or code contributed by the owner under GPL-3.0-or-later.
- **Blocks:** PR M2-5 (drive locator). Nothing in PR-1.
