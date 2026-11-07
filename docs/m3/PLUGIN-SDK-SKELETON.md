# M3 plugin SDK — first checkpoint working note (read-only sample plugin)

**Status:** development note only. Not M3 acceptance, not M2 close, not a merge.
**Base:** `origin/main` `20ddc2b7` (PR #54 squash on PR #53 `9fc9d8af` on `b290f9dc`).

## Scope of this checkpoint

The **plugin half** of the M3 Theme/Plugin SDK, isolated. The theme half (install/preview/switch/remove)
is already shipped browser-local (PR #24, ADR 0010). This checkpoint adds a declarative, read-only
sample plugin with a compiled-in UI contribution and browser-local load/unload. No executable payload.

## Manifest

`schemas/plugin/v1.json` (MIT). One JSON document, fields limited to:

- `schemaVersion` — `1`.
- `id` — reverse-DNS (`^[a-z0-9]+(\.[a-z0-9-]+){1,4}$`, ≤ 100 chars).
- `name` — plain text, 1–80 chars, no control/bidi/markup characters.
- `version` — `major.minor.patch`.
- `license` — simple SPDX identifier.
- `contributions` — non-empty array of **known contribution IDs** (see allow-list).

Extra fields are refused (`additionalProperties: false`). There is no archive, image, font, CSS,
script, binary payload, route, scope, broker op or helper process.

## Contribution allow-list

Exactly one known contribution ID this checkpoint: `overview.sample-card`. It maps to already-compiled
React in `frontend/app/src/plugin-ui.tsx`; when loaded, the Overview page shows a labelled sample card,
and unloading hides it. Unknown IDs are refused at parse time and never mount UI (no `eval`, no dynamic
`import()`, no Go `plugin` package, no spawn).

## In-tree sample

`plugins/sample-readonly/` — a labelled fixture/example manifest plus README. It is **not** a live
host plugin and is **not** registered in `modules.json`; `TestRegistryIsClosed` stays `{services}`
only. First-party modules stay compiled-in (D-10).

## Persistence

Browser-local only, under a **new** localStorage key `hc.plugins.v1` (never reuses `hc.preferences.v1`).
No new API route, no new scope, no `state.db`, no broker operation. Enable/disable lives on the
existing Appearance menu, clearly labelled as a sample plugin, not a theme. This checkpoint never reads
a user-supplied manifest: the only reachable contribution is the compiled-in, allow-listed sample.

## Non-goals (this checkpoint)

- Runtime privileged plugin loading; `eval` / user JS; isolated helper process; signed archives.
- `hlctl plugin`; server-wide plugin library; marketplace; privileged host adapters.
- New `DefaultScopes`; `plugins.read` / `plugins.configure`; any broker amendment.
- A new page/route (avoids a Playwright fixture this slice).
- First-party modules as plugins (D-10 stays compiled-in).
- compat/webmin/ edits. Core stays GPL-3.0-or-later; `sdk/`, `schemas/plugin/`, sample manifests MIT.

## Tests

Parser (Node `sdk/plugin/tdk.test.mjs`): oversized document, duplicate keys, prototype keys, embedded
script/code strings, archive/binary payloads, unknown contribution IDs, extra fields → refuse; in-tree
sample validates. Vitest (`frontend/app/src/plugin.test.tsx`): sample hidden until loaded; unload
removes it; hostile manifest never mounts unknown UI; Theme Studio import/export unchanged. Guard
(`backend/internal/guard`): `plugin` / `os/exec` / `unsafe` imports still forbidden — unchanged.

## Playwright + axe

`tests/e2e/plugin_browser.py` drives the existing Appearance header popover: enable the read-only sample
plugin, assert the Overview page shows the labelled `h2` card, run desktop + mobile axe **while the card
is loaded**, disable, assert it is gone, and confirm Theme Studio (`/appearance/themes`) import/export is
unchanged. It reuses the authenticated `TestStorageBrowserFixture` (plugin state is browser-local
`hc.plugins.v1`), adds no route/API/scope/broker op. Accessibility fails on **violations only** (incomplete
results are persisted to `axe-incomplete.json` / `axe-mobile-incomplete.json` and their ids printed), and
the sample-card subtree itself is scoped and fails closed on violations **or** incomplete. Empty axe JSON
on pass. Opt-in, not in CI.
