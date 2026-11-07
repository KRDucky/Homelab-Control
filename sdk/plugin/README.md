# Homelab Control Plugin Development Kit (PDK)

PDK v1 validates declarative read-only plugin manifests (`.hlplugin`) without installing the
application or any npm dependencies. Requires Node.js 22 or newer. The SDK, schema and examples are
MIT licensed; see [LICENSE](LICENSE).

## What a plugin is (this checkpoint)

A `.hlplugin` is **one JSON document** of metadata plus an allow-list of known contribution IDs. It is
the plugin half of the M3 SDK first checkpoint; the theme half is the TDK in [`sdk/theme/`](../theme/).

```json
{
  "schemaVersion": 1,
  "id": "dev.homelab-control.sample-readonly",
  "name": "Read-only sample plugin",
  "version": "1.0.0",
  "license": "MIT",
  "contributions": ["overview.sample-card"]
}
```

There is **no archive, image, font, CSS, script or binary payload**, and no route, scope, broker
operation or helper process. Contribution IDs map to **already-compiled** React in the app; the only
known ID this checkpoint is `overview.sample-card` (a labelled Overview sample card). Unknown IDs are
refused at parse time and can never mount UI.

## Use the CLI

From the repository root:

```sh
node sdk/plugin/tdk.mjs validate plugins/sample-readonly/manifest.hlplugin
node sdk/plugin/tdk.mjs validate plugins/sample-readonly/manifest.hlplugin --json
node sdk/plugin/tdk.mjs contributions
node sdk/plugin/tdk.mjs create my-plugin.hlplugin local.example "My plugin"
```

`validate` exits 0 for a valid manifest and 1 for malformed input. `contributions` lists the known
contribution IDs. `create` writes a valid starter manifest and refuses to overwrite an existing file.

The file itself is the package: UTF-8 JSON, maximum 64 KiB. Keep the `.hlplugin` extension. The media
type is `application/vnd.homelab-control.plugin+json`.

## Format and validation

The normative schema is [schemas/plugin/v1.json](../../schemas/plugin/v1.json). Metadata is plain
text: a reverse-DNS-style `id`, a numeric `major.minor.patch` version, a simple SPDX `license`
identifier and a non-empty `contributions` allow-list. The strict parser rejects duplicate decoded
keys, prototype-polluting keys (`__proto__`, `constructor`, `prototype`), malformed JSON, unpaired
Unicode surrogates, excessive nesting, more than 2,000 values, oversized strings/documents and any
trailing data. There is no `JSON.parse` reviver, so duplicate keys are actually seen. JSON Schema
alone cannot detect duplicate keys, so an editor's green schema check does not replace PDK validation.

## Use the validation library

```js
import { parsePlugin } from './sdk/plugin/runtime.mjs';
import { readFileSync } from 'node:fs';

const plugin = parsePlugin(readFileSync('plugins/sample-readonly/manifest.hlplugin', 'utf8'));
console.log(plugin.id, plugin.contributions);
```

`parsePlugin` throws a useful error for invalid structure. Treat every external file as untrusted and
enforce the byte limit before reading very large inputs. The app and PDK use the **same** parser:
`scripts/build-plugin-sdk.mjs` generates `runtime.mjs` from `frontend/app/src/plugin.ts`.

## For Homelab Control maintainers

The source is `frontend/app/src/plugin.ts` (MIT), shared with the app the same way the TDK shares
`frontend/app/src/theme.ts`. After changing it:

```sh
node scripts/build-plugin-sdk.mjs
node scripts/build-plugin-sdk.mjs --check
node --test sdk/plugin/tdk.test.mjs
```

Generation uses the project's pinned TypeScript compiler; the resulting `runtime.mjs` has no
dependencies. `make lint` checks generated artifacts for drift. A breaking format change requires a
new schema version and an explicit migration; v1 rejects unknown keys rather than guessing.
Browser-local plugin enable/disable does not introduce a backend API, RBAC scope or broker operation.
