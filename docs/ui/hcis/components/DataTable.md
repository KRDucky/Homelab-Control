# DataTable

The dense, sortable, selectable table for every resource list. In containers narrower than 560px each row becomes a labelled card.

**Status: Production markup, proposed component.** The app renders this today as markup + `styles.css` classes; HCIS packages it as a reusable component. Not yet a component in the app.

Source: `frontend/app/src/pages/Services.tsx; docs/ui/01 §1.4 DataTableProps`

## When to use

Supply `caption`, `columns`, `rows`, `rowKey`. `selectable` + `bulkActions` for multi-row operations, `rowTone` to tint failed rows, `onRowClick` + `activeKey` to drive a DetailPanel. Pair with `FilterBar`.

## Rules

- Sorting is the spec's SortableColumn: every column sorts unless `sortable: false`; headers carry aria-sort.
- Selection survives live refreshes: keys that still exist stay selected.
- Never glass. Tables sit on opaque `surface-raised`.
- Virtualize lists over ~500 rows (docs/ui/01).

## Current implementation vs HCIS

App: plain table. HCIS: sticky header, sort, selection, bulk bar, stacked narrow layout.

Props: `DataTableProps` in `reference/index.d.ts`. Preview data is fictional and labelled “Example data”.
