# 1. Original design system and component architecture (planning)

## 1.1 Visual direction
An instrument-panel aesthetic: calm surfaces, one accent, status colour reserved for status, dense but
legible tables, large readable figures for the few numbers that matter (the M1 "faceplate" hostname is
the seed). It must read as its own product: no Webmin, Authentic, Cockpit, Unraid or TrueNAS layouts,
icons or palettes are referenced or copied. Icons are an original SVG set drawn on a 20 px grid with a
1.5 px stroke, shipped as a sprite and coloured with `currentColor` (no icon font, no third-party set).

## 1.2 Layers
| Layer | Location (planned) | Rules |
|---|---|---|
| Tokens | `frontend/design-system/tokens/` (generated CSS from the built-in theme JSON) | Only source of visual values |
| Primitives | `frontend/design-system/primitives/` | `Box`, `Stack`, `Grid`, `Text`, `Icon`, `VisuallyHidden`; style only via tokens |
| Components | `frontend/design-system/components/` | Accessible, stateless where possible; no data fetching |
| Patterns | `frontend/app/src/patterns/` | `DataTable`, `ResourceHeader`, `OperationBanner`, `EmptyState`, `CapabilityGate` |
| Screens | `frontend/app/src/pages/` | Compose patterns; own data via hooks (`05-TELEMETRY-AND-STATE.md`) |

Dependency direction is one-way (screens → patterns → components → primitives → tokens). A lint rule
rejects hard-coded colours, lengths and durations in `components/` and `patterns/` (the M1 stylesheet
already has none). Styling stays plain CSS with custom properties: no CSS-in-JS runtime.

## 1.3 Component inventory (v1)
Button, IconButton, Link, TextField, NumberField, Select, Combobox, Checkbox, Switch, RadioGroup,
SegmentedControl, Tabs, Disclosure, Dialog, Drawer, Popover, Menu, Tooltip, Toast, Badge, StatusPill,
Meter, Sparkline, Spinner, Skeleton, ProgressBar, Table (with `DataTable` pattern), Pagination,
Breadcrumbs, KeyValueList, CodeBlock, Banner, Card, Toolbar, CommandPalette.

Native elements first (`<button>`, `<dialog>`, `<details>`, `popover` attribute, `<select>` where it
suffices). Custom widgets follow the WAI-ARIA Authoring Practices patterns for keyboard and roles.

## 1.4 Proposed component interfaces (TypeScript, illustrative)
```ts
// Status is semantic; components never take raw colours.
export type Tone = 'neutral' | 'accent' | 'ok' | 'warn' | 'crit' | 'info' | 'unknown';
export type Density = 'compact' | 'standard' | 'spacious';

export interface ButtonProps {
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger';
  size?: 'sm' | 'md';
  icon?: IconName;
  busy?: boolean;                 // shows spinner, sets aria-busy, keeps width stable
  disabledReason?: string;        // rendered as accessible description, not just greyed out
  onClick?: () => void;
  children: React.ReactNode;
}

export interface StatusPillProps {
  tone: Tone;
  label: string;                  // always text: colour is never the only signal
  detail?: string;
}

export interface MeterProps {
  label: string;
  value: number; max: number;
  thresholds?: { warn: number; crit: number };
  format?: (v: number) => string; // e.g. bytes
}

export interface DataTableColumn<Row> {
  id: string;
  header: string;
  cell: (row: Row) => React.ReactNode;
  sortValue?: (row: Row) => string | number;
  filter?: 'text' | 'enum';
  width?: 'auto' | 'narrow' | 'wide';
  hideBelow?: 'sm' | 'md';         // responsive column hiding
}
export interface DataTableProps<Row> {
  rows: readonly Row[];
  columns: readonly DataTableColumn<Row>[];
  rowKey: (row: Row) => string;
  caption: string;                 // required: tables are always labelled
  density?: Density;
  emptyState?: React.ReactNode;
  virtualizeAbove?: number;        // default 500 rows
  selection?: { mode: 'none' | 'single' | 'multi'; onChange: (keys: string[]) => void };
  rowActions?: (row: Row) => Action[];
}

// Actions are data, so the same list feeds buttons, row menus and the command palette.
export interface Action {
  id: string;
  label: string;
  icon?: IconName;
  scope: string;                   // API scope required; hidden when the session lacks it
  kind: 'navigate' | 'read' | 'operation';  // 'operation' = M2+, always goes through a plan/confirm flow
  run: () => void | Promise<void>;
  shortcut?: string;
}

export interface OperationBannerProps {   // background operation tracking (M2+ jobs API)
  operationId: string;
  title: string;
  state: 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled';
  progress?: { done: number; total: number };
  onOpen: () => void;
}
```
`Action.scope` only hides what the server would refuse anyway; authorization stays server-side.

## 1.5 States every component must define
Default, hover, focus-visible, active, disabled (with reason), busy, error, empty, and — for data-bound
patterns — loading (skeleton), stale (data older than its refresh interval, shown with age), and
unavailable (capability missing, with the explanation from `/api/v1/capabilities`).
