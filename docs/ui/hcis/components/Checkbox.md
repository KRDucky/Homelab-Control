# Checkbox

A native checkbox with an indeterminate state.

**Status: Production markup, proposed component.** The app renders this today as markup + `styles.css` classes; HCIS packages it as a reusable component. Not yet a component in the app.

Source: `frontend/app/src/pages/Services.tsx (.inline-check)`

## When to use

Use for independent on/off options in forms and for row selection in DataTable.

## Rules

- Native input underneath, so keyboard and forced-colors work.

Props: `CheckboxProps` in `reference/index.d.ts`. Preview data is labelled “Example data”.
