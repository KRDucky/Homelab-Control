# HCIS fonts: licence records

> **Bundled since migration step 2 (2026-10-23).** The app imports both fonts from npm
> (`frontend/app/package.json`, `src/main.tsx`). No font files are kept in this folder; this file is the
> licence record.

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

## How they ship (step 2)

| Package | Font version | Files bundled | Size |
|---|---|---|---|
| `@fontsource-variable/inter` 5.3.0 (`wght.css`) | Inter 4.001 | 7 variable `.woff2` subsets (latin, latin-ext, cyrillic, cyrillic-ext, greek, greek-ext, vietnamese), weights 100–900 | 218.5 kB |
| `@fontsource-variable/jetbrains-mono` 5.3.0 (`wght.css`) | JetBrains Mono 2.211 | 6 variable `.woff2` subsets (latin, latin-ext, cyrillic, cyrillic-ext, greek, vietnamese), weights 100–800 | 86.2 kB |

- Every file's name table states the copyright above; no Reserved Font Name. Fontsource subsets the
  upstream fonts (from the Google Fonts sources), which the OFL permits.
- Vite emits the files into the UI bundle and `hc-api` serves them from its own origin (CSP `font-src 'self'`).
  Each subset has a `unicode-range`, so browsers fetch only what the page's text needs. No font CDN.
- The licence text and copyright ship in `docs/compliance/THIRD-PARTY-NOTICES.md` (installed by every
  package) and the DEP-5 `copyright`, generated from the installed packages by
  `scripts/gen-third-party-notices.py`; the SBOM lists both packages. `LICENSE-AUDIT.md` LA-06, EX-04 and
  ADR 0006 amendment 2 record the review. The font files are not in the repository, so no REUSE entry is needed.
- The system stacks (`ui-system`, `mono-system`) stay as the fallback and as the future **System** font preference.

## Adding another font

Follow the policy in the brand book. Record the font here with its exact licence text, copyright holder,
Reserved Font Name, source and the version checked. Fonts whose rights can't be verified are not added.
