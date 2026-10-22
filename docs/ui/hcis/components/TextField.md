# TextField

A labelled text input with help text and inline error.

**Status: Production markup, proposed component.** The app renders this today as markup + `styles.css` classes; HCIS packages it as a reusable component. Not yet a component in the app.

Source: `frontend/app/src/Login.tsx (label + input), styles.css input`

## When to use

Supply `label`; add `help` for format hints and `error` once validation fails. `mono` for paths, IDs and commands.

## Rules

- Errors use aria-invalid and aria-describedby; the message starts with an icon.
- Never use placeholder as the label.

## Current implementation vs HCIS

App: input ground `--hc-color-ink`, 8px radius. HCIS: `surface-sunken`, `border-strong` (3:1), 6px radius.

Props: `TextFieldProps` in `reference/index.d.ts`. Preview data is labelled “Example data”.
