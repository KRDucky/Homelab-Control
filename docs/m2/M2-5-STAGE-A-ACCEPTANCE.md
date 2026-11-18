<!--
SPDX-FileCopyrightText: 2026 The Homelab Control Authors
SPDX-License-Identifier: GPL-3.0-or-later
-->

# M2-5 Stage A — manual drive-bay layout: UI acceptance status

Status: development/checkpoint documentation, not an owner acceptance and not a merge decision. This
records what Stage A delivers and what it explicitly defers. D-12 source review is recorded in
`OWNER-DECISIONS.md`: Stage A reuses the HBA Viewer bay-layout concept, with no upstream code copied.

## Delivered (Stage A core)

The manual, application-owned drive-bay layout (`docs/m2/M2-5-STAGE-A-CONTRACT.md`) is implemented and
fixture-tested end to end:

- **Grid**: create, edit (name/kind/rows/cols), and delete chassis; per-cell kind (`hdd-3.5`, `ssd-2.5`,
  `nvme-m2`, `nvme-u2`, `tray`, `empty`, `disabled`, `reserved`) and printable label.
- **Assignments**: manual and unverified only (`provenance=manual`, `confidence=unverified`), keyed by a
  stable persistent identity (`wwn:`, `nvme-uuid:` or `serial:`), never a kernel name, device number or
  snapshot node ID.
- **Reconciliation** (read-time join on `persistent_id` only): `present`, `missing`, `mismatch`,
  `vacant`, `unknown`, `empty`, `disabled`, `reserved`. A renumbered kernel name never moves an
  assignment; a missing drive keeps its assignment; a replacement is never assigned automatically.
- **Guards**: an occupied bay exposes unassign only (never a second assign), so nothing is overwritten;
  grid edits that would drop or de-bay an assigned cell are refused by the API; assignment accepts only
  one observed whole disk with an unambiguous assignable-tier identity.
- **Assignment input**: a drive is assigned to an eligible vacant bay by **drag-and-drop** or by
  **keyboard** (arrow-key navigation selects the bay, then the select/button assign flow); an occupied
  bay is never a drop target and never offers assign.
- **Optimistic concurrency**: update/assign bodies carry the chassis `version` (the per-chassis
  compare-and-swap token) and delete/unassign send `?version=`; a stale version is a 409
  `layout_conflict`, which drops the snapshot and re-syncs the layout.
- **Export**: the layout can be exported without assignment identity or assignment metadata; it includes
  geometry, labels and an `assigned` boolean only. Names and labels are exported as entered and are not
  anonymized.
- **Import**: a JSON file can be imported (POST `/api/v1/storage/layout/import`, scope `layout.manage`):
  the file is size-capped at 8 MiB before read, parsed and validated for a `chassis` array, and imported
  additively. Exported IDs, timestamps and assignment identities are discarded, so imports only add
  unassigned geometry and never replace existing chassis; state refreshes on success and errors surface.

### Acceptance fixture

`frontend/app/src/bays.test.tsx` covers, with synthetic fixtures and a stubbed `fetch`:

- reconciliation: renumbered drive stays put, no auto-transfer, duplicated/partition identity → `mismatch`,
  no observation → `unknown`, non-bay/vacant cells;
- assignment guard: `assignable` accepts only whole disks with an observed assignable tier; `unassignedDrives`
  splits eligible from ineligible;
- UI: keyboard grid selection + assign by persistent identity (CSRF and `persistent_id` body asserted),
  drag-and-drop onto a vacant bay (PUT to the bay route asserted), occupied-bay unassign with no overwrite
  path, and 409-conflict re-sync;
- export: `redactedLayout` omits identities and assignment metadata and exports only an `assigned` boolean;
- import: a valid chassis array is POSTed with CSRF, and oversized/non-JSON files are refused before any
  request. API/state fixtures cover strict schema/size validation, count-only audit, additive atomic
  storage, and assignment discard.

The implementers report 234 frontend tests and the full Go suite passing on their isolated branches,
including manage-only identity redaction, accepting a valid import larger than 64 KiB, rejecting imports
over 8 MiB, and ignoring non-custom drag payloads. These reports are not yet verified on the integrated
candidate; CI must rerun them.

## Remaining gaps (not delivered)

- the **location badge** on other storage views (Stage B);
- hardware/controller discovery, LED locate, and automatic physical bay assignment; the source
  project’s privileged runtime is not imported.

The Playwright + axe fixture is now present and was run against the current local closeout candidate:
18 checks passed, with zero axe violations at desktop and 390×844, and no mobile overflow. The browser
run used labelled synthetic storage identities and the actual fixture API/SQLite. It is recorded in
`docs/m2/evidence/browser-2026-11-18/layout/`. The browser test was corrected to send the required
optimistic-concurrency `version` for assignment and delete. D-12 source identification and concept
reconciliation are recorded; this does not constitute owner acceptance.

## Not claimed

No host writes, no storage access, no broker operation, no SES/controller/LED discovery, and no HBA
Viewer reconciliation. No owner acceptance or merge is implied.
