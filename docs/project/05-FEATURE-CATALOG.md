# Feature and Module Catalog

## Core (installed by default)
- Overview: CPU, memory, swap, disk, GPU and network health, alerts, service state, recent events.
- Identity: users/groups, UID/GID, SSH keys, login history, sessions, PAM visibility, sudo validation, account lifecycle.
- Services: systemd units, timers, journal, logs, scheduled jobs, boot behavior.
- Networking: interfaces, bridges, VLANs, bonds including LACP, DNS, routes, host-wide port-use tracker, firewall inspection and validated changes (installed-tooling gate).
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
- exFAT: mkfs/fsck/label, cluster size; interop (no unix ACL, no checksums).
- ZoneFS: conventional vs sequential zones on zoned block devices; not a general-purpose NAS filesystem.
- Ceph: existing-cluster health, OSDs/MONs/MGRs/MDS, pools, CRUSH classes; consumable **RBD**, **CephFS**, **RGW**. Not a second Ceph orchestrator.
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
Docker, Compose, Podman, libvirt/QEMU/KVM, Kubernetes when installed, VM inventory, snapshots where supported, logs, lifecycle and resource monitoring. Compose/Podman YAML linter+validator and env editor (Dockhand as a UX reference, not a dependency). Each WebUI exists only if that stack is on the host. Integrate rather than needlessly duplicate specialist container UIs.

## Backups, monitoring and integrations (optional)
Restic, Borg, rsync, ZFS send/receive, notifications (email/webhook), Prometheus exporters, UPS shutdown policy, media integrations (Jellyfin/Plex/*arr), scheduled maintenance, metrics history.

## Product scope additions (owner, 2026-10-21)
These are product-scope requirements, not an implementation authorization. The capability matrix is in `docs/m2/CAPABILITY-MATRIX.md`.
- **Storage and filesystems:** ZFS, Btrfs, mergerfs, SnapRAID, ext4, XFS, F2FS, mdadm, LVM2.
  - mergerfs and SnapRAID are separately manageable and combinable.
  - Existing ext4/XFS disks are adoptable without reformatting.
  - Disk replacement and SnapRAID recovery are guided, with a feasibility assessment (parity, content files, surviving disks, last successful sync), never automatic.
- **Containers:** Docker Engine, Compose projects, networks, volumes, images and Buildx, managed as distinct objects.
- **System:** systemd services, timers, sockets and logs; package search, install and remove. Package updates and repository management are subject to separate scope approval.
- **Virtualization:** libvirt, QEMU/KVM — VM lifecycle, virtual networks and storage through libvirt APIs, not raw QEMU.
- **File sharing and connectivity:** SFTP (respecting the existing OpenSSH configuration), SMB/Samba (existing configuration detected first), FTP, FTPS, WebDAV, iSCSI, Thunderbolt (authorization, connectivity and storage/network functions kept distinct). NFS remains in scope from the file-services addon.
- **Physical Drive Layout Designer:**
  - user-defined chassis grids (rows/columns, irregular layouts, multiple chassis/shelves/enclosures, HDD/SSD/NVMe/tray/empty/reserved cells);
  - drag-and-drop assignment by stable drive identity, never `/dev/sdX`, with replacement detection;
  - integration with every storage module and replacement workflow;
  - SES/HBA slot discovery and guarded identify LEDs only where the capability is verified;
  - export/import without unnecessary sensitive metadata.

  Staged A–D in `docs/m2/MODULE-ARCHITECTURE.md` §5.
  - Owner direction (2026-10-21): make the existing **HBA Viewer** concept (a separate owner project; source pending, D-12) a single reusable view instead of a separate drive-location system.
- **Architecture requirements:**
  - independent, discoverable modules with capability detection;
  - read-only separated from privileged changes;
  - module-specific scopes and typed broker operations;
  - adopt existing systems without recreating them;
  - reversible/disruptive/destructive risk classes;
  - cross-module device-ownership conflict prevention;
  - per-distribution adapters;
  - navigation in which modules can be enabled, disabled and extended independently.

## Product scope additions (owner, 2026-11-07)

These **add** to the 2026-10-21 catalog. Product-scope requirements, **not** implementation authorization, **not** M2 close, **not** production, **not** KVM acceptance, **not** ADR acceptance. M3 remains Theme SDK + plugin SDK (`docs/project/04-ROADMAP.md`, D-8). Nothing in this section is claimed implemented.

- **Installed-tooling gate (all modules, all WebUI).** A WebUI surface is shown only when the matching host tooling is present (binary, kmod, unit, or socket as the adapter defines). Missing tooling is not a fake empty page. If tooling is absent, Settings offers to install the **distro-specific** packages that enable that feature (Software Center / M4: apt, dnf, zypper, pacman), with a transaction preview. Never enable a privileged write from a hidden module.
- **Stable disk identity (order).** Persist and display, in this order:
  1. `/dev/disk/by-id/` **serial** form first (`ata-SERIAL`, `nvme-SERIAL`, `usb-…-SERIAL`, `scsi-…` when that is the serial) — the string printed on the drive label.
  2. Then **WWN** (`wwn-0x…`) when present.
  3. `/dev/sdX`, `/dev/nvmeXnY`, `/dev/vdX` only for **temporary mounts** and always labelled volatile. Never as the stored identity, bay map key, replacement target, or fstab/crypttab/zpool member id.
- **Drive class and speed.** Identify HDD (rotational), SATA/SAS SSD, NVMe; link rate and rotational/non-rotational from sysfs. Optional isolated **benchmark** is a separate, labelled, disruptive job (ADR 0011); never a silent background probe on production volumes.
- **Filesystems — native feature surface, not a common subset.** Each adapter exposes that filesystem’s own ops. Exact flags and subcommands are documented at implementation against the installed tools. Destructive paths stay gated. Writes wait for the milestone that authorizes that adapter’s **write** column in `docs/m2/CAPABILITY-MATRIX.md` (M6+ on 04-ROADMAP). Read-only topology can land with M5/M7 as already planned.
  - **ext4:** extents, journal mode, 64-bit, metadata_csum, encrypt/fscrypt, casefold, verity, project quotas, online grow, reserved blocks, tune2fs feature flags, e2fsck/e2scrub. Create is destructive.
  - **F2FS:** log-structured layout, compression, encrypt, zoned, gc/discard, fsck, resize. Create is destructive.
  - **XFS:** reflink, rmapbt, quotas (u/g/p), grow (no shrink), xfs_repair / xfs_scrub, realtime volumes if present. Create is destructive.
  - **ZFS:** pools, vdev tree (stripe, mirror, raidz1/2/3, dRAID where installed OpenZFS supports it; log/cache/special/spare extra vdevs), datasets, zvols, properties, compression, encryption, snapshots/clones, send/recv, scrub/resilver, quotas/reservations, ARC/L2ARC/SLOG/special, autotrim, feature flags (one-way upgrades labelled).
  - **Btrfs:** devices, profiles (raid0/1/1c3/1c4/10/5/6 for data and metadata independently; warn that raid5/6 is still the upstream caution), subvolumes, snapshots, send/recv, scrub, balance, device add/remove/replace, compression (zlib/lzo/zstd), qgroups, zoned if supported. openSUSE root+snapper: do not disturb.
  - **LVM2:** PV/VG/LV; linear, striped, mirror, raid1/4/5/6/10, thin, snapshot, cache (`lvconvert --type cache` / cachevol / cachepool; writethrough vs writeback); tags, activation. Reduce/remove LV is destructive.
  - **mergerfs:** N branches; policy (`epmfs`, `mfs`, `ff`, …), `minfreespace`, `moveonenospc`; SSD+HDD branches as a documented pattern. Optional SnapRAID is a **coordinated workflow** (`MODULE-ARCHITECTURE.md` §4), never implied by mergerfs alone.
  - **exFAT:** mkfs/fsck/label, cluster size; interop warning (no unix permissions, no checksum RAID).
  - **ZoneFS:** conventional vs sequential zones, mount of zoned block devices; not a general-purpose NAS filesystem.
  - **Ceph (logical options, first slice):** detect an existing cluster (ceph.conf / cephadm / microceph / rook — whichever is installed). Surfaces: cluster health, OSDs, MONs, MGRs, MDS, pools, CRUSH device classes (hdd/ssd/nvme). Consumable types: **RBD**, **CephFS**, **RGW**. Do not invent a second Ceph orchestrator. Creating a cluster is a later, separately reviewed write path. Cache-tier only if still supported by the installed Ceph — do not advertise removed features.
  - SnapRAID and mdadm: unchanged from the 2026-10-21 catalog (SnapRAID guided recovery only).
- **Beginner vs Expert** where a technology has topologies (ZFS/Btrfs/LVM2/mergerfs/Ceph):
  - **Beginner:** wizard from (disks in the system + usage goal) → one recommended layout, with pros/cons (sequential/random read and write, rebuild/resilver time, redundancy, usable capacity vs raw, recovery story, operational complexity). Starting hints (always labelled, always overridable in Expert): media library → mergerfs+SnapRAID or ZFS raidz2; transcoding scratch → fast SSD/NVMe ext4 or XFS; databases/VMs → mirror or single NVMe with backups, never raidz for small random writes; mixed NAS+apps → split pools; removable/Windows → exFAT with the interop warning; zoned HDDs → ZoneFS or ZFS on zoned devices only when tools support it; multi-node object/block → Ceph, not a single-disk beginner default.
  - **Expert:** full manual topology (same ops, no reduced feature set); every native create/add/remove/replace/property path the tools support, with dry-run and ADR 0011 for writes.
- **Port-use tracker.** One host-wide map of listening ports and the process/unit/container that owns them. Sources: host sockets (`ss`/netns), Docker, Podman, Kubernetes (when installed). Compose / Podman / Kubernetes editors consult this map and **flag** a host/publish port already bound. The tracker is read-only discovery; changing bindings is a write in the owning module.
- **LACP / bonding.** Bonds and LACP (802.3ad), plus the other kernel bonding modes the installed `iproute2`/NetworkManager/systemd-networkd stack actually supports. Prefer the host’s existing renderer; detect first, do not rewrite an unmanaged stack.
- **Firewall WebUI (installed-tooling gate).** Show a UI only for the stack that is present. Detect the **active** backend; others can appear as “installed but not active”. Do not show five firewall products at once.
  - firewalld: zones, services, ports, rich rules, runtime vs permanent
  - ufw: rules, defaults, numbered delete
  - nftables: tables/chains/sets (`nft`); no silent translation into iptables
  - iptables/ip6tables/legacy: tables/chains if nft is not the actual backend
  - bpfilter / Bumblebee: only if that stack is installed and queryable; otherwise hidden
  - XDP: programs attached to interfaces (`bpftool`/`ip link`); load/replace is a later write
- **Containers / VMs (04-ROADMAP M11).** Installed-tooling gate applies. libvirt/QEMU/KVM, Podman, Docker, Kubernetes: each WebUI exists only if that stack is on the host.
  - **libvirt/QEMU** (never raw QEMU): domain lifecycle, virtual networks, storage pools/volumes, snapshots where libvirt supports them. `virsh` argv templates or D-11 go-libvirt; broker-only.
  - **Docker and Podman:** engine/objects as already catalogued. Port tracker on every publish/host-port field. Compose and Podman play/kube YAML: linter + validator (syntax, Compose spec, known foot-guns); **env editor** so the user does not hand-create `.env` files (side-by-side YAML + env). **Dockhand as a UX reference, not a dependency and not a copy** (visual/YAML compose editor, env vars manual or import `.env`, validation with fix-it, dependency graph, logs). Out of scope unless the owner later asks: Dockhand Git-sync, Restic backups, Grype/Trivy, multi-remote Docker hosts, in-container file browser (Homelab Control already has a confined `/files` module). `hc-api` never joins the `docker` group (socket is broker-only, root-equivalent).
  - **Kubernetes:** only if `kubectl` / a local API (k3s, k8s, microk8s, …) is installed. First slice: cluster/node/workload inventory, Service/NodePort/hostPort tied into the port tracker. Full cluster-admin UI is later. Optional adapter.
- **Roadmap homes (do not move these into M3).** Disk identity / SMART / type/speed / bay map: **M5**. Partition/format/mount and beginner-wizard writes: **M6**. ZFS/Btrfs/mdraid read topology: **M7**; LVM2/mergerfs follow the matrix read→write split. exFAT, ZoneFS: M5 read / M6 format. Ceph logical options: after M7; cluster create separately reviewed. Port tracker, LACP/bonding, firewall WebUI: networking/security module (catalog core; not M2). Docker/Podman/Compose editor, libvirt, Kubernetes: **M11**.

## User journeys
1. New drive: identify → partition → format → mount → assign role → verify.
2. Failing drive: health alert → bay/serial mapping → dependency analysis → technology-specific replacement plan → supervised execution → verify.
3. New proxy host: install backend if absent → DNS/TLS setup → upstream mapping → validate → deploy → health check.
4. New plugin: compatibility check → dependency resolution → source approval → permission review → install → enable → audit.
5. Cooling profile: detect sensors/controllers → test supported controls → set curve → failsafe validation → apply.
