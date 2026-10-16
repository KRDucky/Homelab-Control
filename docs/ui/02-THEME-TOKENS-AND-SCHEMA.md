# 2. Theme token taxonomy and versioned schema (planning)

## 2.1 Principles
- **Semantic, not literal.** Components consume `color.surface.raised`, never "dark blue". A theme maps
  semantic roles to values; components never see raw values (already true of `styles.css` today).
- **Appearance only.** A theme changes how things look. Layout (dashboards, widget placement) and
  function (what is shown, who may do what) are separate documents (`04-DASHBOARD-WIDGETS.md`).
- **Typed and bounded values.** Every token has a type with a strict grammar and range (§2.4). There is
  no "free CSS string" token. This is what makes imported themes safe to apply (`09-THEME-SECURITY.md`).
- **Small runtime.** Applying a theme sets at most ~120 CSS custom properties on `:root` through the
  CSSOM; no stylesheet rebuild, no CSS-in-JS runtime.

## 2.2 Token taxonomy (v1)
Dotted path → CSS custom property `--hc-<path with dots as dashes>` (e.g. `color.surface.raised` →
`--hc-color-surface-raised`).

| Group | Tokens | Type |
|---|---|---|
| `color.surface` | `base`, `raised`, `sunken`, `overlay` | color |
| `color.text` | `primary`, `secondary`, `disabled`, `inverse`, `link` | color |
| `color.border` | `subtle`, `strong` | color |
| `color.accent` | `base`, `hover`, `contrast` (text on accent) | color |
| `color.focus` | `ring` | color |
| `color.status` | `ok`, `warn`, `crit`, `info`, `unknown` (+ `-contrast` text colours) | color |
| `color.chart` | `series` (6–12 colours, ordered), `grid`, `axis` | color[] / color |
| `color.meter` | `track`, `fill` | color |
| `typography` | `family.ui`, `family.mono` | font stack **enum** |
| | `size.xs … size.xl`, `size.display` | number, rem 0.625–5 |
| | `weight.regular`, `weight.strong` | integer 300–800 |
| | `lineHeight` | number 1.1–2.0 |
| `space` | `unit` (base step; others are multiples) | number, px 2–8 |
| `density` | `default` | enum `compact` \| `comfortable` \| `spacious` |
| `shape` | `radius.sm`, `radius.md`, `radius.lg`, `borderWidth` | number, px 0–24 / 0–4 |
| `elevation` | `level1`, `level2`, `level3` | shadow **object** `{y, blur, spread, color}` |
| `motion` | `duration.fast`, `duration.normal`, `easing` | duration (ms) 0–600; easing **enum** |
| `navigation` | `sidebar` (`expanded` \| `rail` \| `hidden`), `position` (`left` \| `top`) | enum |
| `status` | `indicator` (`dot` \| `pill` \| `icon-text`) | enum |

Font stacks are an enumeration such as `system-sans`, `system-serif`, `system-mono`, `humanist-sans`,
each mapping to a fixed, built-in `font-family` list of locally installed fonts. Themes cannot name
arbitrary fonts and cannot load fonts from URLs.

## 2.3 Theme document (v1)
```json
{
  "schemaVersion": 1,
  "id": "org.homelab-control.default",
  "name": "Homelab Default",
  "version": "1.0.0",
  "license": "MIT",
  "engine": ">=1.0.0 <2.0.0",
  "appearance": "auto",
  "modes": { "dark": { "...": "TokenSet" }, "light": { "...": "TokenSet" } },
  "defaults": { "density": "comfortable", "motion": "full", "contrast": "standard" }
}
```
- `appearance`: `dark` or `light` (one mode required) or `auto` (both required; follows
  `prefers-color-scheme` unless the user picks one).
- `extends` (optional): id of an installed theme; omitted tokens inherit from it, then from the built-in
  base. Resolution is shallow and acyclic (max depth 3).
- High-contrast variants are separate modes `darkHighContrast` / `lightHighContrast`, chosen when the user
  or `prefers-contrast: more` asks for them; if a theme lacks them, the built-in high-contrast set is used.
- Unknown top-level keys and unknown token paths are **errors**, not ignored (typo and smuggling defence).

## 2.4 Value grammar (enforced by the schema and again by the runtime validator)
| Type | Accepted | Rejected |
|---|---|---|
| color | `#RRGGBB` or `#RRGGBBAA` | names, `rgb()`, `var()`, `url()`, anything else |
| length | JSON **number**; the unit is fixed by the token (`rem` for type sizes, `px` for space/shape) and the range is per token | strings, `calc()`, `%`, negative, out of range |
| duration | integer milliseconds 0–600 | strings, `infinite` |
| easing | `standard` \| `emphasized` \| `linear` (mapped to fixed curves) | arbitrary `cubic-bezier()` |
| shadow | `{ "y": 0–24, "blur": 0–48, "spread": -8–8, "color": color }` | strings |
| enum | listed values only | anything else |
| text | `name`, `description`: plain text, ≤ 80 / 280 chars, rendered as text nodes only | markup |

## 2.5 Versioning and migration
- `schemaVersion` (integer) versions the *format*; `version` (semver) versions the *theme*; `engine` is a
  semver range of Homelab Control theme engines the theme supports.
- The engine ships migrators `v1→v2…`; an older theme is migrated in memory on load and can be re-exported
  in the new format. A theme newer than the engine is rejected with a clear message.
- Adding a token is a minor engine change (themes without it inherit the base); renaming or removing one
  needs a schema version bump plus a migrator.

## 2.6 Mapping from today's tokens (migration)
| M1 property | v1 token |
|---|---|
| `--hc-color-ink` | `color.surface.base` |
| `--hc-color-panel` / `--hc-color-panel-raised` | `color.surface.raised` / `color.surface.overlay` |
| `--hc-color-rule` | `color.border.subtle` |
| `--hc-color-text` / `--hc-color-muted` | `color.text.primary` / `color.text.secondary` |
| `--hc-color-signal` / `--hc-color-signal-ink` | `color.accent.base` / `color.accent.contrast` |
| `--hc-color-ok` / `warn` / `crit` | `color.status.ok` / `warn` / `crit` |
| `--hc-color-meter-track` | `color.meter.track` |
| `--hc-color-focus` | `color.focus.ring` |
| `--hc-font-*`, `--hc-space-*`, `--hc-radius-*`, `--hc-motion-fast` | `typography.*`, `space.unit` multiples, `shape.radius.*`, `motion.duration.fast` |

During migration the old names stay as aliases (`--hc-color-ink: var(--hc-color-surface-base)`) so
components can move one at a time.

## 2.7 Artifacts
- Draft schema: [`theme.schema.v1.draft.json`](theme.schema.v1.draft.json) (JSON Schema 2020-12).
- Examples (validate against the draft): [`examples/themes/default.json`](examples/themes/default.json)
  (auto, dark + light), [`examples/themes/oled.json`](examples/themes/oled.json) (dark only, extends
  default), [`examples/themes/high-contrast.json`](examples/themes/high-contrast.json).
- Contrast is not expressible in JSON Schema; the validator computes WCAG 2.2 ratios (§`03`, §`09`).
