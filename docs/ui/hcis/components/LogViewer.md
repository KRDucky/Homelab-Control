# LogViewer

A filterable, followable log pane in JetBrains Mono.

**Status: Proposed (HCIS).** Not in the app or the planning docs yet; a reference design for future work.

Source: none in the repository.

## When to use

Use for journald, container and job output. Filter by text and level; Follow keeps the newest line in view.

## Rules

- Line count is announced politely; the pane is keyboard-scrollable.

Props: `LogViewerProps` in `reference/index.d.ts`. Preview data is labelled “Example data”.
