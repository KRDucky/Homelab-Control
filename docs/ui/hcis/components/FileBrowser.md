# FileBrowser

A folder tree plus a sortable, selectable file table with owner and mode.

**Status: Proposed (HCIS).** Writes, download and content are still not in the app. The read-only port is at `/files` (`frontend/app/src/pages/Files.tsx`); New folder, Permissions and Download stay hidden.

Source: `frontend/app/src/pages/Files.tsx`.

## When to use

Use for browsing bind mounts, shares and datasets. Bulk actions open previews.

## Rules

- Paths, owners and modes are mono.

Props: `FileBrowserProps` in `reference/index.d.ts`. Preview data is labelled “Example data”.
