# Capability-driven WebUI dashboard and module navigation

The WebUI is built from server-reported capabilities. A menu entry or related-module
link **never grants permission**. The API and broker remain authoritative for reads
and writes, and unavailable/forbidden modules retain the existing visibility rules.

## Implemented

- `Shell.tsx`: grouped navigation with a fallback group for unknown capability IDs.
  Contextual module tabs appear only when more than one related page is usable.
  Mobile drawer, tablet rail and desktop sidebar share the same source of truth.
- `Dashboard.tsx`: six read-only cards using `/api/v1/host/overview` and the
  current capabilities response. Host identity, CPU, memory, filesystems and
  network figures are backend data, not sample metrics. The Modules card links
  only to available/degraded pages. Any future capability added to the response
  appears in the Modules card without UI-specific registration.
- `OverviewPage.tsx`: Dashboard and Detailed host report views. Existing
  inventory tables remain in the detailed report; plugin overview contributions
  remain mounted outside the view toggle.
- Browser-local layout customization: show/hide and move cards with regular
  buttons (keyboard accessible), Save, Cancel, and Reset. Versioned,
  allow-listed settings contain only widget IDs and their order. They never
  contain host telemetry, API tokens or other identifying data.

## Deliberate limitations and follow-up work

- Widget positions are order-based responsive CSS grid placement, not draggable
  or resizable coordinates. Multi-dashboard selection, cross-device sync,
  the full JSON widget schema, chart history and shared telemetry streams
  remain future work. See `docs/ui/04-DASHBOARD-WIDGETS.md`.
- There are no fake operational buttons in the dashboard. Future actions must
  use the existing server-enforced scopes and plan/confirm/audit flow.
- Future module-specific tabs are driven by the capabilities endpoint. Detailed
  per-module sub-tabs should be built only as their backend APIs are implemented.
- Storage card totals are explicitly a summary of measured mount entries, not
  a physical capacity total; devices/mounts may overlap. Network counters are
  cumulative and are not represented as bandwidth throughput.

## Verification

Run from the repository root:

```sh
make lint
make check
```

Review desktop, tablet and phone viewport screenshots as part of the UI e2e
suite. Test a session with forbidden capabilities, a host with unavailable
tooling, corrupted localStorage, and a missing broker. No host write operation
is required for dashboard validation.
