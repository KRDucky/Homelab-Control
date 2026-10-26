# UI and theme engine architecture (planning)

> **Status: planning only. Nothing in this folder is implemented.** It specifies the original interface
> and the theme engine requested by the owner directive (§3, §4, §8). Implementation needs owner approval
> and is sequenced in `10-PHASED-PLAN.md`. The M1 UI (React 19 + TypeScript + Vite, `frontend/app`) stays
> as it is until then.

| # | Document | Covers |
|---|---|---|
| 1 | [01-DESIGN-SYSTEM.md](01-DESIGN-SYSTEM.md) | Original design system, component architecture, component interfaces |
| 2 | [02-THEME-TOKENS-AND-SCHEMA.md](02-THEME-TOKENS-AND-SCHEMA.md) | Token taxonomy, theme document format, versioning, draft JSON Schema, examples |
| 3 | [03-THEME-STUDIO.md](03-THEME-STUDIO.md) | Theme Studio interaction model |
| 4 | [04-DASHBOARD-WIDGETS.md](04-DASHBOARD-WIDGETS.md) | Dashboards, widgets, layout persistence |
| 5 | [05-TELEMETRY-AND-STATE.md](05-TELEMETRY-AND-STATE.md) | Live telemetry, state management, refresh control |
| 6 | [06-LAYOUT-AND-NAVIGATION.md](06-LAYOUT-AND-NAVIGATION.md) | Responsive layouts, navigation, command palette |
| 7 | [07-ACCESSIBILITY-AND-MOTION.md](07-ACCESSIBILITY-AND-MOTION.md) | Accessibility and reduced-motion requirements |
| 8 | [08-PERFORMANCE-AND-TESTING.md](08-PERFORMANCE-AND-TESTING.md) | Performance budgets and test strategy |
| 9 | [09-THEME-SECURITY.md](09-THEME-SECURITY.md) | Theme package security and validation |
| 10 | [10-PHASED-PLAN.md](10-PHASED-PLAN.md) | Phased implementation and migration |
| HCIS | [hcis/README.md](hcis/README.md) | **HCIS design system** (owner directive 2026-10-21): visual identity, tokens for four themes, 81-component catalogue, migration steps. Supersedes 01–10 where they differ |

Artifacts: `theme.schema.v1.draft.json` (draft JSON Schema, not wired into code) and
`examples/themes/*.json` (three representative themes that validate against it).

## Constraints carried from the accepted architecture
- The UI is a client of the unprivileged API only. Themes, widgets and the Theme Studio never reach the
  broker, never add API scopes, and never change authorization (ADR 0001/0008, directive §9).
- The strict CSP stays (`default-src 'self'`, no `unsafe-inline`/`unsafe-eval`). Themes are applied by
  setting validated CSS custom properties through the CSSOM, which CSP permits; never by injecting
  `<style>` or `style=` markup.
- No Webmin, Authentic, Unraid, TrueNAS or other product assets (directive §2, §5). Fonts follow the HCIS
  font licensing policy (bundled Inter and JetBrains Mono, OFL-1.1, with system fallbacks; LICENSE-AUDIT
  LA-06); icons are original SVG.
- Prefer the platform over dependencies. New runtime dependencies need an ADR 0006 review.

## What already exists (M1)
- `frontend/design-system/tokens.css`: HCIS colour roles for four themes (generated from `hcis/tokens/tokens.json`; Midnight default,
  Daybreak via `prefers-color-scheme: light`, Graphite via `prefers-contrast: more`, `data-hc-theme` to choose) with the M1 colour
  names as aliases; M1 type, space and motion values; reduced motion zeroes `--hc-motion-fast`.
- `frontend/app/src/styles.css` consumes tokens throughout (208 `var(--hc-…)` uses, 0 hard-coded colours).
- `useData` polling hook; capability-aware navigation; axe-clean pages (`docs/evidence/m1/vm-26.04/`).
- Spec `docs/project/06-THEME-AND-PLUGIN-SDK.md` (`.hltheme` archives, `hlctl theme …`), which this
  plan refines.

Implemented frontend and backend handoff: [11 — UI foundation and TDK](11-IMPLEMENTED-UI-AND-TDK.md). User theme authoring: [TDK guide](../../sdk/theme/README.md).
