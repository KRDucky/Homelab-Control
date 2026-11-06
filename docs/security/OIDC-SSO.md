<!--
SPDX-FileCopyrightText: 2026 The Homelab Control Authors

SPDX-License-Identifier: GPL-3.0-or-later
-->

# OIDC single sign-on (working skeleton)

**Status:** development slice for review (2026-11-05). This has not been tested against a real provider and has not been accepted. Libraries: ADR 0006 amendment 3.

## What it does
One administrator-configured OpenID Connect provider can sign a user in to an existing local account. The result is the ordinary hc-api session: the same cookie, CSRF token, idle and absolute timeouts, revocation and audit trail. Scopes always come from the local account in `users.json`, never from token claims. Local password login is unchanged and stays the recovery path.

## Configuration
Pass `-oidc-config /etc/homelab-control/oidc.json` in `HC_API_ARGS`. With no flag, OIDC is off.

The file must be a regular file with mode 0600, owned by `homelab-control`. Symlinks are refused.

```json
{
  "version": 1,
  "issuer": "https://idp.example.lan/realms/home",
  "client_id": "homelab-control",
  "client_secret": "…",
  "redirect_url": "https://homelab.example.lan/api/v1/auth/oidc/callback",
  "accounts": [{ "subject": "<exact sub claim>", "username": "alice" }]
}
```
*(Example values only.)*

- `issuer` must use https. Discovery runs on the first sign-on attempt rather than at startup, so an unreachable provider never stops hc-api.
  - The discovered authorization, token and JWKS endpoints must also use https.
- `redirect_url` must be exactly `scheme://host[:port]/api/v1/auth/oidc/callback`, and it must be registered at the provider. Plain http is accepted only for loopback hosts, in development.
- `client_secret` is optional. Without it, hc-api acts as a public client protected by PKCE.
- `accounts` binds an exact `sub` of this issuer to a local username. Nothing else creates a binding: no email, no preferred_username, no groups, no domain trust and no automatic accounts. A login is refused when the subject is unbound, or when the bound account is missing or disabled.
  - The audit log records refused subjects, so an administrator can copy one from there into `accounts`.
- Startup fails if the file is invalid. Startup logs a warning, but does not fail, for a binding to a missing or disabled account.

## Flow and security properties
1. `GET /api/v1/auth/oidc/start` creates the per-login values and redirects (303) to the provider:
   - a random state, nonce and PKCE S256 verifier, each 256 bits;
   - a browser-binding cookie, `__Host-hc_oidc`: HttpOnly, Secure, `SameSite=Lax`, 10 minutes.
   
   Pending logins are held in memory: at most 1024, each expires after 10 minutes, and the oldest are evicted first. Only the SHA-256 of the state and of the binding is stored.
2. `GET /api/v1/auth/oidc/callback` checks the following, and every failure redirects to the fixed URL `/?login=sso_failed`:
   - The state must be known and unexpired. It is consumed on first use, whatever the outcome.
   - The binding cookie must match, compared in constant time.
   - The code exchange must succeed with the PKCE verifier.
   - The ID token must pass verification: signature against the issuer's JWKS (RS256 or ES256 only), exact issuer, audience containing `client_id` (and `azp` when there are several audiences), and expiry.
   - The nonce must match.
   - The subject must be bound to an enabled local account.
   
   On success, hc-api discards any old session cookie, creates the session and redirects to `/`.
3. Redirect targets are fixed, so there is no open redirect. The provider's `error` text is neither logged nor reflected.
4. Neither the ID token, the access token, the authorization code nor the client secret is stored, logged or returned. The audit log records `auth.oidc.login` with a fixed reason string.
5. Provider requests use a 10 s timeout, follow no redirects, require TLS 1.2 or later and are capped at 1 MiB per response.
6. **Step-up is unchanged:** an OIDC login never records a password re-entry (D-3, ADR 0011). Privileged applies still answer `step_up_required`, and the user satisfies it with the bound local account's password. A test covers this.

## Decisions for review
- **Client secret location.** ADR 0004 assigns secrets to the broker. Here, the unprivileged hc-api reads the OIDC client secret from its own 0600 config file, the same protection as `users.json`. A broker-held secret would mean the token exchange runs in the root broker, which must not link third-party code. A public client (PKCE, no secret) avoids the question entirely. This is recorded as a narrow, Proposed exception in ADR 0004 (supervisor design disposition, 2026-11-06). It is not an owner acceptance.
- **Step-up for OIDC users** uses the local password (current behaviour, unchanged). A design that accepts provider-asserted authentication assurance (`acr`/`auth_time`, re-authentication with `prompt=login`) has not been done.
- **The binding cookie uses `SameSite=Lax`.** This is required because the provider's redirect back is a cross-site top-level GET. The session cookie stays `Strict`.
  - After the 303 to `/`, the browser does not send the Strict session cookie on that one top-level navigation. The SPA's following `fetch` calls are same-site, so they carry it.
  - Checked in Chromium by `tests/e2e/oidc_browser.py`, which uses a loopback fixture: the test IdP runs on `https://127.0.0.1` and the app on `http://localhost`. Before navigating back, the user clicks on the IdP's own page, as with a real login form.
    - With `SameSite=Lax` the login succeeds.
    - With `Strict` it fails. This was checked by mutation on 2026-11-05.

## Not done (deferred)
- Real-provider acceptance.
- Provider-initiated logout.
- More than one provider.
- Group or role mapping.
- An admin UI for bindings.
- Refresh tokens (none are requested or kept).
- Persisting pending logins across restarts.

## Tests
Real browser: `/usr/bin/python3 tests/e2e/oidc_browser.py` (needs `make frontend`; development only, never packaged). Go: `backend/internal/api/oidc_test.go` runs against an isolated in-process TLS provider (`internal/oidclogin/testidp`). That provider enforces exact `client_id` and `redirect_uri`, client authentication, the S256 challenge and one-use codes.

Covered:
- the happy path through to the existing session;
- step-up still required;
- logout;
- no token in the audit log;
- replay.

Rejected:
- missing or wrong binding cookie;
- forged state;
- expired state;
- a PKCE mismatch;
- a provider error;
- wrong audience;
- several audiences without `azp`;
- wrong issuer;
- an expired token;
- a foreign signing key;
- a nonce mismatch;
- an unbound subject;
- a disabled account;
- OIDC not configured;
- config validation.
