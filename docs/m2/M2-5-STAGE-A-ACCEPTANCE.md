<!--
SPDX-FileCopyrightText: 2026 The Homelab Control Authors
SPDX-License-Identifier: GPL-3.0-or-later
-->

# M2-5 Stage A — manual drive-bay layout: UI acceptance status

Status: development/checkpoint documentation, not an owner acceptance and not a merge decision. This
records what Stage A delivers and what it explicitly defers. It takes nothing from the HBA Viewer
project (D-12 remains open).

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
- **Export**: the layout can be exported with assignment identifiers **redacted by default** (tier only,
  e.g. `wwn:[redacted]`), so no serial/WWN value is written to the download.
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
- export: `redactIdentity`/`redactedLayout` mask identities to their tier, and the export button is present;
- import: a valid chassis array is POSTed with CSRF, and oversized/non-JSON files are refused before any
  request. API/state fixtures cover strict schema/size validation, count-only audit, additive atomic
  storage, and assignment discard.

## Remaining gaps (not delivered)

- **optimistic revision / concurrency**: the UI re-syncs after a 409 conflict, but the API has no
  version/If-Match check, so two concurrent editors can still both write.
- the **location badge** on other storage views (Stage B);
- a **Playwright + axe** real-browser fixture (the acceptance fixture above is a Vitest/jsdom synthetic
  fixture only).

## Not claimed

No host writes, no storage access, no broker operation, no SES/controller/LED discovery, and no HBA
Viewer reconciliation. No owner acceptance or merge is implied.
