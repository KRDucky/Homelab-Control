# PAM authentication — explicitly deferred (not in M1)

**Status:** Deferred by owner decision (2026-10-16). M1 ships **local Homelab Control accounts only**.
ADR 0002 Option C (PAM via the broker) remains the intended design, but it is not implemented, not
enabled and not tested.

## Why it is deferred
- PAM needs root (shadow access, PAM stacks with `pam_unix`/2FA modules), so verification must live in
  the broker as an operation such as `auth.pam.verify`.
- That would be the **first broker operation that accepts arguments** (a username and a secret), and the
  first whose result grants access. ADR 0008 is accepted only for the four existing read-only,
  argument-free operations, so PAM needs its own design review and owner approval.
- Group → scope mapping (e.g. `homelab-admins` → scopes) is an authorization policy decision that has
  not been made.

## What protects logins today (local accounts) — implemented and tested
| Control | Value | Where |
|---|---|---|
| Password hashing | PBKDF2-HMAC-SHA256, 600,000 iterations (verify rejects < 210,000 and > 10,000,000), random salt | `internal/auth/password.go` |
| Comparison | constant time; a dummy hash equalises timing for unknown users | `internal/auth/password.go` |
| Login throttling | 5 failures per (user, IP) or 20 per IP in 15 min → 15 min lockout | `internal/auth/throttle.go` |
| Sessions | 256-bit random IDs, 30 min idle, 12 h absolute, max 1024; in memory (lost on restart) | `internal/auth/session.go` |
| Cookie | `__Host-hc_session`, `Secure`, `HttpOnly`, `SameSite=Strict` whenever TLS is in use (direct or `-behind-tls-proxy`); plain `hc_session` only in loopback `-dev-insecure-http` | ADR 0002 |
| CSRF | per-session token + Origin check on every state-changing request; JSON-only login | ADR 0002, live checks |
| Account file | `/etc/homelab-control/users.json`, must be mode 0600, opened with `O_NOFOLLOW` + fstat | `internal/auth/users.go` |
| Account management | `hlctl user add/…` on the host as root only; no web endpoint creates accounts | `cmd/hlctl` |
| Authorization | explicit scopes per account (`system.read`, `services.read`, `hardware.read`); server-side on every route | `internal/api`, guard tests |
| Audit | login success/failure, denials, CSRF rejections to a 0600 JSONL log; no secrets logged (tested) | `internal/audit` |

Known limits: no MFA, no password-change endpoint, sessions and throttle state are per-process memory.

## Acceptance tests for a future PAM feature (to be written before enabling it)
Run only in disposable VMs (`dev/vm/`), never against a real host's accounts.
1. **Allow-list:** a Unix user outside the configured group is rejected even with the correct password;
   inside the group is accepted and receives exactly the mapped scopes.
2. **Fail closed:** broker down, PAM module error, unknown service name or misconfigured stack → login
   refused, with an audit event; never falls back to "allow".
3. **Locked / expired accounts:** `passwd -l`, expired password and `nologin` shells are rejected; the
   behaviour of PAM account-management (`pam_acct_mgmt`) is enforced, not only authentication.
4. **Root and system accounts:** `root` and UID < 1000 are rejected unless explicitly allowed.
5. **Secret handling:** the password never appears in argv, environment, logs, audit, core dumps or the
   broker's error responses; the broker request is length-bounded and the field is zeroed after use.
6. **Throttling:** the existing per-user/per-IP throttle applies before the broker is called; the broker
   applies its own rate limit so a compromised hc-api cannot brute-force through it.
7. **Peer authentication:** only the `homelab-control` UID can call the op (existing broker peer check);
   other local users are refused.
8. **Timing:** response time for unknown user vs wrong password is indistinguishable within tolerance.
9. **2FA stacks:** with `pam_google_authenticator`/`pam_u2f` configured, non-interactive conversation
   fails closed (no silent bypass); interactive 2FA is out of scope until designed.
10. **Coexistence:** local accounts keep working; the same username in both sources has a defined,
    tested precedence.
11. **Packaging:** the PAM service file (`/etc/pam.d/homelab-control`) is installed per distro
    (Debian/Ubuntu, Fedora, openSUSE, Arch) and removed on purge.

## Preconditions to start
Owner approval of a new ADR (or ADR 0008 amendment) for an argument-taking, access-granting broker op;
a group → scope policy; and M2's audit/locking design, since this becomes a security-critical path.
