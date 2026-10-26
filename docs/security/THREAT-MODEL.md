# Threat model (M0 baseline)

Scope: `hc-api` + web UI as built in M0, coexisting with an optional upstream Webmin. Revisit at every
milestone that adds a write path, the broker, plugins or remote access.

## Assets
Host integrity (root), data on disks, admin credentials and sessions, future secrets (DNS tokens, ACME
and TLS keys), audit trail integrity, availability of remote management (lockout).

## Actors
- Unauthenticated network client on the LAN/tailnet.
- Malicious website visited by a logged-in admin (CSRF, clickjacking, XS-leaks).
- Authenticated user with fewer scopes than admin.
- Malicious or compromised plugin/theme author (M3+).
- Compromised upstream or dependency (supply chain).
- Local unprivileged user on the host.

## Trust boundaries
Browser ↔ hc-api (HTTP/TLS) · hc-api ↔ broker (Unix socket, M1+) · broker ↔ host tools/kernel ·
build/CI ↔ release artifacts · upstream Webmin ↔ fork.

## Threats and controls (STRIDE)

| Threat | Example | M0 control | Status |
|---|---|---|---|
| Spoofing | Password guessing | PBKDF2 600k, per-user/IP lockout, constant-time compare, timing-equalised unknown users | Implemented, tested |
| Spoofing | Session theft via XSS | HttpOnly cookies, strict CSP without inline script, React escaping, lint ban on `dangerouslySetInnerHTML`/`innerHTML` | Implemented, tested |
| Spoofing | Session fixation | New ID on every login, old one destroyed | Implemented, tested |
| Tampering | CSRF on state-changing routes | Exact Origin match + per-session token + SameSite=Strict + JSON-only bodies | Implemented, tested |
| Tampering | Clickjacking | `frame-ancestors 'none'`, `X-Frame-Options: DENY` | Implemented, tested |
| Tampering | Log forging via username | Control characters stripped, JSON encoding | Implemented, tested |
| Repudiation | Admin denies action | Audit log of auth/authz events (0600); write actions audited from M2 | Partial (no writes exist yet) |
| Information disclosure | Unauthenticated host data | Every route except `/health` requires a session; `/health` returns no host data | Implemented, tested |
| Information disclosure | Secrets in logs | No secrets reach the audit writer; test asserts password/CSRF/cookie absent | Implemented, tested |
| Information disclosure | Users file readable | Server refuses group/other-readable or symlinked users file | Implemented, tested |
| Denial of service | Hash-flooding login | 4 concurrent verifications, lockouts, 64 KiB body limit, header/read timeouts | Implemented |
| Denial of service | Hung NFS statfs | Network filesystems are listed but not probed | Implemented, tested |
| Elevation of privilege | Web tier → root | hc-api unprivileged, no process execution (guard test), refuses root, hardened unit | Implemented, tested (unit file reviewed, not run under systemd here) |
| Elevation of privilege | Generic RPC/bridge | No generic endpoint; Webmin RPC never used (ADR 0003); unknown `/api` routes 404 | Implemented, tested |
| Elevation of privilege | Theme/plugin code execution | Themes declarative (M3); plugins UI-only by default | Design only |
| Supply chain | Malicious dependency/build | stdlib-only Go, `npm ci --ignore-scripts`, lockfile, reproducible builds, pinned upstream tree | Implemented; signing pending |
| Wrong-target ops | `/dev/sdX` renaming | Overview labels kernel names as volatile; no disk operations exist | Design (M5) |
| Lockout | Network change cuts access | No network writes in M0; timed rollback required later | Design (M11) |

## Residual risks (M0)
1. Upstream Webmin legacy UI, if installed, still has the post-2.670 issues listed in `docs/upstream/SYNC.md`.
2. Sessions are in memory; a restart logs everyone out (availability, not security).
3. No TLS certificate management yet; operators must supply certs or a reverse proxy.
4. `govulncheck`/`shellcheck` could not run in the build environment (network policy); they run in CI.
5. The systemd hardening profile has not been exercised on a real systemd host yet.

## M1 additions: privileged broker (ADR 0008)

New trust boundary: `hc-api` (uid homelab-control) ↔ `hc-broker` (root) over
`/run/homelab-control/broker.sock` (0660 root:homelab-control, systemd socket activation).

| Threat | Example | M1 control | Status |
|---|---|---|---|
| Elevation of privilege | Web-tier compromise asks broker to run a command or read `/etc/shadow` | Closed op registry (4 ops), no args accepted, no generic exec/file/RPC op; registry test fails if ops are added without review | Implemented, tested |
| Elevation of privilege | Command injection via helper arguments | `runner` is the only `os/exec` user (guard test); allow-listed absolute binaries, root-owned, not group/world-writable; fixed argv; argument charset check; replaced env; no shell | Implemented, tested (incl. injection strings) |
| Elevation of privilege | Helper process runs as root | Children run as `homelab-control` via `SysProcAttr.Credential`; observed live under systemd 249/255 | Implemented, verified |
| Spoofing | Another local user talks to the broker | Socket mode 0660 + `SO_PEERCRED` uid allow-list (homelab-control, root); rejections audited | Implemented, verified (`nobody` refused) |
| Information disclosure | Broker returns secrets from Webmin config | Allow-listed fields only; usernames, never hashes; key files never opened; test asserts hashes/keys absent | Implemented, tested on real Webmin 2.670 |
| Tampering | Symlink planted at a root-read path | `O_NOFOLLOW` + regular-file check + size caps; pidfile path confined to /var or /run | Implemented, tested |
| Denial of service | Slow/oversized/concurrent requests, hung `systemctl` | 5 s read deadline, 16 KiB request cap, 4 MiB response cap, 8 in flight, per-op timeouts killing the process group, output caps, panic containment | Implemented, tested |
| Repudiation | Broker use not attributable | Broker audit log (root 0600): peer uid/pid, actor, op, outcome, duration | Implemented, verified |

**Explicit assumption:** a compromised `hc-api` can invoke any registered op with any claimed scope
(the broker cannot verify end-user sessions). M1 keeps that safe because every op is read-only,
argument-free and returns bounded, non-secret data. Write ops (M2+) need broker-side confirmation and
locking before they may exist.

**Sensitive data newly exposed to authorised users:** SMBIOS serials/UUID (scope `hardware.read`),
Webmin usernames with RPC capability (scope `system.read`), the full systemd unit list (scope
`services.read`). These are read-only but identifying; grant scopes accordingly.

## M2 PR-1b additions: service logs and hash-chained audit

| Threat | Example | Control | Status |
|---|---|---|---|
| Elevation of privilege | Option or argument injection through a unit name (`--output=…`, `$(id)`, `../`) | Grammar validated in `hc-api` and in the broker; the unit must be listed by systemd; values only inside `--unit=`/`--lines=`; strict argument decoding; fuzzed (`FuzzLogsArgs`) | Implemented, tested |
| Elevation of privilege | journal reader runs with more privilege than needed | `journalctl` as `homelab-control` plus `systemd-journal` only; gid 0 refused; binary allow-listed | Implemented, tested (unit); verified under real systemd in CI |
| Information disclosure | Journal entries contain secrets printed by services | Separate `services.logs` scope, not granted by default or on upgrade; capped output; the content is never written to audit logs | Implemented, tested |
| Information disclosure / XSS | Log messages containing HTML or terminal escapes | Control characters stripped server-side; rendered as React text, never as markup (Vitest asserts no element is created) | Implemented, tested |
| Denial of service | Huge or numerous log requests | ≤500 lines, 4 KiB per message, 1 MiB total, 15 s op timeout, broker concurrency limit | Implemented, tested |
| Repudiation / tampering | Audit records edited, reordered, deleted or truncated | SHA-256 hash chain with sequence numbers; checkpoints in journald; `hlctl audit verify`; fail-closed audit for broker results and log reads | Implemented, tested. **Tamper-evident only**: see the limits in `docs/security/AUDIT-LOG.md` (root can rewrite both the file and the journal) |

## M2 PR-1a additions: state database, persistent sessions, audit trail

| Threat | Example | Control | Status |
|---|---|---|---|
| Information disclosure / spoofing | State database copied or read: stolen sessions | Only SHA-256(session ID) is stored; the raw cookie never reaches disk (test checks the DB and WAL bytes); file 0600, directory 0700 `homelab-control`; CSRF tokens stored are useless without the cookie | Implemented, tested (unit + real systemd) |
| Tampering | Symlink or loose permissions planted at the database path, its directory or `-wal`/`-shm` | Refused at start (owner, mode, `O_NOFOLLOW`, regular file) | Implemented, tested |
| Tampering / availability | Failed or partial schema migration; downgrade to an older binary | Each migration in one transaction; consistent backup (`VACUUM INTO`, 0600) before migrating an existing database; a newer schema is refused; failure stops `hc-api` instead of running half-migrated | Implemented, tested (injected failure, backup, refusal) |
| Spoofing | Revoking or listing another user's sessions | List and revoke are scoped to the caller's username; unknown and foreign ids are both 404; revoke needs Origin + CSRF | Implemented, tested |
| Repudiation | API audit edited, reordered or deleted in the database | Same hash chain as the files, journald checkpoints, `hlctl audit verify --db`; a tampered last row starts a new segment instead of being extended | Implemented, tested. Tamper-evident only (`docs/security/AUDIT-LOG.md`) |
| Elevation of privilege | Vulnerability in the large SQLite dependency reaching root | Only `hc-api` (unprivileged) and `hlctl` link it; `hc-broker` is enforced standard-library only (`scripts/check-go-deps.py`); module versions and hashes pinned and reviewed (ADR 0006 amendment 1); govulncheck in CI | Implemented, enforced in CI |
| Information disclosure | Audit trail reveals who signed in, from where, and which logs were read | Separate `audit.read` scope, never granted by default or on upgrade (D-7) | Implemented, tested (upgrade test) |

Not changed: login throttling stays in memory (a restart clears it); admin-side management of other
users' sessions is not implemented (out of PR-1 scope).

## M2-2 additions: service start/stop/restart (ADR 0011, ADR 0008 Amendment 2)

New capability: the first privileged writes. New root child processes (`systemctl` start/stop/restart
only). New assets: the plan-token key (broker memory), the operation journal
(`/var/log/homelab-control-broker/operations.jsonl`, root 0600), step-up state (hc-api memory).

| Threat | Example | Control | Status |
|---|---|---|---|
| Elevation of privilege | Web-tier compromise runs an arbitrary command as root | No generic exec op; three fixed root argv templates in the runner; template ID + validated `.service` target after `--` are the only inputs; root handle reaches `services.unit.apply` only; guard test confines it to reviewed files | Implemented, tested (unit, guard, real systemd) |
| Elevation of privilege | Option or argument injection through the unit or action (`-x`, `--now`, `;`, `$()`, newline, NUL, templates, escapes) | Grammar in hc-api, broker and runner (three independent checks); action enum; strict JSON decode; fuzz test of the write-argument decoder | Implemented, tested |
| Spoofing / replay | A confirmed operation replayed, or a token reused for another unit, action, user or session | Broker-minted HMAC-SHA256 tokens (per-boot key in broker memory), 120 s, single use, bound to op, action, canonical target, actor, session, state digest and risk; restart invalidates all | Implemented, tested (unit + real systemd) |
| Tampering (TOCTOU) | Unit replaced, aliased, edited or its dependents changed between review and execution | Revalidation under the target lock: unit digest (identity, aliases, unit file, state, invocation, PID, safety properties, dependencies) and digest of the affected-unit set must match the token, else `conflict` | Implemented, tested (unit + real systemd out-of-band restart) |
| Denial of service / lock-out | Stopping SSH, the network, D-Bus, Homelab Control itself, storage, or rebooting through a dependency | Built-in protected-unit policy in the broker; names, aliases, unit file, template base; transitive dependency walk (≤96 related units) for stop propagation, pulled-in starts, conflicts, `*Action=` and shutdown triggers; RefuseManual*; refused when it cannot be verified | Implemented, tested (unit tables, real systemd incl. alias to the SSH unit and a two-hop chain) |
| Spoofing | Stolen session or unattended browser stops services | Step-up: password re-entry, 5 min, per session and user, throttled with logins, cleared on logout/revocation/disable; confirmation dialog never replaces it | Implemented, tested |
| Tampering (policy inputs) | A compromised hc-api signals, or reads/writes the output pipe of, an unprivileged `systemctl show` child (same uid) to hide a protected dependent | Apply revalidates and verifies the outcome with the read-only **root** `systemctl.show` template; plan-time reads may be tampered with but are re-checked at apply; repeated properties refused | Implemented, tested (path selection, template snapshot); found in the M2-2 security review |
| Repudiation | Who did what, and did it happen | API audit before execution (fail closed) and after; broker journal intent (fsync) before execution and observed outcome after; interrupted operations recorded at restart with the observed state; hash chains verified by `hlctl audit verify` | Implemented, tested (incl. broker killed mid-operation under real systemd) |
| Information disclosure | Tokens or passwords in logs | Passwords never leave hc-api; journal stores the token ID only; real-systemd test greps journal and broker audit for `hcp1.` and the password | Implemented, tested |
| Tampering (integrity of results) | UI reports success that did not happen | The broker reads the state back and classifies the **observed** result (`succeeded`/`failed`/`uncertain`); a unit that fails after systemd accepted the job is `failed`; no rollback claims; recovery guidance on failure | Implemented, tested (failing disposable unit) |
| Denial of service | Concurrent or slow operations; hung `systemctl` | One lock per unit (`busy`), `--job-mode=fail`, 30 s child timeout killing the group, bounded settle wait, 55 s op timeout, broker concurrency limit | Implemented, tested |

**Limitation (ADR 0011):** a compromised `hc-api` can still plan and apply anything the broker's policy
allows, for any user name, and can claim a step-up; tokens bind requests but do not authenticate the end
user to the broker. The protected-unit policy and the narrow op set carry that risk.

**Not covered in M2-2 (see ADR 0011, Known limitations):** operating on units outside `.service`;
enable/disable, mask, unit-file changes and daemon-reload (not implemented); services that are harmful
to stop but not on the built-in list (the operator decides with `services.operate`, never granted by
default); mutual exclusion between operations on *related* units; a protected dependent activated in
the moment between revalidation and execution.

## Residual risks after M1
1. The legacy Webmin UI at 2.670 still lacks post-2.670 upstream fixes (`docs/upstream/SYNC.md`).
2. `systemctl` JSON output is not a stable API; parsers are tolerant and tested against systemd 249/255 only.
3. Real systemd validation used disposable containers (systemd PID 1, cgroup v1 host), not a full VM
   with a real kernel boot; CI adds a GitHub runner VM (pending first run).
4. govulncheck has not run in this environment (blocked); it runs in CI.
5. Sessions and throttling remain in memory.
