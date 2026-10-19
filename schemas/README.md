# Schemas
- `openapi/homelab-control.v1.yaml` — the v1 API contract (M0 endpoints). Contract tests against the
  Go handlers are an M1 item.
- `plugin/`, `theme/`, `resources/` — reserved for the `.hlplugin` / `.hltheme` manifests (M3) and the
  storage resource graph (M2/M5). Empty in M0 by design: no unreviewed draft schemas are published.

Licence: MIT (owner decision 2026-10-16; boundaries in `REUSE.toml`, text in `LICENSES/MIT.txt`).
