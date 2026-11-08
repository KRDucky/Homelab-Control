# First-party plugins

Default to UI-only and read-only; privileged host adapters are broker ops reviewed separately
(ADR 0001). Plugins are **declarative** (`.hlplugin`, one JSON document) and never executable code.

Plugin sources live in the [Homelab-Control-Extensions](https://github.com/KRDucky/Homelab-Control-Extensions)
repository and are compiled in at build time from the pinned `extensions/` subtree (`extensions.lock`,
`scripts/verify-extensions.sh`, `scripts/build-extensions.mjs`). The labelled read-only sample is
`extensions/plugins/sample-readonly/`; it is **not** a live host plugin and is **not** registered in
`modules.json`.

Planned storage/reverse-proxy/tailscale/cooling plugins (spec 05) remain later milestones.
