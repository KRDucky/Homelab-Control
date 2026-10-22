# AppearanceMenu

Popover for theme, transparency, density and motion.

**Status: Proposed (HCIS).** New in the HCIS specification; a reference design, not in the app.

Source: `docs/ui/06 §6.5 AppearancePreferences`

## When to use

Only theme is appearance content; the other three are layout/accessibility preferences that survive theme switches.

## Rules

- Reduced transparency swaps every glass surface for `glass-fallback`.

Props: `AppearanceMenuProps` in `reference/index.d.ts`. Preview data is fictional and labelled “Example data”.
