# 3. Theme Studio interaction model (planning)

A screen under **Settings → Appearance → Theme Studio**, available to accounts with a new scope
`appearance.manage` (themes are per-installation; per-user preferences such as density, motion and
dark/light choice need only a signed-in session). It edits theme documents (`02`); it never edits CSS.

## 3.1 Layout
```
┌ Toolbar: theme name · version · Undo · Redo · Reset · Validate · Export · Save ┐
├──────────────── Controls (left, scrolls) ───────┬──── Live preview (right) ────┤
│ Mode: [Dark] [Light] [HC dark] [HC light]        │ Real components rendered in  │
│ ▸ Colour  (surface, text, accent, status, chart) │ an isolated preview root:    │
│ ▸ Typography (stack, scale, weights)             │ nav, faceplate, table, form, │
│ ▸ Space & density                                │ dialog, toasts, charts,      │
│ ▸ Shape & elevation                              │ status pills, meters         │
│ ▸ Motion                                         │ [Desktop|Tablet|Phone]       │
│ ▸ Navigation & status style                      │ [Reduced motion] [HC] toggles│
├──────────────── Issues panel (bottom, collapsible) ─────────────────────────────┤
│ ✖ text.secondary on surface.raised = 3.9:1 (needs 4.5:1) [Fix: suggest #9aa8c6] │
└──────────────────────────────────────────────────────────────────────────────────┘
```
- Preview applies the draft tokens to a preview container only, so a broken draft never breaks the
  Studio itself. The rest of the app keeps the active theme until **Save → Activate**.
- Colour controls: hex field + picker + eyedropper (where the browser supports it), and a "derive"
  helper that generates hover/contrast variants which the user can override.
- Every token shows where it is used ("used by: buttons, links, focus ring").

## 3.2 Editing model
- The draft is a theme document held in memory; every change is a command on an undo stack (≥ 200 steps,
  coalescing slider drags). Reset per token, per group, or to the base theme.
- **Advanced → JSON:** a code editor view of the same document, validated as you type against the schema;
  the form and JSON views edit the same draft.
- **Duplicate** creates a new id (`…-copy`) and version `1.0.0`; **Save** bumps the patch version unless
  the user picks minor/major; previous saved versions are kept (last 10) and can be restored.
- **Import** accepts `.hltheme` or `.json`; the file goes through the full validator (`09`) before it is
  even previewed. **Export** produces `.hltheme` (manifest + theme JSON + optional preview PNG generated
  by the Studio).

## 3.3 Validation shown to the user
| Check | Severity | Message style |
|---|---|---|
| Schema violation (type, range, unknown key) | Error — blocks save | Token path + expected type/range |
| Text contrast < 4.5:1 (body) or < 3:1 (large) | Error — blocks activation | Ratio, required ratio, suggested nearest passing colour |
| Non-text contrast < 3:1 (borders of inputs, focus ring, status vs surface) | Error | As above |
| Focus ring < 3:1 against adjacent surfaces | Error | As above |
| Status colours too similar to each other (ΔE < 15) | Warning | Explains colour-only distinctions are avoided anyway |
| Chart series adjacent colours too similar | Warning | Index pair |
| High-contrast mode missing | Info | "Built-in high-contrast set will be used" |

Errors explain what to change, never only "invalid". Saving with warnings is allowed; activation with
errors is not, except for the built-in base, which is tested in CI.

## 3.4 Keyboard and accessibility
All controls reachable by keyboard; undo/redo `Ctrl/Cmd+Z / Shift+Z`; issues panel entries focus the
offending control; preview announces nothing (it is decorative) while the issues list is a live region.
