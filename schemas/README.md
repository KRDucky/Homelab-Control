# Schemas
- `openapi/homelab-control.v1.yaml` — the v1 API contract (M0 endpoints). Contract tests against the
  Go handlers are an M1 item.
- `theme/v1.json` — the runtime `.hltheme` format (colours only; see ADR 0010 and `sdk/theme/README.md`).
- `plugin/`, `resources/` — reserved for the `.hlplugin` manifest (M3) and the storage resource graph
  (M2/M5). Empty by design: no unreviewed draft schemas are published.

Licence: MIT (owner decision 2026-10-16; boundaries in `REUSE.toml`, text in `LICENSES/MIT.txt`).
