# StateDot

A small coloured dot that reinforces a state already given in text.

**Status: Production.** Ported from the running app; behaviour matches the source, restyled to HCIS tokens.

Source: `frontend/app/src/ui.tsx (StateDot)`

## When to use

Place before a state word in tables and lists. Supply `tone`.

## Rules

- Always aria-hidden; the adjacent text carries the meaning.
- `pulse` only for transitional states (starting, scrubbing); it stops under reduced motion.

## Current implementation vs HCIS

Same API as the app; adds `info` and `pulse`.

Props: `StateDotProps` in `reference/index.d.ts`. Preview data is labelled “Example data”.
