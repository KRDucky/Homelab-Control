# File Manager UI slice: security review (read-only)

Reviewed in `/data/OpenBot/Agents/agent-81fcbee3-bf39-4580-84a8-c51376cc0fdb/work/m2-acceptance-integrated`, HEAD `993c3758` plus the staged patch (9 files). Product files read in full: `frontend/app/src/pages/Files.tsx`, `filesModel.ts`, the `components.css` diff. Backend read in full: `backend/internal/filesinfo/filesinfo.go`, `backend/internal/api/files.go`, and the route registration, `auditLog`, `audit.sanitize` and security headers in `server.go`. Also read: `api.ts` file calls, `DataTable.tsx`, the docs diff and the File manager section of `docs/security/THREAT-MODEL.md`.
Nothing was built, run or modified. I did not read `files.test.tsx`, `filesModel.test.ts` or `tests/e2e/files_browser.py` line by line.

## Summary
0 Critical, 0 High, 0 Medium, 4 Low (confirmed) and 3 informational. No jail escape, XSS, symlink-following, content-read, new-route or new-dependency issue found in the delta.

## Findings

1. **Low: filenames render with no bidi or control-character handling.** `Files.tsx:119, 416, 419, 461` (also 72 and 342-346).
   - Trigger: a name containing U+202E, zero-width or lookalike characters, or a leading space, in a listed directory. Whoever can create files in a jail can do this.
   - Impact: the administrator can misread a name (`photo‮gpj.exe`) in a view they use to judge what is on disk. This is spoofing only. React escapes the text and no HTML is injected.
   - Fix: wrap names in `<bdi>`, or set `unicode-bidi: isolate` on `.mono`. Render control and format characters as visible escapes, and show the raw escaped name in Details.

2. **Low: non-UTF-8 names and names with `\` give wrong or colliding rows.** `Files.tsx:289, 393, 406`, `filesinfo.go:343-350`, `RelPath` at `filesinfo.go:281`.
   - Trigger: a name with invalid UTF-8 bytes. Go's JSON encoder turns each invalid byte into U+FFFD, so two different on-disk names can arrive as one string. The same happens in the `dirs` tree map (`Files.tsx:209`) and in `key={name}`.
   - Impact: duplicate React keys, and `entries.find((e) => e.name === selected)` can show another entry's metadata in Details. Clicking such a folder sends the mangled name, and the server answers 404, so no access is gained. A folder whose name contains `\` is rejected by `RelPath` (400) and cannot be opened.
   - Fix (server): skip names that are not valid UTF-8, or flag them as unnavigable. In the UI, make the table's `rowKey` the array index plus the name, and look the selection up by index.

3. **Low (existing backend): directory listings can look empty when they are not.** `filesinfo.go:343-353`.
   - Trigger: the service account can read a directory but not search it (mode `r--`). Every `Fstatat` returns EACCES, `List` drops every entry silently and returns `[]` with 200.
   - Impact: the UI shows "This directory is empty." for a directory that has content. This is a correctness and misleading-state issue, not an access gain.
   - Fix: count skipped entries and return them, or return `ErrPermission` when all stats fail.

4. **Low (existing backend): the audit record for a listing is truncated.** `audit.go:209` with `files.go:81`.
   - Trigger: any listing whose `root=<id> path=<rel>` text is longer than 128 bytes. `sanitize` cuts at 128 bytes, which can also split a multi-byte character.
   - Impact: the audit shows a shortened path for deep folders. This is a visibility gap. It is not an injection: control characters are removed and the record is JSON-encoded, so log injection is not possible.
   - Fix: raise the limit for this event or store the path in `Event.Data`.

## Informational
- **I1. Threat model not updated for URL-held paths.** The location is now in `/files?root=&path=`, so relative paths enter browser history, bookmarks and reload. The server sees them in the query string. The existing controls cover most of this: `Referrer-Policy: no-referrer` and a strict CSP are set at `server.go:410-416` (I did not confirm that the static SPA route goes through the same code), and the audit stores only `root` and `path`. Add one row to `THREAT-MODEL.md` for this exposure.
- **I2. Back or forward to an unknown root.** `Files.tsx:239-243` with history `'none'` shows the first root while the address bar keeps the unknown root. After a failed navigation the old location stays on screen with the table hidden (`Files.tsx:219-224`). Neither gives access. The docs say the table always matches the location shown, which is true only for successful loads.
- **I3. Details expose `uid:gid`, setuid, exact size and mtime.** These are the fields the API already returns to `files.read` holders. The threat model's "metadata only" row covers them.

## Checked and fine
- **XSS:** entry names, owner, mode and mtime are only rendered as React text nodes. The one attribute that uses a name is `aria-label` (`Files.tsx:461`), and that is safe. No `dangerouslySetInnerHTML`, `href` or `window.open` takes entry data (grep across `frontend/app/src`).
- **URL handling:** `pushState` and `replaceState` build `pathname + URLSearchParams(root, path)` with the server-returned `root` and cleaned path (`Files.tsx:214`, `filesModel.ts:101`). There is no open redirect and no navigation to an attacker URL. `readLocation` values are only forwarded to the server.
- **Path confinement:** `RelPath` rejects `..` anywhere, absolute paths, `//`, `\`, NUL, `.` components and non-canonical forms. `List` reopens the jail with `O_NOFOLLOW|O_DIRECTORY` and walks with `openat` + `O_NOFOLLOW` per component. `statEntry` uses `AT_SYMLINK_NOFOLLOW`. Symlinks come back with `Dir=false`, so the UI draws no button for them and never requests them. `..` typed into the URL gets 403 from the server (`filesListError`), not a listing.
- **Leaks:** error mapping returns fixed messages and no host path (`files.go:92-110`). Roots are id and label only. `mapOpenErr` can return a raw `*PathError` containing the jail path, but the handler's default branch turns it into a generic 400.
- **Auth boundary:** both GET routes are registered with `ScopeFilesRead`. `filesProtectedGET` checks the CSRF header, then Origin, then audit availability, before reading anything.
- **Scope of the delta:** no new route, process execution, content read or write, or dependency. The UI sends only the two existing GETs.
- **Size limits:** listings are capped at 4096 entries and refused above that. `DataTable` paginates at 100 rows. The filter input has `maxLength=255`.
- **Stale responses:** the generation counter (`Files.tsx:190-200, 220`) stops a slow response replacing a newer location.

## Checks run / Not run / Out of scope
- **Run:** full manual read of the files listed above, grep for HTML sinks and header settings.
- **Not run:** typecheck, Vitest, the e2e fixture, `govulncheck`, any browser, any guest or host operation. The implementer's reported passes (47 checks, axe) are not verified by me.
- **Not verified:** that `handle()` enforces scope for these routes beyond the registrations at `server.go:252-253`, that the security headers cover the SPA HTML, and the Go 1.26 mode vectors in `filesModel.test.ts`.
- **Out of scope:** the broker, other pages, and deployment and `files.json` contents.
