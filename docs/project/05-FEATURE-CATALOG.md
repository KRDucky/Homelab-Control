# Feature and Module Catalog

## Core (installed by default)
- Overview: CPU, memory, swap, disk, GPU and network health, alerts, service state, recent events.
- Identity: users/groups, UID/GID, SSH keys, login history, sessions, PAM visibility, sudo validation, account lifecycle.
- Services: systemd units, timers, journal, logs, scheduled jobs, boot behavior.
- Networking: interfaces, bridges, VLANs, bonds, DNS, routes, firewall inspection and validated changes.
- Software Center: APT/dpkg, DNF/RPM, zypper/RPM, pacman; repos, updates, package details, dependency resolver, transaction preview.
- Storage core: disk inventory, SMART/NVMe, partitions, formatting, mounts, filesystems, topology graph, bay mapping, replacement planning.
- Hardware: PCIe, HBA, motherboard sensors, temperatures, fan RPM, UPS visibility.
- System security: SSH configuration, firewall, access logs, audit and alerts.

## Storage adapters (optional)
- ZFS: pools/vdevs/datasets, ARC, scrub/resilver, snapshots, replacement, replication.
- mdraid: members, degraded arrays, rebuild and replacement.
- Btrfs: devices, profiles, subvolumes, scrub, balance, snapshots.
- mergerfs: branches, policy, min-free-space, branch modes, mount persistence, evacuation.
- SnapRAID: data/parity/content files, status, sync/scrub, parity capacity, recovery, scheduling.
- LVM and LUKS: discovery and management with staged safety gates.
- SMB/NFS/iSCSI and filesystem sharing plugins.

## Reverse Proxy Manager (optional)
Three adapters: **Caddy**, **Nginx**, **Traefik**. Support native systemd services, hostnames, upstreams, WebSockets, headers, redirects, access rules, config validation, atomic managed-file updates, reload and rollback. Respect unmanaged configurations; import only what can be parsed safely. Traefik file-provider and Docker-provider workflows must be distinguished. Avoid pretending all three have identical configuration semantics.

## Certificates and DNS (optional)
- Central ACME/Let's Encrypt manager: HTTP-01, DNS-01, staging/production, renewal, wildcard DNS-01, private key protection, deployment hooks, expiration alerts.
- Cloudflare DNS provider: scoped API tokens, zone/record selection, DNS propagation diagnostics.
- Cloudflare DDNS: A/AAAA, multi-zone, proxy flag, IP-change detection, logs, rate-limit handling.
- Cloudflare Tunnel: separate plugin for tunnel routes and service status; not equivalent to Cloudflare DDNS.

## Networking and remote access (optional)
Tailscale: local daemon state, peers, exit nodes, advertised routes, SSH status, ACL visibility where API access permits. WireGuard, OpenVPN, DNS/DHCP, AdGuard Home/Pi-hole integrations as future modules.

## Cooling and power (optional)
`lm-sensors`/hwmon, `fancontrol`, `liquidctl`, supported GPU interfaces, IPMI/BMC, NUT. Show hardware capabilities before controls. Profiles, hysteresis, temperature sources, RPM/pump monitoring, safe fallback. Never assume GPU or AIO fan adjustment is supported.

## Containers and VMs (optional)
Docker, Compose, Podman, libvirt/QEMU/KVM, VM inventory, snapshots where supported, logs, lifecycle and resource monitoring. Integrate rather than needlessly duplicate specialist container UIs.

## Backups, monitoring and integrations (optional)
Restic, Borg, rsync, ZFS send/receive, notifications (email/webhook), Prometheus exporters, UPS shutdown policy, media integrations (Jellyfin/Plex/*arr), scheduled maintenance, metrics history.

## User journeys
1. New drive: identify → partition → format → mount → assign role → verify.
2. Failing drive: health alert → bay/serial mapping → dependency analysis → technology-specific replacement plan → supervised execution → verify.
3. New proxy host: install backend if absent → DNS/TLS setup → upstream mapping → validate → deploy → health check.
4. New plugin: compatibility check → dependency resolution → source approval → permission review → install → enable → audit.
5. Cooling profile: detect sensors/controllers → test supported controls → set curve → failsafe validation → apply.
