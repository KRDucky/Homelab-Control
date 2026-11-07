# File manager — working slice (not accepted)

Status 2026-11-07: owner `ask_ui` chose the file manager after squash-merging PR #42. This is an isolated
**read-only first checkpoint**. **This is not M2 close, not M2-6 acceptance, and not a merge.** ADR 0012
(confined-root listing) is **Proposed**. There is no ADR 0008 amendment: this slice adds no broker op.

## What exists

| Route | Scope | What it does |
|---|---|---|
| `GET /api/v1/files/roots` | `files.read` (**not** in `DefaultScopes`) | Identifiers of allow-listed roots (`id`, `label`). Host paths are never returned. `browse_available` is true only when the setting is usable and has at least one valid root |
| `GET /api/v1/files/entries?root=<id>&path=<rel>` | `files.read` | Metadata of directory members inside that jail: name, dir, size, owner (`uid:gid`), mode, mtime. Empty path is the jail root |

Capability `files` (UI path `/files`) is **degraded** when browsing is available (read-only; writes are not in this slice) and **unavailable** when the allow-list is missing, empty or unsafe. Core routes, like storage and host settings: **not** registered in `modules.json` (`TestRegistryIsClosed` stays `{services}` only).

Setting: root-owned, not group/world-writable `/etc/homelab-control/files.json` (`{"version":1,"roots":[{"id","label","path"}]}`), same trust class as `modules.json`. hc-api flag `-files-config`. Empty, missing, unreadable or unsafe mode fails closed (no browse). Configured paths that are `/`, `/etc`, `/root`, `/proc`, `/sys`, `/home` or a home under `/home`, or that fail canonicalize-inside-jail, are refused.

### Why this cannot execute or leave the jail

- Served by `hc-api`, which cannot spawn (`guard` tests). No broker op. ADR 0008 forbids a generic path-taking file-read.
- Listing uses in-process `os.Root` plus `O_NOFOLLOW` (same confinement idea as timezone lookup). Symlinks are not followed. `..`, extra slashes and absolute paths are refused before open.
- Identifier-bearing GETs require Origin and the session CSRF token (same bar as layout/storage). A cookie-only same-site GET is 403 and is not audited.
- Audit records root id and relative path only, never contents, never the host jail path.

## Non-goals (this checkpoint)

Frontend/FileBrowser (later, stacked). Download, editor, archive extract. mkdir/rename/chmod/upload/delete.
`files.operate` / `files.configure` / `files.destroy`. Bridging Webmin `filemin`. Samba/NFS/WebDAV/iSCSI shares (M7).
Generic broker file op. `modules.json` registration. Production, merge, KVM, M2 close.

`services.files` stays unit-file contents.

## Tests

- Go: jail escape (`..`, extra slashes, symlink out, `O_NOFOLLOW`), missing/empty/unsafe config, oversize directory, 401, 403 without `files.read`, other scopes including `services.files` insufficient, CSRF/Origin on roots and entries, `files.read` in `KnownScopes` not `DefaultScopes`, OpenAPI + guard.
- Vitest/Playwright/axe: later (`hc-ui`). Isolated fixtures only; this slice does not browse the OpenBot host or a retained VM.
