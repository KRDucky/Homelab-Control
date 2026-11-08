# ADR-0013: Plugin isolation and declarative contributions

**Status:** **Proposed** 2026-11-07 (M3 plugin SDK first checkpoint). Not accepted. Not authorization
for plugin loading, a plugin marketplace, or any privileged host adapter.
**Date:** 2026-11-07
**Related:** ADR 0001 (privilege boundary; hc-api does not spawn), ADR 0010 (theme document format),
D-10 (first-party modules stay compiled-in), `docs/m3/PLUGIN-SDK-SKELETON.md`,
`docs/project/06-THEME-AND-PLUGIN-SDK.md`.

## Context

Spec 02 lists plugin isolation as an unwritten ADR for M3. The M3 acceptance gate pairs a theme SDK
(install/preview/switch/remove, already shipped browser-local in PR #24) with a plugin SDK: load/unload
a read-only sample plugin. The first checkpoint must prove the plugin model without introducing an
executable-payload path.

## Decision (first checkpoint)

1. A plugin is **one declarative JSON document** (`.hlplugin`, media type
   `application/vnd.homelab-control.plugin+json`) validated against `schemas/plugin/v1.json`. Metadata
   (`schemaVersion`, `id`, `name`, `version`, `license`) plus an allow-list of known `contributions`.
   No archive, image, font, CSS, script or binary payload.
2. **Contributions are allow-listed and compiled-in.** A contribution ID maps to already-compiled React
   in the app (`frontend/app/src/plugin-ui.tsx`); unknown IDs are refused at parse time and never mount
   UI. No `eval`, no dynamic `import()` of user files, no Go `plugin` package, no spawn.
3. **Parsing rejects duplicates and prototype pollution** with a dedicated scanner (no `JSON.parse`
   reviver), shared between app and SDK (`frontend/app/src/plugin.ts` → generated `sdk/plugin/runtime.mjs`).
4. **Enable/disable is browser-local** (new `hc.plugins.v1` key). No API route, scope, `state.db` row or
   broker operation. The sample is not registered in `modules.json`; `TestRegistryIsClosed` stays
   `{services}` only.
5. **First-party modules stay compiled-in** (D-10). This sample requests no host operations.

## Consequences

- Third-party plugin bundles, signing, a marketplace, and isolated helper processes need separate
  review and a new ADR (archive safety, provenance, crash isolation) before they exist.
- Privileged host adapters remain broker ops reviewed separately (ADR 0001); plugins never gain
  `plugins.read` / `plugins.configure` scopes in this checkpoint.
- A richer manifest (routes, widgets, config schema) requires a new schema version and an explicit
  migration.
