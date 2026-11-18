# File Manager — working slice (not accepted)

Status 2026-11-18: this is an isolated **read-only first checkpoint**, following the owner's 2026-11-07
choice after squash-merging PR #42. **This is not M2 close, not M2-6 acceptance, and not a merge.** ADR 0012
(confined-root listing) is **Proposed**. There is no ADR 0008 amendment: this slice adds no broker op.

## What exists

| Route | Scope | What it does |
|---|---|---|
| `GET /api/v1/files/roots` | `files.read` (**not** in `DefaultScopes`) | Identifiers of allow-listed roots (`id`, `label`). Host paths are never returned. `browse_available` is true only when the setting is usable and has at least one valid root |
| `GET /api/v1/files/entries?root=<id>&path=<rel>` | `files.read` | Metadata of directory members inside that jail: name, dir, size, owner (`uid:gid`), mode, mtime, and an omitted-name count. Empty path is the jail root |

Capability `files` (UI path `/files`) is **degraded** when browsing is available (read-only; writes are not in this slice) and **unavailable** when the allow-list is missing, empty or unsafe. Host settings remain core routes; storage routes are owned by the compiled-in `storage` module and can be disabled through `modules.json`.

Setting: root-owned, not group/world-writable `/etc/homelab-control/files.json` (`{"version":1,"roots":[{"id","label","path"}]}`), same trust class as `modules.json`. hc-api flag `-files-config`. Empty, missing, unreadable or unsafe mode fails closed (no browse). Configured paths that are `/`, `/etc`, `/root`, `/proc`, `/sys`, `/home` or a home under `/home`, or that fail canonicalize-inside-jail, are refused. `forbiddenPath` is that lead list only; do not add `/var` or `/tmp` without a new owner/lead decision. Jail directory ownership and mode are not checked (only the setting file's trust class).

### Why this cannot execute or leave the jail

- Served by `hc-api`, which cannot spawn (`guard` tests). No broker op. ADR 0008 forbids a generic path-taking file-read.
- Each `List` opens the jail with `O_RDONLY|O_NOFOLLOW|O_DIRECTORY` and walks `RelPath` components with `openat`+`O_NOFOLLOW`. `os.OpenRoot` is not used: it follows a last-component symlink on the stored path. A last-component symlink is never a directory. Intermediate in-jail relative symlinks are also opened with `O_NOFOLLOW` and refused as not a directory (inside-jail, not a filemin ACL-bypass). `..`, extra slashes and absolute paths are refused before open.
- Identifier-bearing GETs require the session CSRF token before anything is read or audited (same bar as layout/storage). A cookie-only same-site GET is 403 and is not audited. Same-origin browser GET fetch omits Origin; a present Origin (cross-origin GET) still has to match `originAllowed`. Future files GETs must use `filesProtectedGET`.
- Audit records the root id and relative path in its bounded detail for short paths in a conservative ASCII alphabet; other or deep paths use a labelled 128-bit SHA-256 digest so the audit store's 128-byte detail limit is respected. It never records contents or the host jail path. Jail-escape refusals (`..`, extra slashes, symlink-out) are not audited. Directory names that cannot be represented or navigated, or whose metadata cannot be read, are omitted and counted in the response.

### UI (not accepted)

- `/files` is a read-only HCIS FileBrowser port (tree + table + breadcrumbs). Paths, owners and modes are mono. Identifiers and labels only; host paths are never shown.
- New folder, Permissions and Download from the reference mock are hidden and never posted. Preview never downloads content. Capability `files` stays degraded while browsing is available; `CapAvailable` is not a write gate.
- Navigation and metadata use the existing two read routes; the entries response also carries the omitted-name count:
  - The location is in the URL (`/files?root=<id>&path=<rel>`). Back/forward and reload restore it. The values are only identifiers the server validates again; an unknown root falls back to the first allow-listed root, and an escape path (`..`) gets the server refusal as an alert, never a listing.
  - Toolbar: **Up one level** (also Alt+Up, disabled at the root), **Refresh**, **Filter by name** (client-side over the current listing, no request) and an item count.
  - A **Type** column and a **Details** pane: type, size with exact bytes, owner `uid:gid`, mode with octal and setuid/setgid/sticky, modified in local time and ISO UTC. Symlinks are labelled "not followed" and are never opened.
  - Mode parsing (`frontend/app/src/filesModel.ts`) matches Go `os.FileMode.String()` (golden vectors from Go 1.26); an unexpected value is shown raw, not guessed.
  - A newer navigation discards a slower earlier response, so the table always matches the location shown.
  - A screen-reader announcement plus focus on the location heading follow each navigation. The omitted-name notice has `role="status"`, clears when navigation fails, and appears only with a current listing.
- No TUI/CLI file manager code or runtime is reused. A terminal UI behind the web would need a PTY/process in `hc-api` (forbidden by `guard`) and exposes shell-oriented behavior. The reviewed [`lf` source](https://github.com/gokcehan/lf/tree/d4b5b4647b3684bf31ce667adfa1f8e6ecc2265b) uses path-based filesystem calls and starts shell/helper commands; its MIT licence is recorded in its [LICENSE](https://github.com/gokcehan/lf/blob/d4b5b4647b3684bf31ce667adfa1f8e6ecc2265b/LICENSE). The separate File Browser server has writes and is archived; its Apache-2.0 [LICENSE](https://github.com/filebrowser/filebrowser/blob/833d908884d5c801f30f5c098d7977177eb3a36b/LICENSE) and archived [security advisory](https://github.com/filebrowser/filebrowser/security/advisories/GHSA-8q5j-8wcr-8v2v) were checked 2026-11-18. No files or dependencies from these projects were copied. Only interaction conventions (up one level, filter, details pane) are borrowed.

## Non-goals (this checkpoint)

Download, editor, archive extract. mkdir/rename/chmod/upload/delete.
`files.operate` / `files.configure` / `files.destroy`. Bridging Webmin `filemin`. Samba/NFS/WebDAV/iSCSI shares (M7).
Generic broker file op. `modules.json` registration. Production, merge, KVM, M2 close.

`services.files` stays unit-file contents.

## Tests

- Go: jail escape (`..`, extra slashes, symlink out, `O_NOFOLLOW`), missing/empty/unsafe config, oversize directory, 401, 403 without `files.read`, other scopes including `services.files` insufficient, CSRF/Origin on roots and entries, `files.read` in `KnownScopes` not `DefaultScopes`, OpenAPI + guard.
- Vitest: `frontend/app/src/files.test.tsx` (page gated without `files.read`; New folder / Permissions / Download absent and never posted; CSRF on roots and entries; deep link, Up, Alt+Up, back/forward; unknown root; filter without a request; details incl. setuid and symlink note; stale-response race; server refusal shown) and `filesModel.test.ts` (Go mode vectors, paths, URL, filter). Isolated fixtures only; this slice does not browse the OpenBot host or a retained VM.
- Opt-in Go fixture `TestFilesBrowserFixture` (`backend/internal/api/files_browser_test.go`, `HC_FILES_BROWSER_DIR`) plus `tests/e2e/files_browser.py`: labelled synthetic jail only (`readme.txt`, `sub/inner.txt`). Skip unless the runtime dir already exists mode 0700; fatal if euid 0. Not in CI. Not live host browse, not M2 close. The script also checks the URL location, Details (octal, no contents), Up/back/Alt+Up, filter (no request), deep link after a new sign-in, a `..` URL refused, and no write method after navigation.
- Verification status (2026-11-18, isolated implementation branches): implementers report typecheck, lint, format, 238 Vitest tests and build pass; the synthetic fixture run reports 47 checks with axe (WCAG 2.2 AA tags) clean on desktop and 390 px mobile and no mobile overflow. Evidence is copied under `docs/m2/evidence/browser-2026-11-18/files-ui/`. These results are not final integrated-candidate browser evidence; exact-head CI and a fresh fixture run remain pending. The browser evidence used a synthetic jail and headless Chromium on one machine; it is not host or VM acceptance.
- Security review (2026-11-18): the read-only review of PR #118 head `715e5c05` found no actionable findings; the prior low findings around omitted-count presentation and ambiguous audit digests are closed. The reviewer ran no tests, so exact-head CI and the integrated browser fixture remain pending. Earlier static reviews and preserved evidence are under `docs/m2/evidence/browser-2026-11-18/security-review/`. URL-held relative paths and unknown-root history behavior remain informational. Owner acceptance and ADR 0012 remain open.
