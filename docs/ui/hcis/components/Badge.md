# Badge

An outlined label for modes and flags, or a numeric count.

**Status: Production markup, proposed component.** The app renders this today as markup + `styles.css` classes; HCIS packages it as a reusable component. Not yet a component in the app.

Source: `frontend/app/src/App.tsx (.readonly-badge, .helper-off, .tag)`

## When to use

Use for persistent modes (Read-only), environment flags, and counts on nav and tabs.

## Rules

- Count badges carry an aria-label with the noun.

Props: `BadgeProps` in `reference/index.d.ts`. Preview data is labelled “Example data”.
