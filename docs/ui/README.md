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

Artifacts: `theme.schema.v1.draft.json` (draft JSON Schema, not wired into code) and
`examples/themes/*.json` (three representative themes that validate against it).

## Constraints carried from the accepted architecture
- The UI is a client of the unprivileged API only. Themes, widgets and the Theme Studio never reach the
  broker, never add API scopes, and never change authorization (ADR 0001/0008, directive §9).
- The strict CSP stays (`default-src 'self'`, no `unsafe-inline`/`unsafe-eval`). Themes are applied by
  setting validated CSS custom properties through the CSSOM, which CSP permits; never by injecting
  `<style>` or `style=` markup.
- No Webmin, Authentic, Unraid, TrueNAS or other product assets (directive §2, §5). System fonts and
  original SVG only, unless the owner keeps Archivo (LICENSE-AUDIT LA-06).
- Prefer the platform over dependencies. New runtime dependencies need an ADR 0006 review.

## What already exists (M1)
- `frontend/design-system/tokens.css`: 48 `--hc-*` custom properties (dark defaults plus a light override set); light/dark via
  `prefers-color-scheme`; reduced motion zeroes `--hc-motion-fast`.
- `frontend/app/src/styles.css` consumes tokens throughout (208 `var(--hc-…)` uses, 0 hard-coded colours).
- `useData` polling hook; capability-aware navigation; axe-clean pages (`docs/evidence/m1/vm-26.04/`).
- Spec `docs/project/06-THEME-AND-PLUGIN-SDK.md` (`.hltheme` archives, `hlctl theme …`), which this
  plan refines.
