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
  additively. The redacted export's `assigned` marker is accepted and discarded. Exported IDs, timestamps
  and assignment identities are discarded, so imports only add unassigned geometry and never replace
  existing chassis; state refreshes on success and errors surface. Chassis create/update allow up to 1 MiB
  for a full 24×24 labeled grid; other API requests retain the 64 KiB cap.

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

The implementers report 238 frontend tests and the full Go suite passing on their isolated branches,
including manage-only identity redaction, exact redacted-export import shape, accepting a valid import
larger than 64 KiB, rejecting imports over 8 MiB, accepting large chassis create/update requests, and
ignoring non-custom drag payloads. These reports are not yet verified on the final integrated candidate;
exact-head CI must rerun them.

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

## Stage B — location badge and read-only hardware discovery (development, not accepted)

Status: development checkpoint on branch `feat/m25-stage-b`. **Not owner-accepted**, not a merge decision
and not M2 close. The Stage B development authorization (2026-11-20) was relayed by the coordinating agent;
this branch does not add an `OWNER-DECISIONS.md` entry for it. Out of scope and not delivered: LED locate,
SES blink or any enclosure control, partitioning, automatic assignment on drive replacement, `files.write`.

Scope note: the Stage A contract's deferral list puts "SES and controller discovery" under Stage C. As
requested for this Stage B slice, only its **read-only** discovery part is delivered here; enclosure
control and identify LEDs (Stage C/D actions) stay undelivered.

### Delivered

- **Location badge.** The Storage inventory ("Observed storage resources"), the enclosure slot tables and
  the selected-resource details show `<chassis name> · <bay label>` (grid position when the label is blank)
  for a whole disk **only** when its bay reconciles as `present`. Missing, mismatch, unknown and unassigned
  drives get no bay. The badge is labelled "Manual, unverified": the administrator's Stage A assignment is
  still the only source of a chassis/bay location. It joins on `persistent_id`, so a renumbered kernel name
  does not move it. No extra request: the Storage page reuses the layout the Drives & bays panel reads.
- **Read-only controller and enclosure discovery** (`backend/internal/storageinfo/hardware.go`), part of the
  existing unprivileged hc-api collector (`os.Root` over `/`, fixed sysfs paths, no request parameters):
  - controllers from `/sys/class/scsi_host` and `/sys/class/nvme`: kind, driver, PCI function, SAS
    address, board model and firmware. The ID is `pci:<address>` when known, otherwise snapshot-local;
  - per-disk links: controller plus port evidence (`ataN`, `phy N` from `/sys/class/sas_device`, `nvmeN`);
  - SES enclosures from `/sys/class/enclosure` (kernel `ses` driver): logical ID, vendor/model, elements
    with slot number and reported status.
- **Enclosure slot → drive identity** only when unambiguous: the element's attached device resolves to
  exactly one observed whole disk with a `wwn:`/`nvme-uuid:`/`serial:` identity (device number, and
  diskseq when both sides report it, must match this observation), the enclosure reports a non-zero,
  unique logical ID, and the identity is in no other slot. Otherwise the slot says why: `empty`,
  `not-a-disk`, `changed`, `unidentified`, `ambiguous` (for example a dual-path enclosure), or
  `no-enclosure-id`. An enclosure slot is firmware-numbered **evidence**; it is never called a bay, never
  creates or changes an assignment, and a present bay only displays it next to the manual statement.
- No new route, scope, broker op or root code. Nothing is executed (hc-api still cannot spawn), no `/dev`
  node is opened, and no sysfs attribute is written (`locate`/`fault` are never opened). `GET /storage`
  gains an optional `graph.hardware` object (OpenAPI `StorageHardware`).
- Source: the controller-link-as-evidence concept follows the owner-supplied HBA Viewer
  (`KRDucky/hbaviewer-cockpit`, MIT, reviewed at `8061595c`; see D-12). No code was copied (that project
  uses lsblk, storcli/lsiutil and a privileged Python helper, and does not read `/sys/class/enclosure`), so
  no third-party notice is added.

### Tests run (2026-11-20, local, branch `feat/m25-stage-b`)

- `go test -count=1 ./internal/storageinfo/` and `go test -count=1 ./...` (backend): pass. New
  `hardware_test.go` (synthetic sysfs trees with real-shaped symlinks): controllers/links/enclosure,
  two disks in two slots (each keeps its own enclosure fields), kernel-name renumbering, dual-path
  ambiguity, six refusal cases, logical-ID zero padding, no controller
  classes, escaping symlink, `locate` untouched.
- Frontend: `npm run typecheck`, `lint`, `format:check`, `npx vitest run` (24 files, 248 tests, including 10
  in `locationBadge.test.tsx`), `npm run build`: pass.
- Browser (opt-in `TestStorageBrowserFixture`, synthetic HBA + SES enclosure, headless Chromium + axe):
  `tests/e2e/storage_browser.py` 27/27 and `tests/e2e/layout_browser.py` 23/23 passed; axe (WCAG 2.2 AA
  tags) reported no violations on desktop, 390×844 mobile, and with a badge shown.
- `make check`: exit 0 (shellcheck skipped locally: not installed).

### Not verified

- **No live hardware.** No SAS HBA, SES enclosure or real controller was read; discovery is verified on
  synthetic sysfs fixtures only. sysfs attribute formats were taken from the kernel's documented
  `scsi_host`/`nvme`/`enclosure`/`sas_device` classes, not observed on a device. No VM or container run.
- Vendor-specific element naming/numbering, multipath beyond the dual-path enclosure case, RAID-mode
  controllers that hide disks behind virtual drives, and USB/Thunderbolt enclosures are untested.
- Exact-head CI has not run on this branch. No owner acceptance or security review.
