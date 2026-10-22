# MetricChart

A line chart for time series with a hover readout and a table view.

**Status: Planned.** Listed in the repository's planning docs but not implemented in the app yet.

Source: `docs/ui/04-DASHBOARD-WIDGETS.md, 07 §7.1 (charts offer a table view)`

## When to use

Use for monitoring (CPU, I/O, network). Up to 6 series in `chart-1…6` order.

## Rules

- The Table toggle shows the same data as an accessible table.
- No animation; live updates redraw in place.

Props: `MetricChartProps` in `reference/index.d.ts`. Preview data is labelled “Example data”.
