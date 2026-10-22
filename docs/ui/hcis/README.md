# HCIS — Homelab Control Interface System

> **Status: design direction; migration step 1 (colours) implemented.** HCIS is the owner-approved visual and interaction
> direction for the Homelab Control UI (owner directives 2026-10-21; density names and the OLED theme
> 2026-10-22). Only the colour tokens are wired into `frontend/app`; everything else is adopted through separately approved PRs
> (see "Migration" below). Colours now come from HCIS; type, spacing, shape, motion and components are
> still the M1 implementation.

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
7. **Themes are appearance only** (ADR 0010). Density, transparency, motion, navigation order and
   workspaces are user layout preferences.

## Contents

| Path | What |
|---|---|
| `BRAND-BOOK.md` | The full HCIS guidelines, as published in the design system. File names in its "Using the components" section refer to the artifact layout; the repo equivalents are listed here. |
| `tokens/tokens.json` | All tokens: 4 themes × 57 semantic colours, 26 raw palette colours, 12 M1 aliases, type, spacing, radii, density, glass, motion, shadows |
| `tokens/hcis-tokens.css` | CSS custom properties generated from `tokens.json` (`gen-css.py`, `--check` for drift) |
| `tokens/README.md` | Mapping to the M1 tokens and to `.hltheme` |
| `components/README.md` | Catalogue of 81 components and showcase views, with status |
| `components/<Name>.md` | Guidelines per component: when to use it, rules, current-vs-HCIS, status |
| `components/reference/` | Reference implementation (`hcis-core.jsx`, `hcis-v2.jsx`), stylesheet (`hcis.css`) and prop types (`index.d.ts`) |
| `icons/` | Original HCIS icon set: 54 SVGs, 20px grid, 1.5px stroke |

## Relationship to the planning docs (01–10)

HCIS refines `01-DESIGN-SYSTEM.md` (component inventory and interfaces), `02-THEME-TOKENS-AND-SCHEMA.md`
(it adds token roles and keeps the naming), `06-LAYOUT-AND-NAVIGATION.md` (shell, navigation, workspaces)
and `07-ACCESSIBILITY-AND-MOTION.md` (WCAG 2.2 AA, reduced motion, plus reduced transparency). Where they
differ, these files record the owner's newer decision. The differences still to reconcile in those docs
are listed below.

## Open decisions and differences to reconcile

- **Fonts vs LA-06.** HCIS specifies Inter (UI) and JetBrains Mono (technical text), self-hosted with
  system fallbacks. `docs/compliance/LICENSE-AUDIT.md` LA-06 recorded the owner's earlier preference
  for system font stacks and the planned removal of Archivo. Shipping Inter/JetBrains Mono (both
  OFL-1.1) needs an owner decision, notices and an ADR 0006 dependency review. The token stacks already
  fall back to system fonts.
- **Density names.** HCIS uses `compact | standard | spacious` (owner, 2026-10-22).
  `06-LAYOUT-AND-NAVIGATION.md` §6.5 still says `compact | comfortable | spacious`.
- **Token taxonomy additions.** Glass, input, notice and unknown roles are new in the `.hltheme` schema
  (`02` §2.2, `theme.schema.v1.draft.json`).
- **Themes.** HCIS ships Midnight, Daybreak, Graphite (high contrast) and OLED (surfaces from
  `examples/themes/oled.json`). The `examples/themes/` files still describe the earlier palettes.
- **Icon licence.** The icons fall under the repository default (GPL-3.0-or-later) unless the owner
  adds them to the MIT boundary with the tokens.

## Migration (each step a separate, approved PR)

1. **Tokens as aliases.** *Done (branch `feature/hcis-step1-token-aliases`).* `frontend/design-system/tokens.css`
   carries the HCIS colour roles for all four themes, generated from `tokens/tokens.json` by
   `tokens/gen-css.py` (`make lint` fails on drift). The 12 M1 names alias them. Midnight is the default,
   Daybreak follows a light OS preference, Graphite follows `prefers-contrast: more` on a dark preference,
   and `data-hc-theme="midnight|daybreak|graphite|oled"` on `<html>` chooses one (`dark` still forces the
   default). There's no theme picker yet. `styles.css` changed in three places only: links use `text-link`,
   and failed/warning rows use `status-*-subtle` (the old tints fell below 4.5:1 in Daybreak).
2. **Typography** (after the LA-06 decision): system stack or bundled Inter/JetBrains Mono.
3. **Shell:** glass header, sidebar/rail/drawer, Appearance menu (theme, transparency, density,
   motion) stored as layout preferences.
4. **Shared components**, one family at a time (DataTable, StatusPill/StatusIndicator, Section live
   states, …), each with Vitest and axe coverage as for M1.
5. **Module views** follow their milestones. Write-capable views wait for M2+ authorization.
