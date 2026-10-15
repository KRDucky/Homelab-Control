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
