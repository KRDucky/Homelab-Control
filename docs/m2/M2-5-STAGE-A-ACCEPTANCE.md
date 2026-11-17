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

### Acceptance fixture

`frontend/app/src/bays.test.tsx` covers, with synthetic fixtures and a stubbed `fetch`:

- reconciliation: renumbered drive stays put, no auto-transfer, duplicated/partition identity → `mismatch`,
  no observation → `unknown`, non-bay/vacant cells;
- assignment guard: `assignable` accepts only whole disks with an observed assignable tier; `unassignedDrives`
  splits eligible from ineligible;
- UI: keyboard grid selection + assign by persistent identity (CSRF and `persistent_id` body asserted),
  and occupied-bay unassign with no overwrite path (DELETE to the bay route asserted).

## Explicit deferrals (not delivered in this slice)

- keyboard-complete and drag-and-drop **assignment** (grid arrow-key navigation is present for
  accessibility, but the full assign/unassign flow is not keyboard-only or drag-drop);
- JSON **export/import** (including identifier redaction);
- **optimistic revision / concurrency** behavior;
- the **location badge** on other storage views (Stage B);
- a **Playwright + axe** real-browser fixture (the acceptance fixture above is a Vitest/jsdom synthetic
  fixture only).

## Not claimed

No host writes, no storage access, no broker operation, no SES/controller/LED discovery, and no HBA
Viewer reconciliation. No owner acceptance or merge is implied.
