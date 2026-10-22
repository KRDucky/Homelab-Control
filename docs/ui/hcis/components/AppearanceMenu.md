# AppearanceMenu

Popover for theme, transparency, density, motion, interface font and text size.

**Status: Proposed (HCIS).** New in the HCIS specification; a reference design, not in the app.

Source: `docs/ui/06 §6.5 AppearancePreferences`

## When to use

Only theme is appearance content. Transparency, density, motion, interface font (Inter or System) and text size (90–125%) are layout and accessibility preferences that survive theme switches, so any font or text size works with Midnight, Daybreak, Graphite and OLED.

## Rules

- Reduced transparency swaps every glass surface for `glass-fallback`.

Props: `AppearanceMenuProps` in `reference/index.d.ts`. Preview data is fictional and labelled “Example data”.
