# Homelab Control Extensions

This is a **build-time import repository** for Homelab Control. Core maintainers
import a reviewed, pinned commit and root tree using a subtree, then statically
compile approved UI contributions. This bootstrap does not change core or
perform that import. It provides no runtime downloader, plugin-code loader,
installer, broker operation or host permission.

The labelled [read-only sample](plugins/sample-readonly/README.md) contains a
declarative manifest and an exported React component. The consuming core keeps
its explicit contribution allow-list and browser-local enable/disable behavior.
The component requires the consuming app's React dependency and existing panel
styles; this repository is not a standalone app.

## Provenance and licences

Bootstrap source: `KRDucky/Homelab-Control` commit
`dd50867c9792a62f0a90c6588e86f41ed97d0afc`.
`LICENSE.md` and the three files in `LICENSES/` are copied verbatim from core.
The sample manifest is copied verbatim; its README is adapted for this layout.
`SampleOverviewCard` is extracted from `frontend/app/src/plugin-ui.tsx`, with
its function body unchanged, its SPDX attribution preserved, a type-only React
import and a named export for static consumption.

GPL-3.0-or-later is the default. MIT and Apache-2.0 are permitted where recorded.
`REUSE.toml` maps only the sample manifest and sample README to MIT; the React
component is GPL-3.0-or-later. See [CONTRIBUTING.md](CONTRIBUTING.md) for DCO and
licensing requirements.

## Validation

With Node.js 22+ and a checkout of core at the source revision above:

```sh
reuse lint
node /path/to/Homelab-Control/sdk/plugin/tdk.mjs validate plugins/sample-readonly/manifest.hlplugin --json
```

The bootstrap includes the permitted Apache-2.0 text for future explicitly
recorded contributions, but no bootstrap file uses it. REUSE 6.3 reports that
text as an unused licence; all bootstrap files have copyright and licence
coverage. Do not assign a different licence just to suppress that diagnostic.

Record the final Extensions commit SHA and `git rev-parse HEAD^{tree}` when
handing off a pinned subtree import. Core integration is a separate task.
