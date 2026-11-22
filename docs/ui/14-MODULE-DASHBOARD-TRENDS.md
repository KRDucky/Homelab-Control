# Module dashboard and snapshot trends (post-PR #140)

This change extends the configurable, browser-local dashboard without changing API scopes or
privileged operations.

## Module launchpad
The Modules widget now groups capability entries into System, Storage & files,
Network & applications, Administration, and an Other modules fallback. Entries are
derived from the same capability response as the sidebar, not from a static list of
installed software. Available and degraded pages are links; unavailable and unsupported
pages show their reasons without becoming actionable. Forbidden entries are omitted.
Degraded reasons remain visible.

## Resource trends
The Resource trends widget samples the existing host overview (CPU usage and RAM
usage percentages) at each new `collected_at` timestamp while mounted, retaining
at most 60 observations in React component memory. It never persists telemetry to
localStorage or calls a new endpoint. A labelled HTML table is available alongside
the SVG trends for accessible numeric inspection. Missing measurements create gaps;
no zero or healthy value is invented.

The widget uses the existing configurable widget layout, and existing `hc.dashboard.v1`
layouts remain valid. New defaults include Resource trends. A user who previously saved
a layout can add the new widget in Customize dashboard.

## Scope and next steps
This is **not** server-side historical telemetry, cross-device synchronization,
per-module data aggregation, or generic plugin widget execution. Those need explicit
backend contracts, scopes, error boundaries, and performance review. See
`docs/ui/04-DASHBOARD-WIDGETS.md`.

Validate via `make lint`, `make check`, and browser e2e including responsive/mobile,
capability-limited accounts, unavailable tooling, and layout restoration.
