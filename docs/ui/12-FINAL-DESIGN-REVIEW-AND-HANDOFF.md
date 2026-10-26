# 12. Final HCIS design review and backend handoff

Date: 2026-10-26. Reviewed: `main` at `f6af207c` (PR #24, "HCIS frontend foundation and Theme Development Kit"). Fixes from this review are on the branch `review/hcis-final` and are not merged until the owner approves them.

## 1. Verdict

**The design is accepted: the frontend foundation faithfully realizes HCIS and is ready for backend and module development.** Seven defects were found and fixed on the review branch (section 4). None needs a redesign. The one open item is administrative: merge the review branch after CI passes on all three browser engines.

No design blocker remains. Everything in section 7 is a backend dependency or optional polish, not unfinished design.

## 2. What was verified, and what was not

Verified by running the built `hc-api` (non-root, loopback, production CSP) with the embedded UI, and by reading the sources named in the review request:

- **Chromium only, locally.** Phone (390px), tablet, desktop and ultrawide (3440px) widths, in all four themes (Midnight, Daybreak, Graphite, OLED). Preferences exercised: density (compact/standard/spacious), text size (90/100/112/125%), Inter and system fonts, reduced transparency, motion (system/full/reduced/none). My review script ran 140 checks (page overflow, axe WCAG 2.0/2.1/2.2 A/AA, focus visibility, target size, titles); it found 20 problems before the fixes and 0 after. That script is not committed; the durable checks are the additions to `tests/e2e/hcis-ui.mjs`. A negative control confirmed the new checks fail without the CSS fixes.
- **Theme Studio and TDK**: authoring through the form and the JSON editor, validation messages and issue-to-control focus, import and export, isolated previews (phone/tablet/desktop), undo, saved versions and restore, and the 15-second Keep/Revert trial including Escape. The TDK `create`, `validate` and `flatten` commands (through its test suite) and the four starter files.
- **Honest states**: loading, error with retained stale data, 401/403 clearing, unavailable-capability explanations and empty states.
- **Unit tests**: Vitest 51/51 and TDK 4/4 after the fixes; `make lint` passes.

Not verified, and what would verify it:

| Not verified | Evidence needed |
|---|---|
| Firefox and WebKit locally | The CI matrix on the review PR (PR #24 passed 21/21 on its head, but the review fixes are new). |
| Real broker data. The privileged broker was not running here, so Services, hardware and Overview tables were inspected only with route fixtures labelled as example data. | A run on a real host or disposable VM. This is a backend-integration check, not a design check. |
| A real screen reader (NVDA, JAWS, VoiceOver, Orca) | A manual pass. Semantics were checked with axe and the accessibility tree only. |
| Windows High Contrast on real hardware | A manual pass. Forced-colors behaviour was verified in Chromium only. |

## 3. Fidelity to the HCIS direction

- **Tokens and themes**: the four themes use the generated semantic tokens; status colours never carry meaning alone (icon plus word in StatusPill and StatusIndicator; Unknown stays neutral).
- **Typography and shape**: Inter and JetBrains Mono (or system stacks), the 14px HCIS body size, HCIS radii, and a root text scale that works in every theme.
- **Shell and layout**: sidebar at 1100px and above, icon rail from 720px, drawer below that; content capped at 120rem on ultrawide.
- **Tables**: native table semantics, sticky headers, and labelled cards below 560px of container width (so nested panels adapt too).
- **States**: Live, Refreshing, Stale, Paused and Disconnected are all distinguishable; stale data stays visible and labelled.
- **Palette**: capability-filtered, navigation only, with no invented actions.
- **Honesty**: no fabricated data was found in a production view.

## 4. Defects found

Design defects, all fixed on the review branch. "Reproduce" gives the shortest route.

| ID | Severity | Defect and reproduction | Expected | Component / file | Fix |
|---|---|---|---|---|---|
| DT-1 | Medium | At table width, the row header (unit, mount point, interface name) wrapped at hyphens when a long description column squeezed it (a name such as `example-000.service` on two lines). Open Services at 1100px. | Identifiers stay on one line; the labelled scroll region handles exceptional length. | DataTable, `components.css` | `white-space: nowrap` on `tbody th`, reset to normal in card mode. |
| DT-2 | Medium | In card mode the table `<caption>` shrink-wrapped into a narrow anonymous cell and wrapped word by word. Open any DataTable at 390px. | The caption spans the card list. | DataTable, `components.css` | `display: block` inside the card container query. |
| DT-3 | High (accessibility) | In forced-colors mode (Windows High Contrast) the focus ring disappeared, because the box-shadow ring is removed and no outline was painted. Tab through any page with `forced-colors: active`. WCAG 2.4.7 and 2.4.11. | A visible focus indicator in every colour mode. | Global focus style and the segmented control, `styles.css` | Transparent 2px outline always present; `Highlight` outline in forced-colors. |
| DT-4 | Low | `<summary>` disclosure targets were shorter than 24px, below WCAG 2.5.8. | At least 24px. | `styles.css` | `min-block-size: 1.5rem`. |
| DT-5 | Medium | A Theme Studio hex field committed only on blur. Typing a colour and then clicking Save or a preview control could lose the click because the layout shifted at blur, and the preview lagged the field. | The preview follows complete valid hex as it is typed; an incomplete value never commits. | ColorValue, `ThemeStudio.tsx` | Local text state, commit on a complete valid hex, revert invalid text on blur. |
| DT-6 | Medium (accessibility) | Every route had the same document title. WCAG 2.4.2. | Page-specific titles, for example "Themes & Theme Studio · Homelab Control". | Shell and App | Shell sets `document.title` from the route label. |
| DT-7 | Low | The TDK CLI rejected a theme file with a UTF-8 byte-order mark (Windows Notepad), although the app importer accepts it. | The two behave the same. | `sdk/theme/tdk.mjs` | The CLI strips a leading BOM; test added. |

Regression coverage added: Vitest tests for hex commit-as-typed and the document title, a TDK BOM test, and e2e checks for axe at default text size, summary height, the Studio title, caption width at 390px and the Chromium forced-colors outline.

## 5. Decision evaluated: runtime themes change colours only

**Accepted, with no change.** Fonts, density, motion, layout and navigation stay separate preferences.

- **For**: it is what the brand book and ADR 0010 promised. A user who needs larger text, system fonts or reduced motion keeps them in every theme, so a third-party theme can never silently defeat an accessibility preference. The import surface is one flat set of hex colours with no URLs, font files, lengths or layout values, so the strict scanner and the CSSOM-under-CSP path remain simple. The TDK stays dependency-free and the starter files stay small.
- **Cost**: a theme cannot carry a typographic or spatial identity. For a system-administration tool that is the right trade.
- **Residual risk**: contrast validation covers the key text/surface, status, hover and border pairs, not every possible pairing. Studio and the TDK say so, and the Keep/Revert trial exists for this reason. The earlier typography/space/motion draft stays historical. If a richer format is ever wanted it belongs in a new schema version under a new ADR, not in v1.

## 6. Documentation reconciled

- Density names are `compact | standard | spacious` everywhere (docs/ui/01 and the brand book were stale).
- The brand book, tokens README and HCIS README no longer describe a `typography.family` theme field or the glass/input/notice/unknown token additions as open decisions. The 57-role runtime v1 schema is the single format.
- Component catalogue statuses now match the code: DataTable, StatusPill, StatusIndicator, LiveIndicator, EmptyState, ErrorState, CommandPalette and ThemeManager are implemented; the other catalogue entries remain designs.
- ADR 0010, the schema READMEs, `docs/STATUS.md`, project memory, the phased plan and the evidence README record the continuation, PRs #23 and #24, and this review. Server persistence of themes and preferences remains phase U5.

## 7. Remaining work

### Optional polish (not blockers)

- Run a manual pass with a real screen reader and Windows High Contrast, and a visual look at Firefox and WebKit.
- Widen Theme Studio contrast validation from key pairs to a fuller pairwise matrix.
- Add table virtualization only if a real list exceeds the 500-row DOM cap. Pagination is sufficient for current data.
- Turn the "production markup" components (Button, Badge, Banner and similar) into shared components when a module needs them, not before.

### Backend and module dependencies (not design work)

These need their own contracts, tests and, where they write to the host, owner authorization. M2 beyond PR-1 is not authorized.

| Needs | Why |
|---|---|
| Server settings store (phase U5) | Themes, density and the other preferences are per browser today. Cross-device persistence needs an API, scope and audit events. |
| Resource search API | The palette is navigation only and must stay so until a real index exists. |
| Host selector and health aggregation | The header chrome has no data source; do not fill it with example state. |
| Jobs, notifications and live telemetry (SSE) | JobProgress, BackgroundJobPanel and live charts need a job model and server limits. |
| Plan, confirm and progress flow | ConfirmDialog and the plan view arrive with the first reviewed write operation, not before. |
| Workspaces and dashboard widgets | They need stored layouts; no layout storage exists server-side. |
| Module views (storage, network, containers and so on) | Each depends on its own broker operations and milestone. |

## 8. Handoff to Claude Opus

### Reuse as is

- **`DataTable<Row>`** (`frontend/app/src/DataTable.tsx`): pass `caption`, `rows`, `rowKey` and typed `columns`. Use stable resource keys (WWN, serial, resource id), never `/dev/sdX` as a durable identity. Supply `sortValue` only for columns with a real order. `selectable` and `bulkActions` are presentation only and grant no permission.
- **State primitives** (`ui.tsx`): `Section` (provenance, partial and unavailable metadata), `StatusPill`, `StatusIndicator`, `LiveIndicator`, `EmptyState`, `ErrorState`. Never turn an unchecked value into a healthy state.
- **`useData`** (`useData.ts`): visibility-aware polling, stale data retained on error, data cleared on 401/403, 401 returns to login, no overlapping polls. Pass its `live` value to `Section`.
- **Shell, capability registry and CommandPalette**: add a usable page to the capability registry and it appears in the sidebar and palette; keep the unavailable-module reason text. Set the route label, which becomes the document title.
- **Preferences and themes**: do not hard-code colours; use the semantic tokens. Test new views in all four themes and at 390px, since the shared cards, focus ring and title handling are already in place.
- **TDK** (`sdk/theme/`): MIT, dependency-free, shares one validator with the app.

### Rules that still apply

Use the existing API client, CSRF and server scopes. New routes go through `Server.handle` with an OpenAPI entry and scope; new broker operations are reviewed and stay read-only until M2 write safety is designed and authorized. Examples and mockups must be labelled. Strict CSP, no inline styles, exact npm versions, `npm ci --ignore-scripts`.

### Suggested order

1. Merge the review branch after CI passes on Chromium, Firefox and WebKit.
2. Continue with the already-authorized M2 PR-1 follow-ups, then request authorization for the next M2 slice.
3. For each new module view, build it on the primitives above, add its Vitest and axe coverage, then extend the browser matrix.
