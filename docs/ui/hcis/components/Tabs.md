# Tabs

Tabs for resource detail views.

**Status: Planned.** Listed in the repository's planning docs but not implemented in the app yet.

Source: `docs/ui/01-DESIGN-SYSTEM.md §1.3`

## When to use

Resource pages use the standard set `RESOURCE_TABS`: Overview, Configuration, Logs, Diagnostics, History, Actions (omit those that do not apply).

## Rules

- WAI-ARIA tabs: arrow keys, Home, End.

Props: `TabsProps` in `reference/index.d.ts`. Preview data is labelled “Example data”.
