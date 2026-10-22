# Select

A native select with label and help.

**Status: Production markup, proposed component.** The app renders this today as markup + `styles.css` classes; HCIS packages it as a reusable component. Not yet a component in the app.

Source: `frontend/app/src/pages/Services.tsx (inline select)`

## When to use

Use for 4–15 fixed options. Fewer: SegmentedControl. More or searchable: a combobox (not yet designed).

Props: `SelectProps` in `reference/index.d.ts`. Preview data is labelled “Example data”.
