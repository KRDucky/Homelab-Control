# Theme and Plugin SDK Specification

## Themes
Use W3C Design Tokens concepts and CSS custom properties; shared accessible component library. Tokens: color, typography, spacing, density, radius, shadows, focus, motion, chart palettes, semantic states. Provide light/dark/OLED/compact base themes, preview and rollback.

**`.hltheme`** is a versioned archive with manifest, JSON token sets, CSS assets, preview and license. Themes must be declarative by default; no arbitrary executable JS. Theme Studio can edit tokens and export/import packages. CLI: `hlctl theme validate|pack|install|list|activate|remove` (planned commands).

Example proposed manifest:
```json
{
  "schemaVersion": "1",
  "id": "org.homelab.oled",
  "name": "OLED Black",
  "version": "1.0.0",
  "engine": ">=1.0.0",
  "modes": ["dark"],
  "tokens": "tokens.json",
  "license": "MIT"
}
```

## Plugins
**`.hlplugin`** is a versioned signed/verified package with manifest, static UI contributions, schemas, permissions, dependencies, optional isolated helper, lifecycle migrations and compatibility checks. Default to UI-only and read-only. Privileged host adapters are separately reviewed and installed through OS packaging where possible.

Manifest declares: id, version, API compatibility, OS/distro constraints, capabilities, dependency alternatives, scopes, routes, widgets, config schema, supported migrations, signatures/checksums and provenance.

Proposed CLI: `hlctl plugin validate|pack|install|list|enable|disable|remove|doctor`. The UI provides install/upgrade/disable/remove with explicit dependency and permission review.

## Dependency contract
Use abstract capabilities (e.g., `proxy.caddy`, `storage.snapraid`, `cooling.liquidctl`) mapped to distro-specific package names, executable versions and service units. Resolver prefers installed and official repos, then curated verified vendor repos. Never silently add repos. No arbitrary install scripts.

## Stability
Versioned extension points, deprecation windows, SDK examples, compatibility test harness, plugin crash isolation where practical. Disabling a plugin must not leave uncontrolled background services or delete user data without explicit action.
