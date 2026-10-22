# Meter

A 24-segment capacity meter with label, percentage and detail line.

**Status: Production.** Ported from the running app; behaviour matches the source, restyled to HCIS tokens.

Source: `frontend/app/src/Meter.tsx`

## When to use

Use for any used/total ratio (CPU, RAM, filesystem, pool). Supply `label`, `used`, `total`, `detail`. Unknown ratios render empty and say “Unknown”.

## Rules

- Thresholds default to warn 75%, crit 90% (from format.ts); override per resource.
- Above a threshold a StatusPill (High / Critical) is added so colour is never the only signal.

## Current implementation vs HCIS

App: 12px segments with 3px gaps. HCIS: 10px segments (6px `compact`), 2px gaps, status fills checked at 3:1 on `meter-track`.

Props: `MeterProps` in `reference/index.d.ts`. Preview data is labelled “Example data”.
