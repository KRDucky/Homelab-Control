# Project Context and Product Definition

## Why this exists
Cockpit is approachable but misses many NAS/homelab workflows; Webmin has broad Linux administration coverage but an aging and fragmented experience. Homelab Control is a **Webmin fork**, not just a Webmin theme or Cockpit plugin. It should preserve useful upstream capabilities and make homelab administration discoverable, safe and pleasant.

## Target users
Self-hosters, NAS operators, Linux enthusiasts and small homelab owners on Ubuntu, Debian, Fedora, openSUSE, Arch and compatible derivatives. Single-host operation first; multi-host management later. Works on bare metal, VMs and servers with heterogeneous disks/GPUs.

## Product values
- Local-first, no required cloud account or telemetry.
- Core administration works without Docker or Kubernetes.
- Accessible, responsive, keyboard-friendly UI; dark/light/OLED/compact themes.
- No enterprise-only clutter in primary navigation; advanced capabilities available when needed.
- Explain what an action changes and how to recover from failures.
- Strong defaults; experts can inspect underlying commands/configurations without unrestricted execution.

## Primary differentiators
Unified physical disk-to-bay mapping; cross-filesystem storage dependency graph; guided drive replacement; modern partition/format/mount editor; ZFS, mdraid, Btrfs, mergerfs and SnapRAID support; distro-aware Software Center; native Caddy/Nginx/Traefik manager; ACME/Cloudflare DDNS; Tailscale; cooling controls; modern plugin/theme SDK.

## Non-goals for v1
Kubernetes control plane, enterprise compliance suite, arbitrary remote root automation, guaranteed support for every HBA/backplane, automatic destructive recovery, or rewriting all of Webmin before first useful release.

## Existing system context (design test cases, not assumptions about live state)
Representative environments include Ubuntu media servers with ZFS and mdraid, Docker/Compose services, Intel Arc and NVIDIA GPUs, SAS/SATA HBAs, and Arch-family desktops. Design adapters to discover rather than assume any particular topology.
