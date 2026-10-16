# ADR-0002: API authentication, session and CSRF model

**Status:** Accepted for M0 (implemented). PAM integration **deferred** (owner, 2026-10-16) — see `docs/security/PAM-AUTH-DEFERRED.md`.
**Date:** 2026-10-15
**Deciders:** Project owner

## Context
The API is reached by a same-origin SPA on a LAN/tailnet, often over self-signed TLS or behind a local
reverse proxy. Upstream Webmin relies on Referer checks plus `SameSite=Lax`; its auth code has had
several bypasses (Basic auth skipping 2FA, spoofed client-cert headers). We must not depend on Webmin
for auth (ADR 0001) and cannot read root-only `/etc/shadow` or `miniserv.users` from an unprivileged
process.

## Decision
- **Accounts (M0):** local Homelab Control accounts in `/etc/homelab-control/users.json`, mode 0600 (the
  server refuses group/other-accessible files and symlinks). Passwords hashed with
  **PBKDF2-HMAC-SHA256, 600,000 iterations, 16-byte salt** (Go stdlib `crypto/pbkdf2`); hashes below
  210,000 iterations are rejected. Managed with `hlctl user add|passwd|disable|list`.
  Unknown and disabled users still incur one hash verification (timing equalisation).
- **Sessions:** 256-bit random IDs in a cookie named `__Host-hc_session` (`Secure`, `HttpOnly`,
  `SameSite=Strict`, `Path=/`) whenever TLS is in use; `hc_session` without `Secure` only in
  loopback-only dev mode. Server stores **SHA-256(session ID)**, never the raw ID. Idle timeout 30 min,
  absolute 12 h, table capped at 1,024 sessions, rotated on every login (fixation defence), and the
  account is re-checked on each request. Sessions are in memory: restart logs everyone out.
- **CSRF:** two independent layers on every non-GET request: (1) `Origin` must exactly match the
  request's own scheme+host or an explicitly configured origin — missing/`null` Origin is rejected; (2)
  a per-session CSRF token returned by `/auth/login` and `/auth/session` must be echoed in
  `X-CSRF-Token`. Login itself is Origin-checked. JSON-only bodies (`415` otherwise), 64 KiB limit,
  unknown fields rejected.
- **Authorization:** every route is registered through one wrapper that requires a declared scope;
  a guard test fails the build if a route is attached to the mux directly. M0 scope: `system.read`.
- **Throttling:** 5 failures per (user, IP) or 20 per IP in 15 min → 15 min lockout (`429`);
  at most 4 concurrent password verifications.
- **Transport:** hc-api refuses to start without TLS unless `-behind-tls-proxy` (loopback listen only)
  or `-dev-insecure-http` (loopback only). HSTS in secure mode. Strict CSP (`default-src 'self'`, no
  inline script/style), `frame-ancestors 'none'`, `nosniff`, `no-referrer`, `no-store` on API responses.
- **Audit:** login success/failure/throttle, logout, Origin/CSRF rejections and authorization denials
  written as JSON Lines (0600) without secrets; control characters stripped to prevent log forging.

## Options Considered

### Option A: Own accounts + server-side sessions + Origin + token (chosen)
| Dimension | Assessment |
|---|---|
| Complexity | Low — stdlib only |
| Security | Strong for a single-host admin tool; no third-party auth code |
| UX | Extra account to create on install (CLI) |

### Option B: Reuse Webmin sessions (validate `sid` against miniserv)
Couples us to root-only session DB and to miniserv's auth history; cookie is `SameSite=Lax`. Rejected.

### Option C: PAM login via the broker
Lets admins use Unix accounts and their 2FA PAM stack. Requires root (shadow access) → belongs in the
broker as `auth.pam.verify`. **Proposed for M1**, with an explicit allow-list group (e.g.
`homelab-admins`) mapping to scopes.

### Option D: Stateless JWT
Revocation and rotation are harder; no benefit for a single host. Rejected.

## Trade-off Analysis
Own accounts give a safe M0 without root, at the cost of one more credential. PAM-through-broker in M1
removes that cost without moving password checks into the web tier.

## Consequences
- Easier: tests of every security boundary run without root (see `internal/api/server_test.go`).
- Harder: multi-instance HA (not a goal), persistent sessions (M2 can add SQLite-backed sessions).
- Revisit: argon2id when a vetted dependency is allowed (ADR 0006); WebAuthn/TOTP in M2.

## Action Items
1. [x] Implement and test the above (M0).
2. [ ] PAM verification broker op + group→scope mapping — **deferred past M1**; needs its own ADR (first argument-taking broker op). Acceptance tests: `docs/security/PAM-AUTH-DEFERRED.md`.
3. [ ] Persisted sessions and per-session revocation UI (M2).
