<!--
SPDX-FileCopyrightText: 2026 The Homelab Control Authors
SPDX-License-Identifier: GPL-3.0-or-later
-->

# M2-5 Stage A — manual drive-bay layout: implementation contract (development)

Status: development contract for the Stage A core (`MODULE-ARCHITECTURE.md` §5, roadmap PR M2-5).
This is routine design within the authorized development slice. It is **not** an owner acceptance and
**not** a merge decision. D-12 (HBA Viewer reuse) remains open; nothing here is taken from that project.

## What it is
An application-owned, manually labelled chassis grid stored in hc-api's `state.db`. An administrator records
which bay holds which drive. This is **the administrator's statement, never verified**: every assignment has
`provenance=manual` and `confidence=unverified`. No SES, LED, controller or broker data is used or implied.

There are no host writes and no new broker operation or root code. Discovery is the existing unprivileged
M2-4 collector.

## Data model (migration 3; migrations 1 and 2 unchanged)
- `layout_chassis(id, name, kind, row_count, col_count, created_at, updated_at)`
  - `kind` is one of `server`, `shelf`, `external`, `desktop`.
  - Rows and cols are each between 1 and 24.
- `layout_cells(chassis_id, row_index, col_index, kind, label)`
  - One row per grid cell.
  - `kind` is one of `hdd-3.5`, `ssd-2.5`, `nvme-m2`, `nvme-u2`, `tray`, `empty`, `disabled`, `reserved`.
  - Labels are at most 64 printable characters.
- `layout_assignments(chassis_id, row_index, col_index, identity, provenance, confidence, assigned_by, assigned_at)`
  - `identity` is the M2-4 `persistent_id` (`wwn:`, `nvme-uuid:` or `serial:`). It is never a `/dev`
    name, device number or snapshot node ID.
  - primary key `(chassis_id,row_index,col_index)` and `UNIQUE(identity)`: one drive per bay and one bay per drive.
  - The CHECK constraints fix `provenance` to `manual` and `confidence` to `unverified`.
- **No cascades.** The database refuses to delete a chassis or a cell that still has an assignment.

## Rules
- **Assign** collects a fresh observation, bounded by the existing collector semaphore and timeout, and accepts
  only a `persistent_id` that meets every one of these conditions:
  - it belongs to exactly one node in that observation;
  - that node is a whole disk (`kind=block`);
  - its `identity_status` is `observed`;
  - its tier is `wwn:`, `nvme-uuid:` or `serial:`.

  Ambiguous identities, missing identities, partitions, stacked devices, weaker tiers (`by-id`, vendor-derived
  or unclassified) and non-bay cells are refused.
- **An occupied bay is never overwritten.** A replacement is two explicit steps: unassign, then assign. If a
  drive is missing, its stored assignment is kept and shows as **missing**. A new drive with a different
  identity shows as **unassigned**. Nothing is transferred automatically.
- **Grid edits** that would remove an assigned cell, or turn it into `empty`, `disabled` or `reserved`, are
  refused (409).
- **Reconciliation** happens at read time, in the UI, against the Storage page's current audited observation.
  It joins on `persistent_id` only. The states are:

  | State | Meaning |
  |---|---|
  | **present** | exactly one observed whole disk has the identity |
  | **missing** | no node carries the identity |
  | **mismatch** | the identity is now duplicated or ambiguous, or appears on something that is not a whole disk with observed identity |
  | **vacant** | a bay cell with no assignment |
  | `empty`, `disabled`, `reserved` | cells with no bay |

  A renumbered kernel name does not move an assignment.

## API (all through `Server.handle`; all documented in OpenAPI)
| Route | Scope | Notes |
|---|---|---|
| `GET /storage/layout` | `storage.read` + session CSRF header | Stored layout only, no collection. Released only after an access audit record (`storage.layout.read`, counts only, never serial/WWN) is written; 403 without CSRF before read/audit; 503 when audit is absent or fails. |
| `POST /storage/layout/chassis` | `layout.manage` | Create a chassis. |
| `PUT /storage/layout/chassis/{id}` | `layout.manage` | Replace the name, kind, size and cells. |
| `DELETE /storage/layout/chassis/{id}` | `layout.manage` | Refused while the chassis has assignments. |
| `PUT /storage/layout/chassis/{id}/bays/{row}/{col}/assignment` | `layout.manage` **and** `storage.read` | Body `{persistent_id}`. Collects a fresh observation. |
| `DELETE /storage/layout/chassis/{id}/bays/{row}/{col}/assignment` | `layout.manage` | Remove an assignment. |

`layout.manage` is a new scope:
- it is in `KnownScopes` but not in `DefaultScopes`;
- `storage.read` does not imply it, and it does not imply `storage.read`;
- unsafe methods require the existing Origin check and the per-session CSRF token.

Every write needs an audit logger and runs in this order:
1. A `requested` record is written first. If that write fails, the request is refused with 503 and nothing
   changes.
2. The write runs in one SQLite transaction.
3. An outcome record follows: `applied` or `refused`. Records name the chassis and the bay but never a
   serial or WWN.

Without a state database, the layout routes return 503. There is no in-memory fallback that pretends to persist.

## Deferred (not delivered in this slice)
Each of these is deferred, not claimed as delivered:
- drag-and-drop;
- JSON export and import, including redaction;
- `last_seen`;
- optimistic-concurrency revisions;
- assignment by weaker identity tiers;
- the location badge on other storage views (Stage B);
- SES and controller discovery (Stage C);
- identify LEDs (Stage D);
- a Playwright + axe flow;
- the HBA Viewer reconciliation (D-12).
