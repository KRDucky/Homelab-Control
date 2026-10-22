# AppShell

The application frame: glass global header, glass sidebar, workspace, adapting to its own width.

**Status: Production markup, proposed component.** The app renders this today as markup + `styles.css` classes; HCIS packages it as a reusable component. Not yet a component in the app.

Source: `frontend/app/src/App.tsx (.shell, .rail, .topbar)`

## When to use

Wrap every signed-in page. Navigation comes from capabilities (`unavailable` with reasons). Theme, transparency, density and motion live in the header's Appearance menu.

## Rules

- ≥1100px container: expanded sidebar. 720–1100px: icon rail. <720px: drawer opened by the menu button, closed by Esc or the scrim.
- Density, transparency and motion are layout preferences on the shell (`data-hc-density`, `data-hc-transparency`, `data-hc-motion`); themes never set them (ADR 0010).
- Skip link to main content; main is focusable.

## Current implementation vs HCIS

App: fixed 14rem rail + topbar. HCIS: container-query shell, glass surfaces, host selector, health chip, workspaces, jobs, notifications, appearance.

Props: `AppShellProps` in `reference/index.d.ts`. Preview data is fictional and labelled “Example data”.
