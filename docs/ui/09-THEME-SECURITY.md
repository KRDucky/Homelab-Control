# 9. Theme package security and validation (planning)

Threat model: a theme file is **untrusted input**. It may come from a forum, a shared link, or an
attacker who wants an administrator to import it. A malicious theme must not be able to execute code,
exfiltrate data, make the UI lie about state, or degrade the server.

## 9.1 What a theme can and cannot contain
| Allowed | Forbidden (rejected by schema, not sanitised) |
|---|---|
| Typed values for known tokens (`02`): hex colours, bounded numbers, enum choices | JavaScript, HTML, SVG, CSS text, selectors, `@import`, `url()`, `expression()`, `var()` references, `!important` |
| Plain-text name/description/author (no `<`, `>` or control characters) | URLs of any kind (no remote fonts, images or icons) |
| `extends` naming a **built-in or installed** theme id | Unknown keys anywhere (`additionalProperties: false`) |
| SPDX licence identifier | Binary payloads; fonts; images (built-in assets only, in M2+ via reviewed packages) |

Because the grammar is typed (a colour is `#rrggbb[aa]`, a length is a number with a fixed unit chosen by
the engine), there is no string the validator must "clean": anything outside the grammar is rejected.

## 9.2 Application path
1. **Parse strictly** text ≤ 64 KiB (server and client both enforce). `JSON.parse` cannot be used on its
   own: it silently keeps the *last* of duplicate keys, and a reviver cannot detect duplicates because it
   only sees the already-merged object. Go's `encoding/json` behaves the same way. So:
   - **Client:** a small dedicated scanner (no dependency, ~150 lines) walks the text once, following the
     JSON grammar (RFC 8259), and rejects: duplicate keys within an object; keys `__proto__`,
     `constructor`, `prototype`; nesting deeper than 8; more than 2 000 values; strings longer than 256
     characters; invalid UTF-16 surrogates. It builds the result from `Object.create(null)` objects, so no
     later merge can reach `Object.prototype`. Only then does schema validation run.
   - **Server (M2+):** the same rules with `json.Decoder.Token()` (a streaming walk that tracks the key
     set per object), before decoding into typed structs with `DisallowUnknownFields`.
   - Both validators run the same corpus (§9.4), including a duplicate key whose first value is valid and
     whose second is malicious, so the two sides cannot disagree about which value wins.
2. **Validate** against `theme.schema.v1.json` (draft in `theme.schema.v1.draft.json`) — same schema on
   server (Go validator, M2) and client.
3. **Resolve** `extends` (max depth 3, no cycles, built-in/installed ids only) and modes.
4. **Contrast-check** every text/background and non-text pair (`07` §7.1); errors block activation,
   warnings are shown.
5. **Apply** only via `document.documentElement.style.setProperty('--hc-<token>', value)` for an
   allowlisted token name with an engine-formatted value. No `<style>` element, no `innerHTML`,
   no `style=` attribute, no `cssText` concatenation. The existing CSP (`style-src 'self'`, no
   `'unsafe-inline'`) stays unchanged.
   **Verified** by `tests/e2e/theme_cssom_csp.py` against the real `hc-api` page and CSP header: the
   CSSOM custom-property write applies and raises no violation, while a `<style>` element
   (`style-src-elem`) and a `style` attribute set with `setAttribute` (`style-src-attr`) are blocked and
   reported, which shows the CSP is in force. Run in Chromium locally (2026-10-16) and in Chromium,
   Firefox and WebKit in CI (`browser-e2e`). Constructable stylesheets also apply without violation;
   the engine does not use them.

## 9.3 Integrity of the interface
Themes must not be able to make the UI misrepresent state:
- Status colours (`danger`, `warning`, …) are themeable but always paired with fixed text/icons.
- Minimum contrast for status, focus and destructive-action styles is enforced, not advisory.
- `opacity`/`display`-like tokens do not exist; a theme cannot hide confirmations, warnings or the
  audit/operation indicators. Spacing and type scale are clamped so controls stay visible and ≥ 24 px.
- The confirmation dialog for privileged operations (M2+) renders with a fixed system palette that
  themes cannot override.

## 9.4 Import/export packages (`.hltheme`)
A `.hltheme` file is the theme JSON itself (single document, `application/vnd.homelab-control.theme+json`).
No archive format is used, which removes zip-slip, zip-bomb and symlink risks entirely. This refines
`docs/project/06-THEME-AND-PLUGIN-SDK.md`, which described `.hltheme` as an archive; accepted in
principle by the owner (ADR 0010, 2026-10-16). If a future
version needs bundled assets, it requires an ADR covering: archive path normalisation, entry count/size
limits, compression-ratio limits, no symlinks/devices, content-type allowlist and signing.

Malicious-theme test corpus (unit tests, `08` §8.3): `url()` in colour, CSS in font, markup in name,
unknown keys, layout keys (`navigation`, `density`: not theme content, ADR 0010), oversize file, deep
nesting, duplicate keys (valid first value, malicious second), `extends` cycle,
`__proto__`/`constructor`/`prototype` keys, NaN/Infinity-like strings, extremely long strings, lone
UTF-16 surrogates, contrast-violating palette, Unicode bidi-override characters in name.

## 9.5 Server side (M2+)
Theme storage is per user (or global with `appearance.manage`), written by `hc-api` only after the same
validation. Themes never reach the broker; no root process parses them. Audit records: install,
activate, delete, export (hash + id, not content).

## 9.6 Plugins vs themes
Themes are data. Anything that needs code is a **plugin** (`docs/project/06-THEME-AND-PLUGIN-SDK.md`),
with declared permissions, review and signing; a theme can never upgrade itself into a plugin.
