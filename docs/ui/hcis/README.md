# HCIS — Homelab Control Interface System

> **Status: design direction; migration step 1 (colours) implemented.** HCIS is the owner-approved visual and interaction
> direction for the Homelab Control UI (owner directives 2026-10-21; density names, the OLED theme and
> the font licensing policy 2026-10-22). Only the colour tokens are wired into `frontend/app`; everything else is adopted through separately approved PRs
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

## Open decisions and differences to reconcile

- **Fonts vs LA-06.** The owner's font licensing policy (2026-10-22, `BRAND-BOOK.md` › Typography)
  names Inter (UI) and JetBrains Mono (technical text) as the preferred fonts, self-hosted, with system
  stacks as fallback and as a user preference. It permits OSS/FLOSS (e.g. OFL 1.1), public domain/CC0 and
  verified royalty-free licences, and forbids external font CDNs. `docs/compliance/LICENSE-AUDIT.md`
  LA-06 still records the earlier preference for system stacks and the planned Archivo removal; step 2
  updates LA-06, adds the notices and does the ADR 0006 dependency review. Licence checks: `fonts/README.md`.
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
2. **Typography:** bundle Inter/JetBrains Mono under the font licensing policy (notices, LA-06 update),
   the four font-family tokens, and the interface-font and text-size preferences.
3. **Shell:** glass header, sidebar/rail/drawer, Appearance menu (theme, transparency, density,
   motion) stored as layout preferences.
4. **Shared components**, one family at a time (DataTable, StatusPill/StatusIndicator, Section live
   states, …), each with Vitest and axe coverage as for M1.
5. **Module views** follow their milestones. Write-capable views wait for M2+ authorization.
