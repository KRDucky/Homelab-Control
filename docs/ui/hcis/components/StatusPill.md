# StatusPill

A tinted label with an icon that names a state.

**Status: implemented in the current UI continuation (2026-10-25), `frontend/app/src/ui.tsx`.**

Source: `docs/ui/01-DESIGN-SYSTEM.md §1.4 (StatusPillProps)`

## When to use

Use for any resource state. `label` is required text; the icon shape differs per tone (check, triangle, octagon, circle-i).

## Rules

- Tone colours come from `status-*-subtle` grounds with `status-*-text` words (4.5:1 in every theme).

Props: `StatusPillProps` in `reference/index.d.ts`. Preview data is labelled “Example data”.
