# HCIS tokens

- `tokens.json`: the source of truth, exported from the HCIS design system artifact. Colour families are flat lists; every colour token has a value per theme (`midnight`, `daybreak`, `graphite`, `oled`). `hc-palette-*` holds the owner-specified raw colours, and semantic `hc-color-*` tokens alias them (`"{hc-palette-teal-300}"`).
- `hcis-tokens.css`: CSS custom properties generated from `tokens.json`. It is a **reference only** and is not imported by `frontend/app`. Selecting a theme is `data-theme="<id>"` on `<html>` or on any element.

## Mapping to the app and to `.hltheme`

- Names follow the app's convention `--hc-<group>-<role>`. The semantic colour names match the v1 taxonomy in `docs/ui/02-THEME-TOKENS-AND-SCHEMA.md` §2.2 (`color.surface.raised` → `--hc-color-surface-raised`). New roles (glass, input, notice, unknown-text, backdrop tints, critical scrim) are additions to that taxonomy, which is a minor engine change (§2.5).
- The 12 M1 names in `frontend/design-system/tokens.css` (`--hc-color-ink`, `panel`, `panel-raised`, `rule`, `text`, `muted`, `signal`, `signal-ink`, `ok`, `warn`, `crit`, `focus`) are kept as aliases of the HCIS roles (docs/ui/02 §2.6). Migration step 1 did this: `frontend/design-system/tokens.css` now holds a generated HCIS region (`gen-css.py` writes it; `--check` covers it and `hcis-tokens.css`, run by `make lint`), so today's `styles.css` renders in HCIS colours.
- Theme JSON accepts only `#RRGGBB` / `#RRGGBBAA`. Aliases are resolved to hex when exporting a theme.
- Density (`compact | standard | spacious`), transparency and motion are **layout preferences** (ADR 0010), set as `data-hc-*` attributes by the shell, never by a theme.
- Typography is independent of themes. `type.families` holds four stacks: `ui` and `mono` (Inter, JetBrains Mono, then system fonts) and `ui-system` and `mono-system` (system fonts only). The **Interface font** preference (`data-hc-font="system"`) remaps `--font-ui`/`--font-mono` to the system stacks. `textScale` (`hc-text-scale-90 | 100 | 112 | 125`) is the **Text size** preference: it sets the root font size, and all type sizes are rem. A `.hltheme` may only name approved families (`typography.family.ui|mono`), never font files or URLs.

Regenerating: re-export `tokens.json` from the design system, then run `python3 -I gen-css.py`. It rewrites `hcis-tokens.css` and the generated region of `frontend/design-system/tokens.css`; `--check` verifies both.
