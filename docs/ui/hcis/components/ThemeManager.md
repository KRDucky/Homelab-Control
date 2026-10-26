# ThemeManager / Theme Studio

**Status: implemented in the current UI continuation (2026-10-25).** Appearance → Themes & Theme Studio; source `frontend/app/src/pages/ThemeStudio.tsx`.

Browser-local themes, validated import/export, metadata, isolated component preview, editing and saved-version recovery. A 15-second application trial auto-reverts unless kept; Escape reverts immediately. Input grammar and key contrast checks block unsafe activation without claiming a full accessibility audit. Colours are independent from typography, density, motion and layout preferences.

Runtime format: `schemas/theme/v1.json`. User tooling: `sdk/theme/README.md`. Server-wide storage, server RBAC and server theme audit are future work. See `../../03-THEME-STUDIO.md` and `../../11-IMPLEMENTED-UI-AND-TDK.md`.
