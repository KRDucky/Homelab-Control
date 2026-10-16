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
1. **Parse** with `JSON.parse` on text ≤ 64 KiB (server and client both enforce); reject duplicate keys
   (custom reviver), depth > 8, > 2 000 total values.
2. **Validate** against `theme.schema.v1.json` (draft in `theme.schema.v1.draft.json`) — same schema on
   server (Go validator, M2) and client.
3. **Resolve** `extends` (max depth 3, no cycles, built-in/installed ids only) and modes.
4. **Contrast-check** every text/background and non-text pair (`07` §7.1); errors block activation,
   warnings are shown.
5. **Apply** only via `document.documentElement.style.setProperty('--hc-<token>', value)` for an
   allowlisted token name with an engine-formatted value. No `<style>` element, no `innerHTML`,
   no `cssText` concatenation. This keeps the existing CSP (`style-src 'self'`, no `'unsafe-inline'`)
   intact — CSSOM property writes are not blocked by CSP and do not need it relaxed.

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
No archive format is used, which removes zip-slip, zip-bomb and symlink risks entirely. (This
refines `docs/project/06-THEME-AND-PLUGIN-SDK.md`, which describes `.hltheme` as an archive; the change
needs owner approval with the rest of this plan.) If a future
version needs bundled assets, it requires an ADR covering: archive path normalisation, entry count/size
limits, compression-ratio limits, no symlinks/devices, content-type allowlist and signing.

Malicious-theme test corpus (unit tests, `08` §8.3): `url()` in colour, CSS in font, markup in name,
unknown keys, oversize file, deep nesting, duplicate keys, `extends` cycle, `__proto__`/`constructor`
keys, NaN/Infinity-like strings, extremely long strings, contrast-violating palette, Unicode
bidi-override characters in name.

## 9.5 Server side (M2+)
Theme storage is per user (or global with `appearance.manage`), written by `hc-api` only after the same
validation. Themes never reach the broker; no root process parses them. Audit records: install,
activate, delete, export (hash + id, not content).

## 9.6 Plugins vs themes
Themes are data. Anything that needs code is a **plugin** (`docs/project/06-THEME-AND-PLUGIN-SDK.md`),
with declared permissions, review and signing; a theme can never upgrade itself into a plugin.
