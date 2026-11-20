# ADR-0012: Confined-root file listing in hc-api

**Status:** **Proposed** 2026-11-07 (file-manager first checkpoint). Not accepted. Not authorization for
file writes, content download, or a broker file operation.
**Date:** 2026-11-07
**Related:** ADR 0001 (hc-api does not spawn), ADR 0008 (no generic path-taking file-read), owner
2026-11-07 choice of the file manager after PR #42, `docs/m2/FILE-MANAGER-SKELETON.md`.

## Context

The M2 roadmap lists a separately authorized file manager as mandatory before M3. Webmin `filemin` is
never bridged (arbitrary command execution / root file access). A broker op that takes a caller path
and reads as root would be that design.

## Decision

1. **Unprivileged listing only**, in `hc-api`, of metadata (name, dir, size, owner, mode, mtime) under
   administrator allow-listed directories. No content, no editor, no archive extract, no mkdir / rename /
   chmod / upload / delete in this checkpoint.
2. **Confined roots** come from root-owned, not group/world-writable `/etc/homelab-control/files.json`
   (same trust class as `modules.json`). Missing, empty, unreadable or unsafe mode fails closed. `/`,
   `/etc`, `/root`, `/proc`, `/sys` and other users' homes are refused.
3. **`openat` + `O_NOFOLLOW`.** Canonicalize the jail at load. Each list reopens it with
   `O_RDONLY|O_NOFOLLOW|O_DIRECTORY` and walks relative components with `openat`+`O_NOFOLLOW` so a
   post-load last-component symlink swap is refused. `hc-api` still does not spawn. There is **no**
   new broker operation and **no** ADR 0008 amendment.
4. **Scope `files.read`**, in `KnownScopes`, not `DefaultScopes`, implied by no other scope including
   `services.files`. Core routes: not registered in `modules.json`.
5. **CSRF + Origin** on identifier-bearing GETs. Audit records root id and relative path only.

## Consequences

- A later write slice needs its own owner authorization, ADR 0011 plan/confirm/apply, a separate
  `files.operate` / `files.configure` / `files.destroy` scope, and reviewed jailed verbs — not a generic
  path-taking root file op.
- The read-only FileBrowser port is on `/files` as a checkpoint (not accepted); writes, download, and content remain out.

## Amendment 2026-11-20 (owner-authorized write slice)

**Status:** Proposed (amendment only). The owner authorized this confined write slice on 2026-11-20
for development and isolated testing; acceptance, merge and production activation remain separate
owner decisions. This amendment does **not** mark ADR 0012 as a whole Accepted.

**Date:** 2026-11-20.

### Decision (this amendment)

1. **In-process jailed verbs only**, still in `hc-api`, still no spawn, still no broker file operation
   (ADR 0008 unchanged). Content read, download, content write/replace, upload, create, rename and
   delete run as `hc-api`'s own uid against the confined jail; a root not writable by `hc-api` fails
   closed with a permission error. A FIFO/device/socket target is never opened for writing (a FIFO
   write would block).
2. **Scopes:** `files.read` gains content read and download. New `files.write` (upload, content write,
   create, rename) and `files.destroy` (delete). Both in `KnownScopes`, neither in `DefaultScopes`,
   neither implied by any other scope, and `files.read` does not imply either. The original
   `files.operate` / `files.configure` naming is superseded by `files.write`.
3. **Size caps:** content read/download/write are capped at 4 MiB (oversize reads are truncated and
   flagged; oversize writes/upload are 413); a multipart upload is capped at 8 MiB. All caps are
   enforced on the request body and again in the verbs.
4. **Same jail invariants as listing:** `RelPath` rejects `..`, extra slashes, absolute paths, NUL and
   backslashes; every open uses `openat`+`O_NOFOLLOW`; `renameat`/`unlinkat` operate on the last
   component itself so a symlink is renamed or removed, never followed out of the jail. `O_CREAT|O_EXCL`
   never overwrites; upload filenames and create/rename names are validated as single safe segments.
5. **Delete uses plan → confirm (ADR 0011 pattern), not a single POST:** `POST /files/delete/plan`
   returns a single-use, expiring (120 s) token bound to actor, session, root, path and the observed
   target identity (kind, size, mtime). `POST /files/delete/apply` consumes the token, requires a
   password re-entry (destructive class; a missing step-up does not consume the token), revalidates the
   target and refuses `delete_conflict` on any change. Because there is no broker file op, the token is
   minted in `hc-api` memory (HMAC-SHA256, bounded outstanding set), not by the broker.
6. **CSRF + Origin** on every write verb and on content/download (identifier-bearing GETs keep the
   existing `filesProtectedGET` bar). Audit records the root id and relative path (hashed when needed)
   only; never file contents, passwords, or the host jail path.

### Consequences

- `files.read` is no longer listing-only: it authorizes returning file bytes (capped). `services.files`
  remains unit-file contents and is unrelated.
- The `/files` page now shows read/write controls gated by the session scopes; chmod/setuid and share
  setup remain out of scope.
- OpenAPI, the OpenAPI↔route guard contract, and the `/files` Vitest coverage were extended to the new
  verbs.
