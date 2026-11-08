# Read-only sample plugin (fixture/example — not a live host plugin)

This directory is the labelled sample for the M3 plugin SDK first checkpoint,
prepared for build-time import into Homelab Control. It demonstrates the
declarative `.hlplugin` manifest and a statically compiled UI contribution; it is
not a live host plugin and is not registered in `modules.json`.

- `manifest.hlplugin` is one declarative JSON document, licensed MIT. Its fields
  are limited to `schemaVersion`, `id`, `name`, `version`, `license` and an
  allow-list of `contributions`. It has no executable payload.
- `ui/SampleOverviewCard.tsx` exports the GPL-3.0-or-later React component for
  static consumption. It preserves the card from core's `plugin-ui.tsx`,
  including its JSX, labels and IDs. This README is MIT; neither it nor the
  manifest's MIT metadata relicenses the component.
- The only contribution ID is `overview.sample-card`. In the consuming core it
  maps to already-compiled React: loading shows the labelled Overview card and
  unloading hides it. Enable/disable remains browser-local (localStorage key
  `hc.plugins.v1`). This repository adds no API route, scope, database row or
  broker operation. The card is static fixture content, not live host data.

Validate from this repository with Node.js 22+ and the core checkout:

```sh
node /path/to/Homelab-Control/sdk/plugin/tdk.mjs validate plugins/sample-readonly/manifest.hlplugin --json
```

See core's `sdk/plugin/README.md`, `schemas/plugin/v1.json` and
`docs/m3/PLUGIN-SDK-SKELETON.md`. Source revision and extraction provenance are
recorded in this repository's root README.
