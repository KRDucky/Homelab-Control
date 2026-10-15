# ADR-0004: Configuration, state database and secrets storage

**Status:** Proposed (M0 implements only the users file and audit log)
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
3. [ ] SQLite driver decision and schema migrations (M2).
