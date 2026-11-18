# M2-5 Stage A drive-bay layout: security review (read-only)

Candidate: `/data/OpenBot/Agents/agent-81fcbee3-bf39-4580-84a8-c51376cc0fdb/work/m2-acceptance-integrated` (HEAD `993c3758` plus staged patch). `backend/internal/api/storage_layout.go` does not exist. The layout code is in `backend/internal/api/layout.go`, `backend/internal/state/layout.go` (migrations 3 and 4 in `state.go`), `frontend/app/src/pages/DriveBays.tsx`, `bayModel.ts` and the layout functions in `api.ts`. There is no broker path: layout.go imports no broker package, and the contract says no host write exists. I read these files in full, plus `server.go` (`handleRoute`, `ServeHTTP`), `stepup.go` (`decodeStrict`), the scope definitions in `auth/users.go` and the contract `docs/m2/M2-5-STAGE-A-CONTRACT.md`. Nothing was built or run.

Summary: 0 Critical, 0 High, 1 Medium, 3 Low, 3 informational.

## Findings

1. **Medium: every successful layout write returns the full layout, including stored serials/WWNs, to a caller who has only `layout.manage`.** `layout.go:70-77` (`writeLayout`) called from `layoutWrite` at `layout.go:171`.
   - Trigger: a session with `layout.manage` but without `storage.read` sends a valid write that succeeds. For example, `POST /storage/layout/chassis` (create) returns `List()`: all chassis, cells and `assignment.identity` values.
   - Impact: the contract (lines 67 and 75-78) and `layout.go:246-247` say neither scope implies the other, and that identities are released only after a `storage.layout.read` audit record. Here a `layout.manage` holder reads every stored identity, and the only audit trail is the write's own requested/applied records, which do not mark an identity read. `TestLayoutScopesCSRFAndOrigin` covers only assign without `storage.read` (layout_test.go:98), not the other writes.
   - Fix: in `writeLayout`, return identities only when `sess.HasScope(auth.ScopeStorageRead)`, otherwise blank `assignment.identity` or return only `{ok, version}`. Add a test that a manage-only session gets no identity from create, update, delete, unassign or import.

2. **Low: the "redacted" export still carries the administrator account name and timestamps.** `bayModel.ts:111-112` spreads `...cell.assignment` and then overrides only `identity`, so `assigned_by`, `assigned_at`, `provenance` and `confidence` are exported. Chassis names and cell labels (free text, up to 64 printable characters) are also exported as typed, and nothing warns that a label could contain a serial. The identity text is correctly reduced to `tier:[redacted]`.
   - Fix: export only `kind` and `label` per cell, with `assigned: true`, or drop `assigned_by`/`assigned_at`. Show a short notice that names and labels are exported as written.

3. **Low: the import limit is 64 KiB in practice, not the documented 8 MiB.** `layout.go:388-397` wraps the body with an 8 MiB `MaxBytesReader`, but `ServeHTTP` already wrapped every request body at 64 KiB (`server.go:423`). The inner limit still applies, so the larger one is never reached (from Go's reader semantics; not run). The UI export is pretty-printed with two-space indent, so one 24×24 chassis is probably more than 64 KiB, and a file exported from the UI is refused with 413 `body_too_large`. `TestLayoutImportOversizeRefused` (layout_test.go:323) passes either way, and the contract (line 69), the UI constant `MAX_IMPORT_BYTES` (`DriveBays.tsx:37`, 120) and the comment at `layout.go:385-388` all state 8 MiB. This is a correctness issue that fails safe.
   - Fix: either skip the global 64 KiB wrapper for this one path in `ServeHTTP`, or change the contract, the UI message and the comment to 64 KiB and test a valid import near the limit.

4. **Low: a drop accepts any `text/plain` and writes at once.** `DriveBays.tsx:282-291` takes `e.dataTransfer.getData('text/plain')` and calls `assignBay` with no confirmation and without checking `busy`. Text dragged from elsewhere (for example a selected `serial:…` value in the Identifiers list at lines 425-430) triggers an assignment.
   - Impact: the server still refuses anything that is not exactly one observed whole disk (`assignableNow`, layout.go:301-324), so only an accidental assignment of a real drive is possible. It is manual and reversible with Unassign.
   - Fix: use a custom drag type such as `application/x-hc-drive` set in `onDragStart` (line 557), ignore other drops, and ignore drops while `busy`.

## Informational
- **I1. Partial PUT resets unlisted cells.** `ChassisSpec.cells()` (`state/layout.go:137-153`) fills missing positions with `hdd-3.5` and an empty label, and `Update` applies that (lines 352, 377). The UI always sends a full grid. An API caller that sends a partial list silently loses the kinds and labels of the cells it left out. Assigned bays keep their assignment (the bay-kind check passes for `hdd-3.5`).
- **I2. `List` reads in two queries without one transaction** (`state/layout.go:163-210`). A write between them gives a torn view. It fails safe: the version comes from the first query, so a later mutation with it returns 409.
- **I3. Import is not idempotent.** A retried import duplicates chassis, and names are not unique. The cap of 32 chassis (`state/layout.go:278`) bounds this, and delete recovers it.

## Checked and fine
- **CSRF and Origin:** every write route is registered with `s.handle`, and `handleRoute` rejects unsafe methods without an allowed Origin and without a valid `X-CSRF-Token` before scope or handler code (`server.go:308-353`). `GET /storage/layout` checks the CSRF token inside `handleLayout` before any read or audit (`layout.go:86-89`).
- **Scopes:** `layout.manage` is in `KnownScopes` and not in `DefaultScopes`. The assign route also needs `storage.read` (`layout.go:248-252`, tested at layout_test.go:98). Scope is re-evaluated per request against the user's current scopes.
- **Identity validation:** two layers. `ValidAssignableIdentity` restricts the tier to `wwn:`, `nvme-uuid:` and `serial:` with a non-empty value, at most 512 printable characters. `assignableNow` then requires a fresh observation in which exactly one node carries the identity, it is a whole `block` node, its identity status is `observed`, its `persistent_id` equals the identity, and no matching identifier is ambiguous. Partitions, by-id tiers, kernel names and device numbers are refused.
- **Reassignment:** an occupied bay, a non-bay cell, or an identity already assigned elsewhere returns a conflict inside the transaction (`state/layout.go:438-447`), and the table has `UNIQUE(identity)` and a per-bay primary key plus a foreign key from assignments to cells. Foreign keys are on (`state.go:216`). Update refuses to remove or reclassify an assigned cell. Delete refuses while assignments exist. There are no cascades.
- **Concurrency:** all stores use immediate transactions with a 5 s busy timeout and one writer connection. Assign, unassign, update and delete are compare-and-swap on `version` (`casBump`, and the version-guarded `UPDATE` and `DELETE`). A stale version returns 409 `layout_conflict`, and the UI drops its snapshot and reloads on 409 (`DriveBays.tsx:106-109`).
- **Audit:** a `requested` record is written first and the write is refused with 503 when that fails (`layout.go:147-150`). Details name chassis and bay and the identity tier only, never a serial or WWN. Layout reads are audited before release with counts only.
- **Import:** `decodeStrict` rejects unknown fields and trailing data. `id`, timestamps and each cell's `assignment` are accepted as raw JSON and discarded (`layout.go:351-383`), so imported geometry is always unassigned. All chassis are validated first and inserted in one transaction, and the total cap is enforced inside it.
- **Errors:** responses use fixed messages and echo no input.
- **XSS:** names, labels and identities render as React text. The only attribute use is `aria-label`.

## Checks run / Not run / Out of scope
- **Run:** full read of the files above and a grep for broker or host calls in the layout code.
- **Not run:** build, tests, any browser or live host. I did not read layout_test.go, state/layout_test.go, bays.test.tsx or the unseen `Storage.tsx` and `storageModel.ts` line by line.
- **Not verified:** the Go `MaxBytesReader` nesting behaviour in finding 3 (reasoned from the standard library, not executed), the OpenAPI document, and whether the module gate (`modules.ForRoute`) enables these routes in the production module set.
- **Out of scope:** the owner design acceptance (the contract says it is not an acceptance) and any storage write to the host. I saw none in the layout path.
