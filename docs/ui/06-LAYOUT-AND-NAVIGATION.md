# 6. Responsive layouts and navigation (planning)

## 6.1 Shell
Header (host name, global status, operations indicator, search/command, user) · sidebar navigation ·
content. Breakpoints are content-driven, expressed in `rem`:

| Width | Navigation | Content |
|---|---|---|
| ≥ 75 rem (desktop) | Expanded sidebar (user may choose rail, §6.5) | Multi-column; tables show all columns |
| 48–75 rem (tablet) | Icon rail with labels on focus/hover | Two columns; low-priority columns hide (`hideBelow`) |
| < 48 rem (phone) | Bottom-sheet menu from a header button | Single column; tables become stacked rows with the key column first |

Container queries size widgets and cards by their own width, so a widget looks right in any dashboard
slot. No page-level horizontal scroll at any width (already an M1 e2e check).

## 6.2 Information architecture (homelab-first, directive §1)
Overview · Storage (pools, disks, shares) · Applications (containers, Compose) · Services · Network
(interfaces, firewall, reverse proxy) · Users & access · Updates · Hardware · Logs & audit · Settings.
Sections appear only when their capability exists on this host, as in M1; unavailable ones explain why
in-place. Legacy Webmin is a link under Settings → Integrations, not a primary section.

## 6.3 Command palette
`Ctrl/Cmd+K` or `/`: searches navigation targets, resources (by stable id and name) and `Action`s the
session is allowed to run (`01` §1.4). Fuzzy match, recent items first, fully keyboard-driven, ARIA
combobox pattern. Operations launched from the palette open their normal plan/confirm flow — the palette
is never a shortcut around confirmation.

## 6.4 Contextual actions
Each resource page has one toolbar of actions; the same `Action` list appears in row menus and the
palette. Destructive or privileged actions (M2+) are visually separated and always lead to a preview.

## 6.5 Layout preferences (separate from themes)
Navigation style, density and dashboard layout are **user preferences**, not theme content (ADR 0010).
A theme never changes where things are or how much fits on screen; a user's layout survives switching
themes, and a shared theme cannot rearrange anyone's interface.

```ts
export interface LayoutPreferences {
  schemaVersion: 1;
  navigation: { style: 'expanded' | 'rail' | 'hidden'; position: 'left' | 'top' };
  density: 'compact' | 'comfortable' | 'spacious';
  dashboard: string;                 // id of the user's active dashboard layout (04 §4.1)
  refresh: { defaultMs: 0 | 2000 | 5000 | 15000 | 60000; paused: boolean }; // 05 §5.4
}
export interface AppearancePreferences {
  themeId: string;                   // installed theme
  mode: 'auto' | 'dark' | 'light';
  contrast: 'auto' | 'standard' | 'high';   // auto follows prefers-contrast
  motion: 'auto' | 'full' | 'reduced' | 'none'; // auto follows prefers-reduced-motion
}
```
Both are stored per user in the settings store (M2; until then per browser only) and validated against
their own schemas. Density maps to fixed spacing multipliers chosen by the engine, not by themes.
