# AppShell

The application frame: glass global header, glass sidebar, workspace, adapting to its own width.

**Status: Production** (HCIS step 3). `frontend/app/src/Shell.tsx` + `shell.css`.

Source: `frontend/app/src/Shell.tsx`, `shell.css`

## When to use

Wrap every signed-in page. Navigation comes from capabilities (`unavailable` with reasons). Theme, transparency, density and motion live in the header's Appearance menu.

## Rules

- ≥1100px container: expanded sidebar. 720–1100px: icon rail. <720px: drawer opened by the menu button, closed by Esc or the scrim.
- Density, transparency and motion are layout preferences on the shell (`data-hc-density`, `data-hc-transparency`, `data-hc-motion`); themes never set them (ADR 0010).
- Skip link to main content; main is focusable.

## Current implementation vs HCIS

App (step 3): glass header and sidebar with solid fallbacks; expanded sidebar ≥1100px (user can collapse it to the rail), icon rail 720–1100px, drawer <720px (menu button; Escape or the scrim closes it; the rest of the page is `inert` while it is open and focus returns to the button); Appearance menu; content capped at 120rem on ultrawide screens. Breakpoints use viewport media queries (the shell is always full width). Not yet built: host selector, health chip, command palette, workspaces, jobs and notifications.

Props: `AppShellProps` in `reference/index.d.ts`. Preview data is fictional and labelled “Example data”.
