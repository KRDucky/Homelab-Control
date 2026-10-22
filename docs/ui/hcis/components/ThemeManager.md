# ThemeManager

Appearance → Themes: installed themes, safe timed preview, .hltheme import with validation, metadata, requirements and the mandatory tooltip.

**Status: Proposed (HCIS).** New in the HCIS specification; a reference design, not in the app.

Source: `ADR 0010; docs/ui/03 Theme Studio`

## Rules

- Preview auto-reverts after 15 s unless kept; Esc reverts immediately.
- Validation checks structure, value formats, forbidden layout/code keys and key contrast pairs, and says it is not a full accessibility audit.
- Theme Studio itself is not designed here.

Props: `ThemeManagerProps` in `reference/index.d.ts`. Preview data is fictional and labelled “Example data”.
