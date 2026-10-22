# PermissionEditor

An owner/group/others × read/write/execute matrix with the octal and symbolic mode.

**Status: Proposed (HCIS).** Not in the app or the planning docs yet; a reference design for future work.

Source: none in the repository.

## When to use

Use wherever a mode is set (shares, Safe Permissions, file browser). Controlled via `value` / `onChange` with a 4-digit octal string.

## Rules

- Flags world-writable modes with a warning pill.

Props: `PermissionEditorProps` in `reference/index.d.ts`. Preview data is labelled “Example data”.
