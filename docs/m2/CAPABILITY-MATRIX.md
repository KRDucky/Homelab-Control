# Module capability matrix

**Date:** 2026-10-21, extended 2026-11-07 · **Status:** planning. Product-scope requirements (owner,
2026-10-21 and 2026-11-07); **nothing in this table is implemented** unless the row says so. The
2026-11-07 rows are **not** implementation authorization, **not** M2 close, **not** production, **not**
KVM acceptance, and **not** ADR acceptance.

Milestone names in the 2026-10-21 rows followed the then-proposed sequence in `M2-ROADMAP.md` §4
(D-8 was open). **D-8 (2026-11-07) keeps `docs/project/04-ROADMAP.md`.** M3 remains Theme SDK + plugin
SDK. New 2026-11-07 rows use 04-ROADMAP homes. Owner 2026-11-07 inserted a later **M11** networking
slot (former M11–M13 became M12–M14); M3 is unchanged.

Privilege column:
- **U** = unprivileged in `hc-api`;
- **BR** = broker read op;
- **BW** = broker write op (plan/apply, ADR 0011, accepted 2026-10-23; first used by M2-2 service start/stop/restart).

**Installed-tooling gate (owner, 2026-11-07; all modules, all WebUI):** a WebUI surface is shown only
when the matching host tooling is present (binary, kmod, unit, or socket as the adapter defines).
Missing tooling is not a fake empty page. Settings “install packages to enable” stays
**informational until M4** (Software Center: apt, dnf, zypper, pacman; transaction preview; **no
silent repos**). Never enable a privileged write from a hidden module.

Package names vary by release and must be resolved at runtime (spec 06). Entries marked *verify* were
not confirmed for every family during this review.

## Storage and filesystems
| Technology | Discovery / monitoring | Configuration / lifecycle | Privileges | Backend integration | Main safety risks | Host dependencies | Distro notes | Milestone (read → write) |
|---|---|---|---|---|---|---|---|---|
| Disk inventory (all) | Disks, partitions, identity, transport, size, HDD/SSD/NVMe class and link rate; **M1 `hwinfo` exists**. **2026-11-07 identity split (product scope, M5; not a claim that M1 already implements it; M2-5 stored-key contract unchanged):** **display/label** = serial then WWN (`ata-SERIAL`, `nvme-SERIAL`, `usb-…-SERIAL`, `scsi-…` when that is the serial, then `wwn-0x…`); **persist/write-target** = strongest tier (`wwn:`, `nvme-uuid:`, `serial:`); `/dev/disk/by-id/` refused for bay assignment; `/dev/sdX`, `/dev/nvmeXnY`, `/dev/vdX` only for **temporary mounts** and labelled volatile; destructive ops re-verify identity at execution and type-confirm the **label serial** | — | U | sysfs, `/dev/disk/by-id` | Volatile kernel names used as persistent id; by-id used as a bay key | none | all | done (M1) → M5 identity order |
| Mounts / filesystems (generic) | Mount table, fs type, UUID/label, usage | Mount/unmount, fstab or systemd `.mount` units | U / BW | `/proc/self/mountinfo`, sysfs, `blkid` (BR) | Unmounting busy or in-use filesystems; boot failure from bad fstab | util-linux | all | M2 → M6 |
| ext4 | Features, state, last check, reserved blocks | Create (**destructive**), grow, label, fsck offline | BR / BW | `dumpe2fs -h`, `tune2fs`, `resize2fs`, `mkfs.ext4`, `e2fsck` | mkfs on the wrong device; fsck on a mounted fs | e2fsprogs | all | M3 → M6 |
| XFS | Geometry, quotas | Create (**destructive**), grow (no shrink), label, repair offline | BR / BW | `xfs_info`, `xfs_growfs`, `mkfs.xfs`, `xfs_repair` | Repair with `-L` loses log; shrink impossible | xfsprogs | all | M3 → M6 |
| F2FS | Basic info | Create (**destructive**), resize | BR / BW | `dump.f2fs`, `mkfs.f2fs`, `resize.f2fs` | Tooling maturity varies | f2fs-tools (*verify* Fedora/openSUSE) | most | M3 → M6 |
| Btrfs | Filesystems, devices, profiles, subvolumes, scrub/balance status | Subvolume/snapshot create/delete, scrub, balance, device add/remove | BR / BW | `btrfs … --format json` where supported, else parsed output | Balance/device removal on degraded fs; openSUSE root fs (snapper) must not be disturbed | btrfs-progs | all (openSUSE root default) | M3 → M6 |
| mdadm | Arrays, members, state, sync progress | Create (**destructive**), add/replace member, grow, assemble | U (`/proc/mdstat`, sysfs) / BR (`mdadm --detail --export`) / BW | mdadm, sysfs | Wrong member removal; superblock overwrite | mdadm | all | M3 → M6/M9 |
| LVM2 | PVs, VGs, LVs, thin pools, usage | Create/extend/reduce LV, snapshots, VG/PV changes | BR / BW | `pvs/vgs/lvs --reportformat json` | Reduce/remove LV loses data; PV removal | lvm2 | all | M3 → M6 |
| ZFS | Pools, vdev tree, health, scrub/resilver, datasets, snapshots, ARC | Datasets, properties, snapshots, scrub; pool create/replace (**destructive** paths) | BR / BW | `zpool`/`zfs` (`-j` JSON on OpenZFS ≥ 2.3, else `-Hp`) | Pool/disk replacement errors; `zpool create -f`; feature upgrades are one-way | OpenZFS kmod + utils | Ubuntu packaged; Debian contrib; Arch/CachyOS via ZFS repos; **Fedora/openSUSE need the OpenZFS repo** (third-party, explicit approval) | M3 → M6 |
| mergerfs | Pools from mounts and fstab/systemd units, branches, policies, free space per branch | Pool definition, branch add/remove, policy changes, mount persistence | U / BR / BW | mount table, `/proc`, xattr control file (`.mergerfs`), config files | Changing branches under writing apps; mixed ownership | mergerfs (*verify* per distro; often upstream release packages) | most | M3 → M6 |
| SnapRAID | Config, data/parity/content files, last sync/scrub, `status`, `diff` | Config edit, **sync**, scrub, fix (**guided only**), scheduled jobs | BR / BW | `snapraid status/diff/smart` parsed; `snapraid.conf` | Sync with missing disks destroys recoverability; parity is point-in-time (spec 03) | snapraid (*verify* per distro) | most | M3 → M6/M9 |
| SMART / NVMe health | Attributes, self-test results, temperatures | Start self-test | BR / BW (`reversible`) | `smartctl --json`, `nvme` | Tests on USB bridges; spurious failures | smartmontools, nvme-cli | all | M3 → M6 |
| Physical Drive Layout Designer (reusing the existing HBA Viewer concept, one shared view; D-12) | Stage A grid + manual mapping; B identity across modules; C enclosure discovery. **Display** serial then WWN. **Stored key** remains M2-5 Stage A `persistent_id` (`wwn:`, `nvme-uuid:`, `serial:`). `/dev/disk/by-id/` refused for bay assignment | D identify LEDs, replacement guidance. Destructive ops re-verify identity at execution and type-confirm the **label serial** | U (A) / BR (B, C) / BW (D) | own model + `/sys/class/enclosure`, `sg_ses`, optional user-installed `storcli` | False confidence in manual maps (labelled unverified); by-id used as a bay key | none (A); sg3_utils, ledmon optional | all | **A: M2**, B: M3, C/D: M9 |
| exFAT | Volume label, cluster size, fsck status | Create (**destructive**), label, fsck. Interop warning: no unix permissions, no checksum RAID | BR / BW | `mkfs.exfat`, `fsck.exfat`, `exfatlabel` (*verify* package names) | mkfs on the wrong device; treated as a general-purpose NAS FS | exfatprogs (*verify*) | most | 04-ROADMAP M5 read → M6 format |
| ZoneFS | Conventional vs sequential zones; zoned block devices | Mount of zoned devices. Not a general-purpose NAS filesystem | BR / BW | ZoneFS mount, zoned-device sysfs | Pretending ZoneFS is ext4/XFS; zoned tools absent | util-linux, zoned kernel support | *verify* | 04-ROADMAP M5 read → M6 format |
| Ceph (logical options, first slice) | Existing cluster health, OSDs, MONs, MGRs, MDS, pools, CRUSH classes (hdd/ssd/nvme); RBD, CephFS, RGW as consumable types | Cluster create is a **later, separately reviewed** write. Cache-tier only if still supported by the installed Ceph — do not advertise removed features | BR / BW (later) | Detect whichever is installed: ceph.conf / cephadm / microceph / rook. Do not invent a second orchestrator | Inventing an orchestrator; advertising removed cache-tier | ceph / cephadm / microceph / rook (*verify*) | *verify* | after 04-ROADMAP **M8**, unslotted backlog (no 04-ROADMAP row); cluster create separately reviewed |

**Owner storage requirements:**
- **mergerfs and SnapRAID** are separate modules and usable alone. The combined setup is a coordinated workflow (`MODULE-ARCHITECTURE.md` §4).
- **ext4/XFS adoption:** existing disks are discovered and adopted with no reformat (mount, label, add as mergerfs branch or SnapRAID data disk).
- **Replacement and SnapRAID recovery** are guided only, with a feasibility check before any action:
  - parity count vs failed disks;
  - content files present on surviving disks;
  - time and age of the last successful sync and scrub;
  - files changed since the last sync (`snapraid diff`);
  - whether the missing disk is really gone, by stable identity and location.

  The UI states clearly what can and cannot be recovered and never runs `fix` automatically.

**Owner storage requirements (2026-11-07, product scope, not implemented):**
- Adapters expose the **native feature set** of each filesystem, not a lowest-common-denominator subset.
- **Beginner** wizard (disks + usage goal → one recommended layout with pros/cons) and **Expert** (full native topology, no reduced feature set) for ZFS, Btrfs, LVM2, mergerfs, Ceph.
- **Tiering only when the tools exist:** ZFS special/L2ARC/SLOG/spare/allocation classes; Btrfs per-profile data/metadata/system (no fake L2ARC); LVM2 cachevol/cachepool writethrough vs writeback and thin pool; mergerfs policy/`minfreespace`/`moveonenospc`; Ceph pools and CRUSH classes.
- Optional isolated **benchmark** is a labelled disruptive ADR 0011 job; never a silent background probe on production volumes.

## Containers
| Technology | Discovery / monitoring | Configuration / lifecycle | Privileges | Backend integration | Main safety risks | Host dependencies | Distro notes | Milestone |
|---|---|---|---|---|---|---|---|---|
| Docker Engine | Engine info, version, storage driver, events. WebUI only if Docker is installed | Daemon settings (later) | BR / BW | Engine API over `/var/run/docker.sock` or rootless `/run/user/<uid>/docker.sock` with stdlib HTTP **from the broker only**. `hc-api` must never join the `docker` group (that is root-equivalent) | Socket access = root; privileged containers | docker engine | Debian/Ubuntu `docker.io`, Fedora `moby-engine`, openSUSE/Arch `docker`; Docker CE repo is third-party (approval) | 2026-10-21: M5; 04-ROADMAP (D-8, after M11 insert): **M12** |
| Containers | List, state, health, ports, mounts, logs, stats | Start/stop/restart/remove (remove = **destructive** for anonymous volumes). Port tracker on every publish/host-port field | BR / BW | Engine API | Removing data volumes; exposing ports | — | — | 2026-10-21: M5; 04-ROADMAP (D-8, after M11 insert): **M12** |
| Compose projects | Projects from labels (`com.docker.compose.project`, config files, working dir) | up/down/pull/restart of **existing** projects; edit only managed/imported files. **2026-11-12:** Traefik easy-setup writes compose when no Traefik is present and hands start/recreate to this module once it exists. **2026-11-07 (product scope, 04-ROADMAP M12):** linter + validator (syntax, Compose spec, known foot-guns); **env editor** (side-by-side YAML + env). Arcane and Dockhand are UX/architecture references, not dependencies. Consult the port-use tracker and **flag** a host/publish port already bound | BR / BW | `docker compose` (v2 plugin) argv templates with validated project dir, over docker.sock or the rootless Docker socket | `down -v` deletes volumes (destructive, separate confirm); editing user files | compose plugin | varies | 2026-10-21: M5; 04-ROADMAP (D-8, after M11 insert): **M12** |
| Images | List, size, dangling, in use | Pull, remove, prune (**destructive**) | BR / BW | Engine API | Prune removes images in use by stopped containers | — | — | M5 |
| Networks | List, drivers, attached containers | Create/remove | BR / BW | Engine API | Breaking connectivity | — | — | M5 |
| Volumes | List, usage, mountpoint, users | Create/remove (**destructive**) | BR / BW | Engine API | Data loss | — | — | M5 |
| Buildx | Builders, platforms | Create/remove builders | BR / BW | `docker buildx ls/…` | Low | buildx plugin | varies | M5 (late) |
| Podman | Engine/objects when installed (containers, images, networks, volumes, play/kube YAML) | Same object split as Docker. Compose/play linter+validator, env editor, port-tracker flags. WebUI hidden if Podman is absent | BR / BW | Podman API / `podman` argv templates from the broker only | Socket/root equivalent; `hc-api` never in a podman socket group | podman | *verify* | 04-ROADMAP **M12** |
| Kubernetes (optional) | Cluster/node/workload inventory; Service/NodePort/hostPort tied into the port tracker. Only if `kubectl` / a local API (k3s, k8s, microk8s, …) is installed | Full cluster-admin UI is later. WebUI hidden if tooling is absent | BR / BW (later) | kubectl / local API | Cluster-admin blast radius | kubectl, k3s/k8s/microk8s (*verify*) | *verify* | 04-ROADMAP **M12** (optional adapter) |

2026-10-21 Docker rows used M2-ROADMAP **M5**. After the 2026-11-07 M11 networking insert, 04-ROADMAP homes containers/VMs at **M12**. Neither numbering is an implementation claim.

## System management
| Technology | Discovery / monitoring | Configuration / lifecycle | Privileges | Backend integration | Main safety risks | Host dependencies | Distro notes | Milestone |
|---|---|---|---|---|---|---|---|---|
| systemd services | Unit list **exists (M1)**; unit detail | start/stop/restart (`disruptive`), enable/disable (`configure`) | BR (list, as `homelab-control`) / BW (root-exec) | `systemctl` argv templates | Lockout, self-termination (protected units, D-5) | systemd | all supported | **M2** |
| Timers / sockets | List, next/last trigger | enable/disable, start | BR / BW | `systemctl list-timers/list-sockets --output=json` | Low–medium | systemd | all | M2 (read) → M2/M3 |
| Journal / logs | Per-unit logs | — | BR (journal group) | `journalctl --unit=… -o json` | Sensitive content (scope `services.logs`) | systemd-journald | all | **M2 (PR-1)** |
| Packages: search, install, remove | Installed packages, available versions | Install/remove with transaction preview | BR / BW | apt/dpkg, dnf/rpm, zypper, pacman adapters (or PackageKit where present, *verify*) | Removing essential packages; locks held by other tools; partial transactions | native PM | per-family adapter | M4 |
| Package updates, repositories | Pending updates, repo list | Apply updates, add/remove repos — **subject to separate scope approval (owner)** | BR / BW | same | Untrusted repos, keys; reboot-required | native PM | per-family | M4 (needs approval) |
| Host settings (hostname, timezone) | Current values | Set hostname/timezone | U/BR / BW | `hostnamectl`, `timedatectl` | Low; hostname affects certificates/mDNS | systemd | all | M2 optional (D-9) |
| Users and groups | Local accounts, groups, sudo membership | Create/modify/lock, group membership | U (`/etc/passwd`, `/etc/group`) / BW | `useradd/usermod/groupadd`, `passwd -l` | Lockout, privilege escalation via groups, UID/GID reuse, home deletion | shadow-utils | all | Own milestone (after M2) |

## Virtualization
| Technology | Discovery / monitoring | Configuration / lifecycle | Privileges | Backend integration | Main safety risks | Host dependencies | Distro notes | Milestone |
|---|---|---|---|---|---|---|---|---|
| libvirt / QEMU-KVM: VMs | Domains, state, vCPU/memory, disks, NICs. WebUI only if libvirt/QEMU is installed | start/shutdown/reboot/force-off, define/undefine, snapshots where supported | BR / BW | libvirt API via `virsh` argv templates (or pure-Go `go-libvirt`, D-11); **never raw QEMU** | Force-off data loss; undefine with storage removal (**destructive**) | libvirt, qemu | all | 2026-10-21: M8; 04-ROADMAP (D-8, after M11 insert): **M12** |
| Virtual networks | Networks, bridges, DHCP leases | Create/start/stop/autostart | BR / BW | libvirt | Host network disruption | libvirt | all | M8 |
| Virtual storage | Pools, volumes | Create pools/volumes; delete (**destructive**) | BR / BW | libvirt | Deleting volumes | libvirt | all | M8 |

## File sharing and connectivity (addon specs 01–08 apply)
| Technology | Discovery / monitoring | Configuration / lifecycle | Privileges | Backend integration | Main safety risks | Host dependencies | Distro notes | Milestone |
|---|---|---|---|---|---|---|---|---|
| SMB / Samba | Existing shares (`testparm -s`), sessions (`smbstatus`) | Share create/edit (managed fragments), Samba users | BR / BW | samba tools, managed include file | Guest/world-writable exposure; overwriting unmanaged config | samba | all | M7 |
| SFTP (OpenSSH) | Effective config (`sshd -T`), Match blocks, chroot validity | Managed `Match` drop-in, chroot dirs, keys | BR / BW | OpenSSH, `sshd -t` validation, reload | **SSH lockout**; chroot ownership rules | openssh-server | all (Fedora/openSUSE/Arch package names differ) | M7 |
| NFS (existing spec) | Exports, clients | Exports with CIDR, root_squash | BR / BW | `exportfs` | Broad exports | nfs-utils / nfs-kernel-server | all | M7 |
| WebDAV | Apache `mod_dav` sites | Managed vhost/location, auth | BR / BW | Apache config + `apachectl configtest` | Unauthenticated writable DAV | apache2/httpd | all | M7 |
| FTP / FTPS | vsftpd config | Managed config, TLS, passive ports | BR / BW | vsftpd | Plain FTP credentials; anonymous write | vsftpd | all | M7 (opt-in) |
| iSCSI target | LIO targets, LUNs, ACLs, sessions | Backstores, targets, ACLs, CHAP | BR / BW | `targetcli` / saveconfig JSON | Exposing a mounted filesystem's device; no auto-format | targetcli-fb | *verify* names per family | M7 (late) |
| Thunderbolt | Devices, authorization state, security level, domains (sysfs `/sys/bus/thunderbolt`) | Authorize/enroll/forget (`boltctl`) | U / BW | `bolt` daemon via `boltctl` | DMA-capable devices: authorization is a security decision | bolt | all | M9 (read earlier) |

**Integration rules (owner):**
- **Docker:** Compose projects, containers, images, networks, volumes and Buildx builders are separate views and ops. `hc-api` never joins the `docker` group.
- **libvirt:** use its APIs. Never raw QEMU.
- **File sharing:** detect existing Samba and other service configs before any change.
- **Packages:** respect native tools, locks and transactions.
- **Thunderbolt:** separates authorization from connectivity and from storage/network functions.
- **SFTP:** respects the existing OpenSSH configuration and authentication policy.
- **Installed-tooling gate (2026-11-07):** WebUI only when the matching tooling is present; Settings install stays informational until M4; no silent repos.
- **Port-use tracker (2026-11-07):** Compose / Podman / Kubernetes editors consult the host-wide map and flag a host/publish port already bound.
- **Arcane and Dockhand:** UX and architecture references for Docker + Compose (socket, project directory, visual/YAML editor, env editor, up/down); not dependencies and not copies. Out of scope unless the owner later asks: Git-sync, Restic, Grype/Trivy, multi-remote Docker hosts, in-container file browser.

## Networking

Installed-tooling gate applies. **04-ROADMAP M11** (2026-11-07 owner slot). Not M2. Not M3. First slice: port-use tracker **read-only**, LACP/bonding **detect**, firewall WebUI for the **active** backend only.

| Technology | Discovery / monitoring | Configuration / lifecycle | Privileges | Backend integration | Main safety risks | Host dependencies | Distro notes | Milestone (04-ROADMAP) |
|---|---|---|---|---|---|---|---|---|
| Port-use tracker | Host-wide map of listening ports and the process/unit/container that owns them. Sources: host sockets (`ss`/netns), Docker, Podman, Kubernetes when installed | Read-only first. Changing bindings is a write in the owning module | U / BR | `ss`, netns, Docker/Podman/K8s APIs when present | Missing a bound port so the editor does not flag a conflict | iproute2 | all | **M11** (read-only first) |
| Bonds / LACP | Existing bonds, modes, slaves, LACP (802.3ad) and other kernel bonding modes the installed stack supports. **Detect** first | Bond create/change is a later write. Prefer the host’s existing renderer (NetworkManager vs systemd-networkd vs ifcfg); detect first, do not rewrite an unmanaged stack | BR / BW (later write) | `iproute2`, NetworkManager, systemd-networkd, ifcfg as detected | Rewriting an unmanaged stack; dropping the management link | iproute2; NM or networkd | all | **M11** (detect) |
| firewalld | Zones, services, ports, rich rules, runtime vs permanent. UI only if this is the **active** backend | Rule changes with preview | BR / BW | `firewall-cmd` | Lockout; showing five firewall UIs at once | firewalld | *verify* | **M11** (active backend) |
| ufw | Rules, defaults, numbered delete. UI only if this is the **active** backend | Rule changes with preview | BR / BW | `ufw` | Lockout | ufw | *verify* | **M11** (active backend) |
| nftables | Tables/chains/sets (`nft`). No silent translation into iptables. UI only if this is the **active** backend | Rule changes with preview | BR / BW | `nft` | Lockout; translating nft into iptables | nftables | *verify* | **M11** (active backend) |
| iptables / ip6tables / legacy | Tables/chains **if nft is not the actual backend**. UI only if this is the **active**/legacy stack | Rule changes with preview | BR / BW | iptables-legacy / ip6tables | Editing a nft-compat shim as if it were iptables | iptables | *verify* | **M11** (active backend) |
| bpfilter / Bumblebee | Only if that stack is installed, queryable, and **active**; otherwise hidden | Later write | BR / BW (later) | queryable bpfilter/Bumblebee stack | Showing a UI for software that is not installed | *verify* | *verify* | **M11** (active backend) |
| XDP | Programs attached to interfaces (`bpftool` / `ip link`). UI only if queryable | Load/replace is a later write | BR / BW (later) | bpftool, ip link | Replacing an XDP program on the management NIC | bpftool, iproute2 | *verify* | **M11** (later write) |
