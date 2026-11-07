# Read-only sample plugin (fixture/example — not a live host plugin)

This directory is the in-tree **labelled sample** for the M3 plugin SDK first checkpoint. It exists to
prove the declarative `.hlplugin` manifest format and the compiled-in UI contribution path; it is
**not** a live host plugin and it is **not** registered in `modules.json`.

- `manifest.hlplugin` — one declarative JSON document (MIT). Fields are limited to `schemaVersion`,
  `id`, `name`, `version`, `license` and an allow-list of `contributions`. There is no archive,
  image, font, CSS, script or binary payload.
- The only known contribution ID is `overview.sample-card`. It maps to already-compiled React in the
  app: when loaded, the Overview page shows a labelled sample card; unloading hides it. Enable/disable
  is browser-local only (localStorage key `hc.plugins.v1`); there is no API route, scope, database row
  or broker operation.
- This manifest is a **fixture/example**: its copy says so, and it never executes plugin code.

Validate it with the MIT validator:

```sh
node sdk/plugin/tdk.mjs validate plugins/sample-readonly/manifest.hlplugin
```

See `sdk/plugin/README.md`, `schemas/plugin/v1.json` and `docs/m3/PLUGIN-SDK-SKELETON.md`.
