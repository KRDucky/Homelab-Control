# Homelab Control SDK

MIT-licensed validators and examples for the declarative theme and plugin formats, plus `hlctl
theme|plugin` packaging helpers when those land (later milestones). No npm dependencies; Node.js >= 22.

- [`theme/`](theme/) — Theme Development Kit (TDK) v1: create, validate and flatten `.hltheme`
  documents (shipped).
- [`plugin/`](plugin/) — Plugin Development Kit (PDK) v1: validate declarative read-only `.hlplugin`
  manifests (M3 plugin SDK first checkpoint).

Both share parser rules with the app: `scripts/build-theme-sdk.mjs` and `scripts/build-plugin-sdk.mjs`
generate `theme/runtime.mjs` and `plugin/runtime.mjs` from `frontend/app/src/theme.ts` and
`frontend/app/src/plugin.ts` respectively. Licence: MIT (owner decision 2026-10-16; `REUSE.toml`,
`LICENSES/MIT.txt`).
