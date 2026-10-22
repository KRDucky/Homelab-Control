# Button

The single action control: primary, secondary, ghost and danger.

**Status: Production markup, proposed component.** The app renders this today as markup + `styles.css` classes; HCIS packages it as a reusable component. Not yet a component in the app.

Source: `frontend/app/src/styles.css (button, button.ghost), Login.tsx`

## When to use

One primary button per view region. Use `secondary` for safe alternatives, `ghost` in toolbars and rows, `danger` only for the confirm step of a destructive operation.

## Rules

- `busy` shows a spinner, keeps width and sets aria-busy.
- `disabledReason` replaces a bare disabled state: the reason is rendered and linked with aria-describedby.
- Labels are verbs; append “…” when the button opens a preview or dialog.

## Current implementation vs HCIS

App: primary fill `--hc-color-signal`, ghost variant only. HCIS adds secondary and danger, 4px radius, density-driven height.

Props: `ButtonProps` in `reference/index.d.ts`. Preview data is labelled “Example data”.
