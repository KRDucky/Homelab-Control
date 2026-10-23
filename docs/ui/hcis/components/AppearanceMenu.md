# AppearanceMenu

Popover for theme, transparency, density, motion, interface font and text size.

**Status: Production** (HCIS step 3). `frontend/app/src/AppearanceMenu.tsx`, preferences in `preferences.ts`.

Source: `docs/ui/06 §6.5 AppearancePreferences`

## When to use

Only theme is appearance content. Transparency, density, motion, interface font (Inter or System) and text size (90–125%) are layout and accessibility preferences that survive theme switches, so any font or text size works with Midnight, Daybreak, Graphite and OLED.

## Rules

- Reduced transparency swaps every glass surface for `glass-fallback`.
- App: a non-modal dialog opened by the header button (`aria-expanded`); focus moves to the selected theme; Escape, Done or a click outside closes it and returns focus. Native radio groups (arrow keys). Theme offers **System** (no `data-hc-theme`: Daybreak on a light OS, Midnight on dark, Graphite with `prefers-contrast: more`) plus the four themes. Motion offers System (follows `prefers-reduced-motion`), Full, Reduced and None. Reset restores the defaults.
- The popover is opaque (`surface-overlay`): inside the glass header a backdrop blur only sees the header, so a translucent popover would let page text show through.
- Persistence: per browser (`localStorage` key `hc.preferences.v1`, schema version 1, every value checked against an allow-list), applied as `data-hc-*` attributes on `<html>` before the first render; changes in another tab are picked up. The server-side settings store is phase U5 (`docs/ui/10`, needs M2 approval).

Props: `AppearanceMenuProps` in `reference/index.d.ts`. Preview data is fictional and labelled “Example data”.
