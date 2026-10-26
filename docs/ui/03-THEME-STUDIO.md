# 3. Theme Studio

Implemented as a browser-local frontend under **Appearance → Themes & Theme Studio** (`/appearance/themes`), authorized 2026-10-25. See [handoff](11-IMPLEMENTED-UI-AND-TDK.md) and [TDK](../../sdk/theme/README.md).

The library shows saved themes and their metadata, with Edit, Preview, Export and Remove. Create from any built-in base or import a `.hltheme`/JSON file. Import is parsed and validated before it reaches an isolated, labelled example preview. It never executes a host operation.

The editor offers name/id/version/license/base, grouped hex fields and colour pickers, per-token and all-colour reset, Duplicate, Undo/Redo (200 edits), explicit Advanced JSON validation and desktop/tablet/phone preview widths. Fonts, density, motion and layout stay in Appearance. Issue entries open and focus the affected colour control. The preview includes real shared components with labelled example data and remains keyboard-operable for inspection.

Save stores a draft in this browser, with at most 20 themes and 10 prior saved versions per theme. Restoring a version loads it into the draft. Saving an existing id increments the patch version unless the author supplied a higher version. Local-storage failure reports an error and preserves the previous atomic record. Export is the single `.hltheme` document, with no archive or preview image payload.

Contrast issues may be saved in drafts, but they block application preview and activation. **Preview on application** starts a 15-second trial. Keep persists activation; Revert, Escape, timeout and leaving Studio restore the prior appearance. The confirmation uses fixed system colours, independent of the trial theme.

This implementation needs no `appearance.manage` server scope. Per-installation themes, server sync, audit events for server storage and server validation remain future work with the settings API. No browser-local preference is represented as server-wide state. An automated contrast pass is not a full accessibility audit.
