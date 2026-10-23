# Sidebar

Grouped, capability-aware primary navigation on glass.

**Status: Production (ungrouped)** (HCIS step 3). The app has five modules, so there are no groups yet; grouping and configurable order come with more modules.

Source: `frontend/app/src/Shell.tsx (NavItem, .sidebar)`

## When to use

Order: Overview, Health & Diagnostics · Manage: System, Storage, Docker, Virtualization, Networking, File Sharing, Packages, Hardware · Govern: Audit, Settings. Grouping and order are user-configurable layout preferences.

## Rules

- Unavailable modules are either listed muted with the reason or hidden (`hideUnavailable`); never shown as working.
- Current item: aria-current="page", accent edge bar and selected fill.

Props: `SidebarProps` in `reference/index.d.ts`. Preview data is fictional and labelled “Example data”.
