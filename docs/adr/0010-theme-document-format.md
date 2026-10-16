# ADR-0010: Theme document format (`.hltheme`)

**Status:** **Accepted in principle** (2026-10-16, owner). Implementation (theme engine, Theme Studio) is
**not** authorized yet; it follows the phased plan in `docs/ui/10-PHASED-PLAN.md`.
**Date:** 2026-10-16
**Refines:** `docs/project/06-THEME-AND-PLUGIN-SDK.md`, which described `.hltheme` as an archive.

## Decision
1. A theme is **one JSON document** (`.hltheme`, media type `application/vnd.homelab-control.theme+json`)
   validated against `theme.schema.v1.json` (draft: `docs/ui/theme.schema.v1.draft.json`, MIT). No
   archive, so no zip-slip, zip-bomb or symlink handling exists to get wrong.
2. Themes contain only **appearance**: typed values for known tokens (colour, typography, spacing,
   shape, elevation, motion) and modes. **Layout and navigation preferences** (sidebar style, dashboard
   layouts, refresh rates) are user preferences stored separately and never travel in a theme.
3. Parsing rejects duplicate keys and prototype-polluting keys with a **dedicated parser**, not
   `JSON.parse` revivers (which cannot see duplicates); see `docs/ui/09-THEME-SECURITY.md` §9.2.
4. Themes are applied by setting allow-listed CSS custom properties through the CSSOM
   (`style.setProperty`), which works under the existing CSP without `'unsafe-inline'`; verified in
   Chromium, Firefox and WebKit (`tests/e2e/theme_cssom_csp.py`).
5. Anything needing code is a plugin, never a theme.

## Consequences
- Bundled assets (images, fonts) cannot be part of a theme; adding them later needs a new ADR covering
  archive safety and signing.
- The schema and example themes are MIT so third parties can build themes freely.
