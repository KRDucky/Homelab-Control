# DataTable

**Status: implemented in the current UI continuation (2026-10-25).** Source: `frontend/app/src/DataTable.tsx`.

Typed columns provide `id`, `label`, `render`, optional `sortValue` and `rowHeader`. Supply caption, rows and a stable rowKey. Native table/row semantics and headers persist in labelled cards below 560px container width. Desktop has sticky sortable headers; narrow cards have a visible sort selector so hidden headers never hide focused controls.

Default pagination is 100 rows, bounded to 500, replacing the earlier proposed virtualizer dependency. Sorting is numeric for numbers and natural text for strings, and only exists when a column supplies `sortValue`. Audit keeps server order. Selection uses stable keys, survives refreshed objects, and passes currently present selected keys to bulk-action slots. Selection never grants an operation permission.

Existing Overview, Services, Hardware, Audit and Sessions use the shared component. Tables remain opaque. The dedicated log view retains its own scroll region. See `../../11-IMPLEMENTED-UI-AND-TDK.md`.
