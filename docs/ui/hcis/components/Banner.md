# Banner

A full-width message about the current page or host.

**Status: Production markup, proposed component.** The app renders this today as markup + `styles.css` classes; HCIS packages it as a reusable component. Not yet a component in the app.

Source: `frontend/app/src/App.tsx, pages/*.tsx (.banner)`

## When to use

Use for page-level conditions (helper missing, pool degraded). Supply `tone`, optional `title`, and an optional `action`.

## Rules

- crit banners use role="alert"; others role="status".

## Current implementation vs HCIS

App: warn-coloured outline only. HCIS: four tones, tinted grounds, icon + title.

Props: `BannerProps` in `reference/index.d.ts`. Preview data is labelled “Example data”.
