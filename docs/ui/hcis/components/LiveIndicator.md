# LiveIndicator

Reactive state of a data source: Live, Refreshing, Stale, Paused, Disconnected.

**Status: Proposed (HCIS).** New in the HCIS specification; a reference design, not in the app.

Source: none in the repository.

## When to use

Put it in a Section header or page header. Disconnected offers Retry.

## Rules

- Announced politely only when the state changes, not on every update.
- Pair Stale/Disconnected with dimmed content (`hc-stale`) instead of clearing it.

Props: `LiveIndicatorProps` in `reference/index.d.ts`. Preview data is fictional and labelled “Example data”.
