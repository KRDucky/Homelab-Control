# CommandPalette

**Status: implemented in the current UI continuation (2026-10-25).** Source: `frontend/app/src/CommandPalette.tsx`.

Header Search pages or Ctrl/Cmd+K opens a native modal with combobox/listbox navigation. Arrow keys move the active result, Enter navigates, Escape closes; focus returns on dismissal and moves to main after navigation. The initial focus is the search input.

Only available/degraded capability pages, own Sessions and browser-local Theme Studio are indexed. No root command, mutation or resource-search endpoint is implied. Future resource/operation search must use its reviewed backend contracts and normal plan/confirm flow. See `../../11-IMPLEMENTED-UI-AND-TDK.md`.
