# HCIS — Homelab Control Interface System

> **Status:** Steps 1–3 merged in PR #19. The owner authorized the remaining frontend and TDK on 2026-10-25. Shared tables/states, navigation-only palette, browser-local theme import/export, Theme Studio and TDK are implemented in the current change. [Implementation handoff](../11-IMPLEMENTED-UI-AND-TDK.md) defines what exists and what module/backend work remains.


Visual identity: **Adaptive Glass Linux Admin**. That means restrained frosted glass for the shell and
overlays, opaque surfaces for data, Material-style interaction discipline, Nordic/Arc calm, and dense,
readable technical content.

The live, interactive design system (all component previews, four themes, and the source these files
were exported from) is the HCIS artifact:
<https://claude.ai/artifact/4EqUtyxYPEoU2SnHX5sYwt> (private to the owner).

## For agents working on the UI (read this first)

1. **Read before building UI.** Read `BRAND-BOOK.md` (principles, voice, themes, glass, layout, safety
   flow, accessibility), then the component's page in `components/`.
2. **Status is binding.** A component marked Planned or Proposed is a design, not shipped code. Never
   describe it as existing. Anything marked “awaiting authorization” writes to the host and is M2+ work
   that needs owner approval (root `CLAUDE.md`, `docs/STATUS.md`, `docs/m2/OWNER-DECISIONS.md`).
3. **Use tokens, never raw values.** Components consume semantic `--hc-color-*`, `--hc-space-*`,
   `--hc-radius-*`, `--hc-shadow-*` and the type styles in `tokens/`. Never use `--hc-palette-*` in
   components.
4. **The reference code is a reference.** `components/reference/*.jsx` and `hcis.css` are hand-written
   reference implementations (React 18 globals, built for the design-system previews). Port behaviour and
   markup into `frontend/app` as typed TSX that follows the app's lint, CSP and dependency rules. Don't
   import the reference files or add their patterns wholesale.
5. **Keep the safety flow.** Every write follows preview, confirm (risk class, re-auth or type-to-confirm
   where required), progress, verified result, then audit. No preview or design ever executes an operation.
6. **Keep the identity rules.** Drives are identified by WWN/serial/EUI, never `/dev/sdX`. Bay locations
   are Verified, Inferred or Unknown. Findings are Confirmed or Suspected. Unchecked means Unknown,
   never healthy. Sample data is labelled.
7. **Themes are appearance only** (ADR 0010). Density, transparency, motion, interface font, text size,
   navigation order and workspaces are user layout preferences. Typography never changes with the theme.
8. **Fonts follow the licensing policy** (`BRAND-BOOK.md` › Typography). Components use `var(--font-ui)` /
   `var(--font-mono)` only. Never add a font file without the policy checks, and never use a font CDN.

## Contents

| Path | What |
|---|---|
| `BRAND-BOOK.md` | The full HCIS guidelines, as published in the design system. File names in its "Using the components" section refer to the artifact layout; the repo equivalents are listed here. |
| `tokens/tokens.json` | All tokens: 4 themes × 57 semantic colours, 26 raw palette colours, 12 M1 aliases, type (4 font-family stacks), text scale, spacing, radii, density, glass, motion, shadows |
| `tokens/hcis-tokens.css` | CSS custom properties generated from `tokens.json` (`gen-css.py`, `--check` for drift) |
| `tokens/README.md` | Mapping to the M1 tokens and to `.hltheme` |
| `components/README.md` | Catalogue of 81 components and showcase views, with status |
| `components/<Name>.md` | Guidelines per component: when to use it, rules, current-vs-HCIS, status |
| `components/reference/` | Reference implementation (`hcis-core.jsx`, `hcis-v2.jsx`), stylesheet (`hcis.css`) and prop types (`index.d.ts`) |
| `fonts/README.md` | Licence records for the preferred fonts (no font files) |
| `icons/` | Original HCIS icon set: 54 SVGs, 20px grid, 1.5px stroke |

## Relationship to the planning docs (01–10)

HCIS refines `01-DESIGN-SYSTEM.md` (component inventory and interfaces), `02-THEME-TOKENS-AND-SCHEMA.md`
(it adds token roles and keeps the naming), `06-LAYOUT-AND-NAVIGATION.md` (shell, navigation, workspaces)
and `07-ACCESSIBILITY-AND-MOTION.md` (WCAG 2.2 AA, reduced motion, plus reduced transparency). Where they
differ, these files record the owner's newer decision. The differences still to reconcile in those docs
are listed below.

## Reconciled design decisions (2026-10-25)

- Fonts are separate preferences, using the already bundled/licensed Inter and JetBrains Mono or system stacks. Runtime themes carry no font or typography fields.
- Density is `compact | standard | spacious`; shell thresholds stay 720px/1100px. Cards adapt below 560px container width.
- Runtime theme schema: `schemas/theme/v1.json`, 57 semantic colours and a fixed built-in base. The earlier draft and example files under `docs/ui/examples/themes/` are historical planning artifacts; current starter themes are under `sdk/theme/examples/`.
- TDK and its shared parser/validator are MIT, within the approved SDK boundary. No font/icon dependency is added. The original icon set retains the repository GPL license.
- Server persistence, resource search, jobs/notification data, workspaces and privileged actions remain with their backend milestones. See the implementation handoff rather than treating previews as production modules.

## Migration history

1. **Tokens as aliases.** *Done (branch `feature/hcis-step1-token-aliases`).* `frontend/design-system/tokens.css`
   carries the HCIS colour roles for all four themes, generated from `tokens/tokens.json` by
   `tokens/gen-css.py` (`make lint` fails on drift). The 12 M1 names alias them. Midnight is the default,
   Daybreak follows a light OS preference, Graphite follows `prefers-contrast: more` on a dark preference,
   and `data-hc-theme="midnight|daybreak|graphite|oled"` on `<html>` chooses one (`dark` still forces the
   default). There's no theme picker yet. `styles.css` changed in three places only: links use `text-link`,
   and failed/warning rows use `status-*-subtle` (the old tints fell below 4.5:1 in Daybreak).
2. **Typography.** *Done (branch `feature/hcis-step2-typography`).* Inter and JetBrains Mono are bundled
   from `@fontsource-variable/inter` and `@fontsource-variable/jetbrains-mono` 5.3.0 (`main.tsx`, all
   subsets, served from `hc-api`'s own origin; CSP `font-src 'self'`). `gen-css.py` writes a second
   generated region in `frontend/design-system/tokens.css`: `--hc-font-ui`, `--hc-font-mono`,
   `--hc-font-ui-system`, `--hc-font-mono-system` (the reference's `--font-*`), with the M1 name
   `--hc-font-family` as an alias of `--hc-font-ui`. Archivo and the Archivo-only `--hc-font-stretch-*`
   tokens are gone. `styles.css`: technical text (`code`, log lines, kernel/device names, addresses,
   mount points, unit names, serials and UUIDs) uses `--hc-font-mono`; heading weights moved from
   800–850 to 700 and the login title from 2.5rem to 2rem, because Inter is wider than condensed Archivo;
   tags no longer wrap; the top bar wraps instead of overflowing on phones; table wrappers contain their visually-hidden headers so they can't widen the page.
   The M1 type scale, spacing, radii, motion and colours are unchanged.
3. **Shell and appearance.** *Done (branch `feature/hcis-step3-shell`).* `Shell.tsx` + `shell.css`: glass
   header and sidebar (solid `glass-fallback` without `backdrop-filter`, with Surfaces: Solid, or with
   `prefers-reduced-transparency`); sidebar ≥1100px (collapsible to the icon rail, remembered), rail
   720–1100px, drawer <720px; content capped at 120rem. `AppearanceMenu.tsx`: theme (System, Midnight,
   Daybreak, Graphite, OLED), density, interface font, text size, surfaces and motion, each independent;
   the theme never changes layout, navigation or permissions. `preferences.ts`: per-browser storage
   (`hc.preferences.v1`, allow-listed values) applied as `data-hc-*` on `<html>` before first render,
   synced across tabs; server-side storage is phase U5. Density maps to cell, control and panel spacing
   (Standard = the M1 values). `gen-css.py` adds the themed shadows and a generated layout region (glass,
   density, motion). Tables scroll inside `TableRegion`, which becomes a focusable, labelled region
   when it scrolls (fixes axe `scrollable-region-focusable`). Not in step 3: command palette, host
   selector, health chip, workspaces, jobs/notifications, stacked table cards, `.hltheme` import and
   Theme Studio (stacked cards, import and Studio were delivered later in step 4; the rest need backend contracts).
4. **Shared components** — implemented in the current change (see handoff), including DataTable, status and live states. Delivered one family at a time (DataTable with stacked cards on phones, StatusPill/StatusIndicator, LiveIndicator, EmptyState/ErrorState, CommandPalette, ThemeManager), each with Vitest and axe coverage as for M1. Remaining catalogue designs (host selector, health chip, workspaces, jobs/notifications, module showcases) wait for their backend contracts.
5. **Module views** follow their milestones. Write-capable views wait for M2+ authorization.

Theme import/export, browser-local Studio and TDK: see `../11-IMPLEMENTED-UI-AND-TDK.md`.
