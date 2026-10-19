# 4. Dashboard and widget architecture (planning)

## 4.1 Model
A **dashboard** is a layout document, separate from the theme and from data:
```json
{
  "schemaVersion": 1,
  "id": "home",
  "name": "Home",
  "grid": { "columns": 12, "rowHeight": 72 },
  "breakpoints": { "md": { "columns": 8 }, "sm": { "columns": 4 } },
  "widgets": [
    { "id": "w1", "type": "core.host-summary", "x": 0, "y": 0, "w": 12, "h": 2 },
    { "id": "w2", "type": "core.cpu", "x": 0, "y": 2, "w": 4, "h": 3, "config": { "window": "15m", "refresh": "5s" } },
    { "id": "w3", "type": "core.memory", "x": 4, "y": 2, "w": 4, "h": 3 },
    { "id": "w4", "type": "core.services", "x": 8, "y": 2, "w": 4, "h": 5, "config": { "filter": "failed" } }
  ]
}
```
Stored server-side per user (M2 settings store, SQLite per ADR 0004) with a built-in default; until then,
only the built-in default dashboard exists. Layouts are validated against a schema like themes are.

## 4.2 Widget contract
```ts
export interface WidgetDefinition<Config> {
  type: string;                         // 'core.cpu' — namespaced; plugins use their own prefix (M3)
  title: string;
  requiredScopes: string[];             // widget hidden (with reason) when the session lacks a scope
  requiredCapability?: string;          // from /api/v1/capabilities; shows 'unavailable' otherwise
  sizes: { min: [w: number, h: number]; default: [number, number]; max?: [number, number] };
  configSchema: JSONSchema;             // validated config; no free-form code
  defaultConfig: Config;
  component: React.ComponentType<WidgetProps<Config>>;
}
export interface WidgetProps<Config> {
  config: Config;
  size: { w: number; h: number };
  refresh: RefreshPolicy;               // from 05-TELEMETRY-AND-STATE
}
```
- Widgets read data only through the shared data layer (`05`), so two CPU widgets share one stream.
- Each widget is wrapped in an error boundary: a crashing widget shows its error, not a blank dashboard.
- Off-screen widgets pause their subscriptions (IntersectionObserver).
- Plugin widgets (M3) are declared in the plugin manifest and rendered only from the plugin's static,
  reviewed UI bundle — never from remote code; they get the same scope and capability gating.

## 4.3 Editing
"Edit layout" toggles a mode with drag/resize handles and a widget catalogue. Keyboard alternative for
every drag: select a widget, then arrow keys move it and Shift+arrows resize it, with a live-region
announcement of the new position. Changes are a draft until **Done**; **Reset to default** is always
available.

## 4.4 Core widgets (initial set, read-only)
Host summary, CPU, memory, filesystems usage, network throughput, services (failed / all), hardware
identity, legacy Webmin status, recent audit events (M2), background operations (M2).
