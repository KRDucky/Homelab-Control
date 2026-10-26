# Homelab Control Theme Development Kit (TDK)

TDK v1 lets you create, validate and share HCIS themes without installing the application or any npm dependencies. Requires Node.js 22 or newer. The SDK, schema, examples and validation runtime are MIT licensed; see [LICENSE](LICENSE).

## Create your first theme

From the repository root:

```sh
node sdk/theme/tdk.mjs create ocean.hltheme local.ocean "Ocean" midnight
node sdk/theme/tdk.mjs tokens
node sdk/theme/tdk.mjs validate ocean.hltheme
```

Edit `ocean.hltheme` in a text editor, or import it into **Appearance → Themes & Theme Studio**. Choose Midnight, Daybreak, Graphite or OLED as the base; omitted colours inherit from that built-in palette. The four files in `examples/` are ready to import.

```json
{
  "schemaVersion": 1,
  "id": "local.ocean",
  "name": "Ocean",
  "version": "1.0.0",
  "license": "MIT",
  "base": "midnight",
  "colors": {
    "accent-base": "#4ED4C8",
    "accent-hover": "#79E2D8",
    "accent-contrast": "#062024"
  }
}
```

The file itself is the package: UTF-8 JSON, maximum 64 KiB. Keep the `.hltheme` extension. There is no archive, image, font, CSS or script payload. The media type is `application/vnd.homelab-control.theme+json`.

## Format and validation

The normative schema is [schemas/theme/v1.json](../../schemas/theme/v1.json). It lists all 57 semantic colour roles. Values must be `#RRGGBB` or `#RRGGBBAA`; data surfaces and the solid glass fallback must stay opaque. Metadata is plain text. Use a reverse-DNS-style id, a numeric `major.minor.patch` version and a simple SPDX license identifier. Authors are responsible for the rights to their work; the license field is checked for syntax, not a legal determination.

Never include density, typography, fonts, motion, navigation, layout, host data or permissions. These remain independent application preferences. Legacy M1 colour aliases cannot be overridden. The earlier `docs/ui/theme.schema.v1.draft.json` is a historical planning format and is **not** the runtime format. TDK v1 deliberately uses a fixed built-in `base`, with no installed-theme inheritance graph or remote resolution.

```sh
node sdk/theme/tdk.mjs validate ocean.hltheme --json
node sdk/theme/tdk.mjs flatten ocean.hltheme ocean-complete.hltheme
```

`validate` exits 0 when structure and key contrast checks pass; it exits 1 on malformed input or activation-blocking contrast. `--json` provides structured contrast results after parsing succeeds. `flatten` writes all inherited colours explicitly into a new file. Creation and flattening refuse to overwrite an existing file.

The strict parser rejects duplicate decoded keys, dangerous object keys, malformed JSON, unpaired Unicode surrogates, excessive nesting, more than 2,000 values and oversized strings/documents. The app and TDK use the **same** parser and contrast validator. JSON Schema alone cannot detect duplicate keys or calculate contrast, so an editor's green schema check does not replace TDK validation.

## Preview, save and share

1. Import your file into Theme Studio. Import affects only the labelled example preview.
2. Review the issues list. Entries focus the affected colour field. Check all status shapes, disabled controls, links, focus indicators and long technical strings.
3. Preview Desktop, Tablet and Phone. Try Compact, Standard and Spacious density; System fonts; 90–125% text; Solid surfaces and reduced motion using Appearance.
4. Use **Preview on application**. The trial reverts after 15 seconds or Escape unless you choose **Keep theme**. The confirmation uses fixed system colours. Keep a built-in theme available for recovery.
5. Save a draft and export `.hltheme`. Save keeps ten previous revisions per theme; restoring opens a revision in the editor, and the next save creates a new revision. Browser storage is local to this browser profile and origin, may be cleared, and is not a server backup. Export before moving browsers.
6. Share the file and a screenshot, naming the base, theme version and Homelab Control version tested. Do not claim full WCAG compliance from the automated contrast result alone.

For colour changes, check text at 4.5:1 and focus at 3:1 against their actual backgrounds. Transparency must be tested against real content; glass fallback and opaque data surfaces must remain legible. Status always includes fixed text and icons. Themes cannot change permissions, host operations, confirmation flows or component placement.

## Use the validation library

```js
import { parseTheme, themeIssues, resolvedColors } from './sdk/theme/runtime.mjs';
import { readFileSync } from 'node:fs';

const theme = parseTheme(readFileSync('ocean.hltheme', 'utf8'));
const issues = themeIssues(theme);
if (issues.length) throw new Error(issues.map(i => i.message).join('\n'));
console.log(resolvedColors(theme));
```

`parseTheme` throws a useful error for invalid structure; `themeIssues` reports activation-blocking colour issues as `{path, message}`. `resolvedColors` returns the complete semantic palette. Treat every external file as untrusted and enforce the byte limit before reading very large inputs.

## For Homelab Control maintainers

The source is `frontend/app/src/theme.ts` (MIT), shared with the app. Palettes derive from `docs/ui/hcis/tokens/tokens.json`. After changing either:

```sh
node scripts/build-theme-sdk.mjs
node scripts/build-theme-sdk.mjs --check
node --test sdk/theme/tdk.test.mjs
```

Generation uses the project's pinned TypeScript compiler; the resulting `runtime.mjs` has no dependencies. `make lint` checks generated artifacts for drift. A breaking format change requires a new schema version and an explicit migration; v1 rejects unknown keys rather than guessing. Browser-local theme storage does not introduce a backend API, RBAC scope or broker operation.
