# 2. Runtime theme format and tokens

The implemented v1 contract supersedes the earlier planning draft. The owner authorized the remaining frontend and TDK on 2026-10-25. See [implementation handoff](11-IMPLEMENTED-UI-AND-TDK.md), [runtime schema](../../schemas/theme/v1.json) and [TDK author guide](../../sdk/theme/README.md).

A `.hltheme` is one UTF-8 JSON document, at most 64 KiB:

```json
{
  "schemaVersion": 1,
  "id": "local.ocean",
  "name": "Ocean",
  "version": "1.0.0",
  "license": "MIT",
  "base": "midnight",
  "colors": { "accent-base": "#4ED4C8" }
}
```

`base` is `midnight | daybreak | graphite | oled`; omitted colours inherit from that built-in palette. No remote resolution or installed-theme inheritance exists. `colors` allow-lists the 57 semantic HCIS roles from `hcis/tokens/tokens.json`, excluding raw palette tokens and the 12 M1 aliases. `surface-raised` maps to `--hc-color-surface-raised`, for example. The TDK lists all roles with `node sdk/theme/tdk.mjs tokens`.

Values are `#RRGGBB` or `#RRGGBBAA`. Data surfaces and glass fallback must be opaque. Contrast is calculated after resolution/compositing; failing key pairs block activation. Schema validation alone cannot check contrast or duplicate keys; use the shared validator.

Themes do not carry typography, fonts, type sizes, density, spacing, navigation, layout, motion settings or behavior. These remain application preferences and component tokens. The earlier draft allowed some of these fields; it was never a runtime format. Importing that draft now fails with a clear unsupported-key error rather than silently changing meaning.

`schemaVersion` versions the format; `version` versions the author's document. Unknown fields and newer versions are rejected. Breaking changes require a new schema and an explicit migration. No published legacy runtime theme format requires a migration today. The schema, examples and TDK are MIT licensed.
