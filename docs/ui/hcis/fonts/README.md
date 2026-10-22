# HCIS fonts: licence records

> **Records only.** No font files are in this folder or in `frontend/app`, and nothing here is
> installed or loaded. Bundling Inter and JetBrains Mono is migration step 2, a separately approved PR.
> The app still uses Archivo (LA-06) until then.

Owner font licensing policy (2026-10-22) is in `../BRAND-BOOK.md` › Typography › Font licensing policy.
This file records the fonts checked against it.

| Family (token) | Package checked | Licence | Copyright | Reserved Font Name | Upstream |
|---|---|---|---|---|---|
| Inter Variable (`ui`) | npm `@fontsource-variable/inter` 5.3.0 (`LICENSE`, package licence `OFL-1.1`) | SIL OFL 1.1 | 2016 The Inter Project Authors | None declared | https://github.com/rsms/inter |
| JetBrains Mono Variable (`mono`) | npm `@fontsource-variable/jetbrains-mono` 5.3.0 (`LICENSE`, package licence `OFL-1.1`) | SIL OFL 1.1 | 2020 The JetBrains Mono Project Authors | None declared | https://github.com/JetBrains/JetBrainsMono |

The design-system artifact uses the Latin variable subsets (`*-latin-wght-normal.woff2`; Inter weights
100–900, JetBrains Mono 100–800) and keeps verbatim copies of both licence texts under Assets › Fonts.

## What the OFL 1.1 allows and requires

- **Allowed:** bundling with GPL-3.0-or-later software and redistributing it in packages, unmodified or modified.
- **Not allowed:** selling the fonts on their own.
- **Required:** the licence text and copyright notice accompany every copy.
- **Naming:** a modified version must not use a Reserved Font Name. Neither font declares one; a modified build should still be renamed.

## When step 2 bundles them

- Add the licence texts (`LICENSES/OFL-1.1.txt`, REUSE annotations for the `.woff2` files) and third-party
  notices, as was done for Archivo (LA-01, LA-06), and record the decision in `LICENSE-AUDIT.md`.
- Serve the files from the app itself. Never use an external font CDN.
- Keep the system stacks (`ui-system`, `mono-system`) as the fallback and as the **System** font preference.

## Adding another font

Follow the policy in the brand book. Record the font here with its exact licence text, copyright holder,
Reserved Font Name, source and the version checked. Fonts whose rights can't be verified are not added.
