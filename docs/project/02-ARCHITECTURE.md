# Architecture Specification

## System layers
1. **UI shell**: React + TypeScript + Vite, router, data fetching, accessible component kit, navigation, charts, command palette, responsive dashboards.
2. **Design system**: token-based CSS variables, theme pack loader, validated theme manifests, optional theme studio.
3. **Extension host**: signed/verified package metadata, module registry, plugin lifecycle, capability negotiation, permissions, isolated UI contribution points.
4. **Management API**: versioned REST/OpenAPI; SSE for status streams; WebSockets only for terminal-like interactive use cases; explicit authentication, authorization and CSRF defenses.
5. **Core services**: discovery, resource graph, jobs, audit, notifications, configuration and secrets, package resolver, health.
6. **Host adapters**: Webmin Perl bridge, systemd/D-Bus, udev/sysfs, UDisks2/libblockdev where available, CLI adapters with strict argument arrays, Docker/Podman/libvirt APIs.
7. **Privileged executor**: narrow allowlisted operations; per-operation RBAC, target revalidation, locking, timeout, audit, safe cancellation.

## Module principle (owner decision, 2026-10-21)
- **Modules are independently useful.** Each module works whenever its own host dependencies are available; users are never forced to configure unrelated services, and a missing tool disables only that module.
- **Coordinated cross-module workflows add convenience without mandatory dependencies.** Examples:
  - ZFS dataset → SMB/SFTP/WebDAV share;
  - filesystem → Docker bind mount;
  - LVM LV → format → persistent mount;
  - mergerfs + optional SnapRAID;
  - libvirt VM on an existing pool;
  - storage → iSCSI target.

  They are offered only when the participating modules are enabled. Each one:
  - uses explicit ownership boundaries;
  - validates prerequisites;
  - shows the planned changes, with dry runs where feasible;
  - marks destructive steps;
  - defines recovery for partial failures;
  - never bypasses a module's own security controls;
  - never promises automatic rollback for irreversible steps.
- Design: `docs/m2/MODULE-ARCHITECTURE.md`. This is design authorization only, not implementation.

## Migration strategy
- Maintain an upstream Webmin fork with reproducible upstream sync and security patch tracking.
- Start with new UI and authenticated API facade. Bridge mature Perl modules only through audited endpoints; never expose arbitrary Perl module calls.
- Move high-value functions to typed adapters one at a time; maintain parity tests.
- Preserve an advanced legacy interface until feature parity and safety justify retirement.

## Core resource graph
Represent entities and typed edges, not only a tree: controller, enclosure, bay, disk, partition, filesystem, mount, LVM, mdraid, ZFS pool/vdev/dataset, Btrfs filesystem/subvolume, mergerfs branch/pool, SnapRAID data/parity/content file, shares, container bind mounts, services. Each edge records source and confidence. Distinguish observed facts, user labels, inferred links and stale records.

## Data and runtime
SQLite with WAL for jobs, audit metadata, settings and resource snapshots; do not put credentials in ordinary plaintext records. Use system keyring/credential facilities or protected root-owned files with strict permissions. No required cloud service. Expose health endpoints and diagnostics; redact secrets in logs.

## Suggested repository layout
```text
homelab-control/
  CLAUDE.md
  docs/
  frontend/{app,design-system,theme-studio}
  backend/{api,auth,resources,jobs,packages,plugins,adapters}
  compat/webmin-perl/
  plugins/{storage-zfs,storage-mdraid,storage-btrfs,storage-mergerfs,storage-snapraid,reverse-proxy,tailscale,cooling,...}
  themes/{default-light,default-dark,oled,compact}
  schemas/{openapi,plugin,theme,resources}
  packaging/{debian,fedora,opensuse,arch}
  tests/{unit,integration,e2e,security,vm-fixtures}
  scripts/
```

## Key ADRs to write during M0
Webmin fork maintenance policy; API auth/session model; privilege boundary; legacy Perl migration; plugin isolation; package-source trust; storage resource identifiers; database and secrets; UI theming architecture.
