# Sidebar

Grouped, capability-aware primary navigation on glass.

**Status: Production markup, proposed component.** The app renders this today as markup + `styles.css` classes; HCIS packages it as a reusable component. Not yet a component in the app.

Source: `frontend/app/src/App.tsx (NavItem, .rail)`

## When to use

Order: Overview, Health & Diagnostics · Manage: System, Storage, Docker, Virtualization, Networking, File Sharing, Packages, Hardware · Govern: Audit, Settings. Grouping and order are user-configurable layout preferences.

## Rules

- Unavailable modules are either listed muted with the reason or hidden (`hideUnavailable`); never shown as working.
- Current item: aria-current="page", accent edge bar and selected fill.

Props: `SidebarProps` in `reference/index.d.ts`. Preview data is fictional and labelled “Example data”.
