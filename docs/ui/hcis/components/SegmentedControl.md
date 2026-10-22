# SegmentedControl

Radio buttons styled as a compact segmented filter.

**Status: Production markup, proposed component.** The app renders this today as markup + `styles.css` classes; HCIS packages it as a reusable component. Not yet a component in the app.

Source: `frontend/app/src/pages/Services.tsx (.segmented)`

## When to use

Use for 2–5 mutually exclusive views or filters. Options may carry counts.

## Rules

- Native radios in a fieldset with a hidden legend; focus ring drawn on the segment.

## Current implementation vs HCIS

App: selected segment filled with signal. HCIS: selected segment raised on `surface-overlay`; the accent is kept for primary actions.

Props: `SegmentedControlProps` in `reference/index.d.ts`. Preview data is labelled “Example data”.
