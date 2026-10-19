# 5. Live telemetry and state management (planning)

## 5.1 Today (M1)
Each page calls `useData(fetcher, { intervalMs })`: independent polling, no sharing, no cache, no
back-off. Fine for four pages; not for dashboards with many widgets.

## 5.2 Proposed client data layer (no new dependency)
A small, purpose-built store (~300 lines, `frontend/app/src/data/`) instead of a general library:
- **Resources keyed by API path** (`/api/v1/host/overview`). One in-flight request per key; components
  subscribe with `useResource(key, policy)` built on React's `useSyncExternalStore`.
- **Stale-while-revalidate:** cached value shown immediately with its age; refreshed in the background.
- **Refresh policy** per subscription: `{ intervalMs, pauseWhenHidden: true, pauseWhenOffscreen: true }`.
  The effective interval for a key is the shortest among active subscribers; zero subscribers = stopped.
- **Back-off** on errors (×2 up to 60 s) and on `429`; `401` ends the session as today.
- **Mutations (M2+)** never update the cache optimistically for privileged operations: the UI shows the
  operation's state from the jobs API, then refetches affected resources.

## 5.3 Live streams (server work, M2+)
The architecture spec already chooses **Server-Sent Events** for status streams (WebSockets only for
terminal-like use). Proposed, for a later milestone and subject to the M2 security review:
- `GET /api/v1/stream/metrics?series=cpu,memory,net&interval=2s` — `text/event-stream`, same session
  cookie and scopes as the REST endpoints; server enforces minimum interval (≥ 1 s) and a per-session
  stream cap.
- Events carry `{ series, t, value }` batches; the client keeps a fixed-size ring buffer per series
  (e.g. 15 minutes at the chosen interval) for sparklines and charts.
- Fallback to polling when SSE is blocked by a proxy.
The unprivileged API serves all telemetry; the broker is not involved for metrics readable without root.

## 5.4 User-configurable refresh
Global setting (Off · 2 s · 5 s · 15 s · 60 s; default 5 s) with per-widget override; "Pause live
updates" toggle in the header. Hidden tabs pause automatically. Reduced-data mode lowers rates for slow
remote links.

## 5.5 UI state
URL holds navigational state (page, filters, sort, selected resource) so views are shareable and the
back button works (M1 already routes this way). Local component state for ephemeral UI. User preferences
(theme, density, motion, refresh) in the server settings store (M2), mirrored to `localStorage` only as a
cache for first paint.
