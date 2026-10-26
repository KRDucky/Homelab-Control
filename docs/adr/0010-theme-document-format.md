# ADR-0010: Theme document format (`.hltheme`)

**Status:** **Accepted in principle** (2026-10-16, owner; retained at the PR #5 final review the same day,
including the separation of navigation/layout preferences from appearance themes). Frontend implementation and the TDK authorized by the owner on 2026-10-25; browser-local scope implemented in the current change. Server persistence and host-operation work remain separate.
**Date:** 2026-10-16
**Refines:** `docs/project/06-THEME-AND-PLUGIN-SDK.md`, which described `.hltheme` as an archive.

## Decision
1. A theme is **one JSON document** (`.hltheme`, media type `application/vnd.homelab-control.theme+json`)
   validated against `schemas/theme/v1.json` (the runtime format; the original draft, `docs/ui/theme.schema.v1.draft.json`, is historical, see the 2026-10-25 refinement below). No
   archive, so no zip-slip, zip-bomb or symlink handling exists to get wrong.
2. Themes contain only **appearance**. As implemented in runtime v1 that means colours only (see the 2026-10-25 refinement); the originally listed typography, spacing, shape, elevation and motion groups were not adopted. **Layout and navigation preferences** (sidebar style, dashboard
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

## Frontend refinement (2026-10-25)

The implemented schema is `schemas/theme/v1.json`; the previous draft was a planning artifact. V1 has a built-in base and 57 semantic colour overrides, no arbitrary installed-theme inheritance and no typography/spacing/motion/layout fields. This preserves the shipped Step 3 promise that changing theme does not change layout, fonts or accessibility preferences. Import/export, Studio, timed preview and the MIT Theme Development Kit run entirely in the browser/author tools; no server storage or new permission is implied. See `docs/ui/11-IMPLEMENTED-UI-AND-TDK.md` and `sdk/theme/README.md`.

## Design review confirmation (2026-10-26)

The final HCIS design review re-evaluated the colours-only decision and kept it: user font, text-size, density and motion preferences hold in every theme, and the import surface stays a flat set of hex colours. A richer format would need a new schema version and a new ADR. See `docs/ui/12-FINAL-DESIGN-REVIEW-AND-HANDOFF.md` §5.
