# Dropdown

A menu button that opens a list of actions.

**Status: Planned.** Listed in the repository's planning docs but not implemented in the app yet.

Source: `docs/ui/01-DESIGN-SYSTEM.md §1.3 (Menu)`

## When to use

Use for secondary or overflow actions. Items take label, icon, shortcut, danger. Pass `"-"` for a separator.

## Rules

- Arrow keys move, Escape closes and returns focus.
- Destructive items are marked `danger` and should open a ConfirmDialog, never act directly.

Props: `DropdownProps` in `reference/index.d.ts`. Preview data is labelled “Example data”.
