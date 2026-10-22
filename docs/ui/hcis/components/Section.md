# Section

A titled panel that always shows where its data came from, says when data is partial or unavailable, and now shows its live state.

**Status: Production.** Ported from the running app; behaviour matches the source, restyled to HCIS tokens.

Source: `frontend/app/src/ui.tsx`

## When to use

Use for every block of host data. Supply `title`, `meta` (status, sources, errors, notes) and children. Pass `live` (`{state, updated, onRetry}`) for data that refreshes; pass `glass` only for summary panels.

## Rules

- Never hide a panel because its data is missing: `status: "unavailable"` plus a note.
- Stale and disconnected data dims in place (`hc-stale`) and keeps its layout, focus and scroll; it never blanks out.

## Current implementation vs HCIS

App: `.panel`, no live state. HCIS adds `live`, `glass`, `level`, and the HCIS surface tokens.

Props: `SectionProps` in `reference/index.d.ts`. Preview data is fictional and labelled “Example data”.
