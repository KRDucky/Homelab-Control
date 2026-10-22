# DriveBayGrid

Physical Drive Layout Designer: saved layouts per host, irregular grids, HDD/SSD/NVMe bays, reserved slots, custom labels, drag-and-drop or keyboard assignment, list view, zoom, import/export.

**Status: Proposed (HCIS).** New in the HCIS specification; a reference design, not in the app.

**Implementation: awaiting authorization.** Anything here that writes to the host belongs to M2+ and needs a separately approved PR. Previews never execute operations.

Source: `CLAUDE.md (stable drive identity, location confidence)`

## When to use

Drives are identified by WWN/serial/NVMe EUI and shown with controller/HBA port, health, temperature and pool. Missing and replaced drives are flagged.

## Rules

- Keyboard: Hold a drive, then press a bay; or use Assign drive in the bay panel.
- Narrow screens: scroll and zoom the chassis, or switch to the accessible List view.
- LED locate is shown but disabled until guarded enclosure control is authorized.

Props: `DriveBayGridProps` in `reference/index.d.ts`. Preview data is fictional and labelled “Example data”.
