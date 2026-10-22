# GlobalHeader

Identity, active host, health status, workspace, global search, background jobs, notifications, appearance and account.

**Status: Proposed (HCIS).** New in the HCIS specification; a reference design, not in the app.

Source: `docs/ui/06 §6.1`

## When to use

Rendered by AppShell. The health chip always shows a word (“2 critical”, “Not checked”), never colour alone.

## Rules

- The search field opens the CommandPalette; Ctrl/Cmd+K anywhere does the same.

Props: `GlobalHeaderProps` in `reference/index.d.ts`. Preview data is fictional and labelled “Example data”.
