# First-party plugins

Default to UI-only and read-only; privileged host adapters are broker ops reviewed separately
(ADR 0001). Plugins are **declarative** (`.hlplugin`, one JSON document) and never executable code.

- [`sample-readonly/`](sample-readonly/) — in-tree labelled **sample** for the M3 plugin SDK first
  checkpoint. A fixture/example manifest whose `overview.sample-card` contribution maps to compiled-in
  React; load/unload is browser-local only. It is **not** a live host plugin and is **not** registered
  in `modules.json`.

Planned storage/reverse-proxy/tailscale/cooling plugins (spec 05) remain later milestones.
