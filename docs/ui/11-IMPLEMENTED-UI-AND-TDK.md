# UI foundation and Theme Development Kit — implementation handoff

Owner direction, 2026-10-25: implement the remaining UI so backend/module work can proceed, and provide a Theme Development Kit for users. This work extends merged HCIS Steps 1–3 (PR #19 merge `16650cb6`). It authorizes frontend work; it does not expand host-operation permissions.

## Delivered in this change

| Area | Implementation |
|---|---|
| Type and shape | HCIS type-size, font-weight and radius tokens generated into the app; root text scale remains independent and body uses the HCIS 14px default. |
| Shared table | `frontend/app/src/DataTable.tsx`: typed columns, native table/row-header semantics, labelled cards below 560px container width, sticky desktop headers, numeric/text sort, mobile sort controls, stable-key selection and bulk-action slots, pagination (100 by default, at most 500 DOM rows). |
| Existing pages | Services, hardware disks/PCI, overview filesystems/network, audit and sessions use DataTable. Authorization and existing actions are preserved. Logs retain their dedicated scrollable text/table view. |
| States | `ui.tsx`: StatusPill, StatusIndicator, LiveIndicator, EmptyState, ErrorState, Section live metadata, existing source provenance. Shapes and words carry status; Unknown stays neutral. |
| Refresh | `useData.ts`: keeps last successful data on transport failure, reports stale/refreshing/paused/disconnected state, avoids overlapping polls, pauses background polling, retries on visibility, rejects obsolete responses and clears protected data on 401/403. |
| Navigation | `CommandPalette.tsx`: header search or Ctrl/Cmd+K, native modal, combobox/listbox, arrows/Enter/Escape, focus restoration. Lists usable capability pages, own Sessions and local Theme Studio. No host-operation commands or resource-search API. |
| Themes | `theme.ts`: single JSON document, strict scanner, 57 semantic colour roles, built-in base, bounded schema and key contrast checks, allow-listed CSSOM writes under the existing CSP. No extra runtime dependency. |
| Theme Studio | `pages/ThemeStudio.tsx`: local library/import/export, form + explicit validated JSON editing, isolated labelled preview, viewport sizes, colour pickers, token reset, duplicate, 200 undo steps, metadata, issue-to-control focus, save and saved-version recovery, 15-second application trial with Keep/Revert/Escape. |
| TDK | `sdk/theme/`: dependency-free Node CLI, shared validator runtime, four starter files, MIT license and author guide. `schemas/theme/v1.json` is the runtime format. Generated files checked for drift by `make lint`. |

## Decisions that resolve earlier planning conflicts

- Runtime themes change colours only. Fonts, type size, motion, density, transparency behavior, navigation and layouts remain independent preferences. The historical draft's typography/space/motion groups and arbitrary installed-theme inheritance are not accepted in runtime v1.
- Runtime format: `{schemaVersion, id, name, version, license, base, colors}`. `base` is one of Midnight, Daybreak, Graphite or OLED. `colors` contains zero or more of the 57 semantic roles, flattened without the `hc-color-` prefix. The older `theme.schema.v1.draft.json` remains historical and is not a supported import.
- All imported values are hex colours. Fixed data surfaces and glass fallback stay opaque. Themes cannot override the 12 M1 aliases. Aliases rebind inside the preview so a dark preview remains correct inside a light app.
- Density names are Compact, Standard and Spacious. Shell thresholds remain the shipped 720px and 1100px boundaries. Cards use their own container width, so nested panels adapt too.
- Theme Studio is browser-local; it needs no server settings store or new scope. There are at most 20 themes and 10 previous saved versions per theme. History and current documents are written atomically into one bounded local-storage record. Save increments the patch version when an existing id would otherwise reuse or lower its version. Restoring loads an old revision into the editor; saving creates the next revision.
- A preview is never persisted as active without Keep. Timer, Escape and unmount restore the prior appearance. Keep/Revert uses fixed system colours. Invalid contrast can be saved as a draft but cannot be applied to the application.
- Tables use bounded pagination rather than adding a virtualizer dependency. Sorting is available only for columns supplying a typed sort value. Audit order remains the server's newest-first order.

## Frontend contract for backend/module developers

Use `DataTable<Row>` with `caption`, `rows`, `rowKey` and `columns`. Give resource rows stable keys (WWN/serial/resource id). A kernel name is allowed only as a render identity for an inventory row that has no stable id; it is never a durable operation target. Supply `sortValue` only when the column has a meaningful order. `selectable` and `bulkActions` provide presentation; they do not grant an operation permission or execute a host action.

Use Section metadata for provenance and partial/unavailable information. Pass `live` from `useData` for updating sections. Errors retain the last known reading and label it stale; authorization loss clears it. Never turn an unchecked value into a healthy state. Use StatusPill/StatusIndicator for icon-plus-word states.

Add usable pages to the existing capability registry; the sidebar and palette pick them up. Keep unavailable-module reasons. Future resource search, host selector, health aggregation, notifications, jobs and workspaces need real data/contracts; do not fill production chrome with example state.

Use the existing API client, CSRF and server scopes for all real actions. This frontend change adds no route, scope, broker operation, root execution or privileged write. Confirm/plan/progress component integration belongs with the corresponding reviewed backend operation. Reference module showcase pages remain designs until their backend milestones supply data.

Theme authoring instructions: [TDK guide](../../sdk/theme/README.md). TDK output can be imported into Theme Studio directly; no packaging service is needed. Server-wide install/persistence, settings sync, marketplace trust and plugins are later work.

## Verification

See the PR and `docs/evidence/hcis-ui/README.md` for measured results and local limitations. Do not infer that a component or future module is shipped merely because its reference design exists.
