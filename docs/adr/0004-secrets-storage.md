# ADR-0004: Configuration, state database and secrets storage

**Status:** Accepted for non-secret state (2026-10-21, M2 PR-1a; owner decision D-1). Secrets handling remains Proposed.
**Date:** 2026-10-15
**Deciders:** Project owner

## Context
Later milestones hold high-value secrets: Cloudflare API tokens, ACME account keys, TLS private keys,
Tailscale auth keys, backup repository passwords. Spec 02 asks for SQLite (WAL) for jobs/audit/settings
and forbids credentials in ordinary plaintext records. Homelab hosts are usually headless, so desktop
keyrings (Secret Service) are not available; TPMs are present on some hardware only.

## Decision
- **Non-secret state** (jobs, audit index, settings, resource snapshots): SQLite in WAL mode at
  `/var/lib/homelab-control/state.db`, owned by `homelab-control`, mode 0600. Driver choice deferred to
  M2 (cgo `mattn/go-sqlite3` vs pure-Go `modernc.org/sqlite`) under the dependency policy (ADR 0006).
  M0 has no database.
- **Secrets are owned by the broker, not the API.** Stored as individual files under
  `/etc/homelab-control/secrets/<scope>/<id>` (root:root, dir 0700, files 0600), written atomically,
  never logged, never returned in full by any API (only metadata: id, type, created, last-used,
  fingerprint/last 4 characters). Consumers receive secrets only inside broker operations (e.g. the
  ACME op reads the DNS token itself).
- **Optional at-rest encryption** with a key from systemd-creds (`LoadCredentialEncrypted=`, TPM2 when
  available) in a later milestone; documented as defense-in-depth, not a substitute for file
  permissions.
- **Export/backup** of app configuration excludes secrets unless the user explicitly includes them and
  supplies an export passphrase.
- **Redaction** is centralised: the audit and log writers accept typed fields only; secret types have no
  string representation.
- **M0 specifics:** password hashes in `users.json` (0600, refused if broader); audit log 0600 JSONL;
  session IDs never stored raw; no other secrets exist yet.
- **Exception: OIDC client secret (Proposed; supervisor design disposition and implementation
  review, 2026-11-06; not an owner acceptance).** The optional OIDC `client_secret`
  (`docs/security/OIDC-SSO.md`) authenticates hc-api itself to the identity provider. It is used only by
  the unprivileged hc-api, for the token exchange. It is not a secret for a host operation run by the
  broker, so the broker-owned rule above does not cover it. Limits on this exception:
  - It lives only in the operator's `oidc.json`. hc-api refuses to load the file if it is a symlink,
    not a regular file, or readable or writable by group or other (the same check as `users.json`).
    Ownership by `homelab-control` is a documented install requirement; it is not checked at load.
  - It is never stored in the state database, never logged, audited or returned by an API, and never
    included in an export. ID, access and refresh tokens and authorization codes are not stored at all.
  - The broker secret store and broker operations are unchanged. No broker op reads this file.
  - A public client (PKCE, no secret) remains supported. Account access still comes only from explicit
    subject → local-account bindings.

  This exception covers no other credential. A host-operation secret (ACME, DNS, backup, Tailscale)
  still follows the broker-owned rule.

## Options Considered
| Option | Pros | Cons |
|---|---|---|
| Root-owned files via broker (chosen) | Works headless on all five distro families; simple to audit | Relies on filesystem permissions |
| Secrets in SQLite rows | Single store | Spec forbids plaintext records; API process would hold them |
| Secret Service / keyring | OS-native on desktops | Needs a session bus/unlocked keyring; absent on servers |
| HashiCorp Vault etc. | Strong | Heavy external dependency; violates local-first simplicity |

## Consequences
- The unprivileged API can never leak a secret it never reads.
- Secret-using features depend on the broker (M1+).
- Revisit when TPM-backed systemd-creds coverage is confirmed across target distros.

## Action Items
1. [x] users.json permission enforcement + atomic writes (M0).
2. [ ] Broker secret store + metadata API (first consumer: ACME/Cloudflare, M10).
3. [x] SQLite driver decision and schema migrations (M2 PR-1a): `modernc.org/sqlite` (ADR 0006 amendment 1). The state database is `/var/lib/homelab-control/state.db`: 0600, in a 0700 directory owned by `homelab-control`, with symlinks refused. It runs in WAL mode with `synchronous=FULL`. Migrations are forward-only and transactional, each existing database is backed up with `VACUUM INTO` before migrating, and a newer schema is refused. It holds sessions (hashes only) and the API audit trail. It holds no secrets: the CSRF tokens it stores are useless without the matching cookie.
