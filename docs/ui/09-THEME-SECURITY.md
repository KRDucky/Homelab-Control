# 9. Runtime theme input security

Implemented frontend contract, authorized 2026-10-25. See [runtime schema](../../schemas/theme/v1.json), [shared validator](../../frontend/app/src/theme.ts) and [TDK](../../sdk/theme/README.md).

A theme is untrusted data. The importer bounds the file to 64 KiB before reading, then a dedicated JSON scanner checks grammar, duplicate decoded keys, prototype-related keys, depth (8), value count (2,000), string length (280) and Unicode surrogate pairing. Objects are created without prototypes. Native JSON parsing is used only for individual scanned string/primitive literals, never to merge unchecked documents.

The typed validator rejects unknown fields, arbitrary CSS/functions/URLs/markup, layout/preferences, unsupported schema versions and invalid metadata/colours. Names reject control and bidi-override/isolation characters. The runtime schema allow-lists all supported fields. Data surfaces and the solid glass fallback remain opaque. Key text/status/focus contrast checks run after resolving the built-in base and compositing alpha. Errors block activation; saving an invalid-contrast draft is permitted. The limited contrast check does not assert full WCAG compliance.

Application uses allow-listed semantic CSS custom properties with `style.setProperty` and engine-validated hex values. There is no CSS text, selector injection, inline stylesheet, `style` attribute concatenation, executable package, external font or asset request. Existing CSP stays unchanged. Legacy aliases cannot be set by imported documents. Isolated previews explicitly rebind those aliases to their scoped semantic colours.

The 15-second application trial is transient. Escape, timeout or unmount restores the previous appearance; only Keep persists. Keep/Revert uses fixed system colours. Theme changes never alter permissions, operation targets, layout or confirmation behavior. Status keeps fixed text/icons alongside colour.

Browser-local documents are revalidated when read, and active documents are contrast-checked again. Malformed or unreadable storage falls back to built-in appearance. Theme library/history writes are atomic and bounded. Export is exactly one JSON document (`application/vnd.homelab-control.theme+json`), without archive/assets.

The shared malicious-input tests include decoded duplicate keys, prototype keys, malformed JSON, excessive size/depth/count, nonfinite numeric input, unpaired surrogates, code strings, forbidden layout fields, alias overrides and contrast failures. The generated TDK runtime uses the same validator; drift is a lint failure.

Server persistence, server validation and server audit records are not added here. Themes never reach the privileged broker. Any future server store or asset-bearing package needs its own reviewed contract.
