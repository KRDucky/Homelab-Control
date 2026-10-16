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

## Residual risks after M1
1. The legacy Webmin UI at 2.670 still lacks post-2.670 upstream fixes (`docs/upstream/SYNC.md`).
2. `systemctl` JSON output is not a stable API; parsers are tolerant and tested against systemd 249/255 only.
3. Real systemd validation used disposable containers (systemd PID 1, cgroup v1 host), not a full VM
   with a real kernel boot; CI adds a GitHub runner VM (pending first run).
4. govulncheck has not run in this environment (blocked); it runs in CI.
5. Sessions and throttling remain in memory.
