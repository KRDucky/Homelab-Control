# NavigationRail

The collapsed, icon-only form of the sidebar (56px).

**Status: Production** (HCIS step 3), as a mode of the sidebar in `frontend/app/src/Shell.tsx`.

Source: `frontend/app/src/Shell.tsx`, `shell.css`

## When to use

Used automatically by AppShell between 720 and 1100px, or by choice (Collapse sidebar, remembered). Each icon keeps its label as the accessible name; the label (and, for unavailable modules, the reason) shows as a tooltip on hover and keyboard focus. Unavailable modules are focusable in the rail so the reason can be read.

Props: `NavigationRailProps` in `reference/index.d.ts`. Preview data is fictional and labelled “Example data”.
