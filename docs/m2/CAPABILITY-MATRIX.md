# Module capability matrix

**Date:** 2026-10-21 · **Status:** planning. Product-scope requirements (owner, 2026-10-21); **nothing in
this table is implemented** unless the row says so.

Milestone names refer to the proposed sequence in `M2-ROADMAP.md` §4 (owner decision D-8).

Privilege column:
- **U** = unprivileged in `hc-api`;
- **BR** = broker read op;
- **BW** = broker write op (plan/apply, ADR 0011 proposed).

Package names vary by release and must be resolved at runtime (spec 06). Entries marked *verify* were
not confirmed for every family during this review.

## Storage and filesystems
| Technology | Discovery / monitoring | Configuration / lifecycle | Privileges | Backend integration | Main safety risks | Host dependencies | Distro notes | Milestone (read → write) |
|---|---|---|---|---|---|---|---|---|
| Disk inventory (all) | Disks, partitions, identity (WWN/serial/by-id), transport, size; **exists in M1** (`hwinfo`) | — | U | sysfs, `/dev/disk/by-id` | Volatile kernel names (labelled) | none | all | done (M1) → — |
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
| Physical Drive Layout Designer (built from the existing HBA Viewer concept, one shared view) | Stage A grid + manual mapping; B identity across modules; C enclosure discovery | D identify LEDs, replacement guidance | U (A) / BR (B, C) / BW (D) | own model + `/sys/class/enclosure`, `sg_ses`, optional user-installed `storcli` | False confidence in manual maps (labelled unverified) | none (A); sg3_utils, ledmon optional | all | **A: M2**, B: M3, C/D: M9 |

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

## Containers
| Technology | Discovery / monitoring | Configuration / lifecycle | Privileges | Backend integration | Main safety risks | Host dependencies | Distro notes | Milestone |
|---|---|---|---|---|---|---|---|---|
| Docker Engine | Engine info, version, storage driver, events | Daemon settings (later) | BR / BW | Engine API over `/var/run/docker.sock` with stdlib HTTP **from the broker only**. `hc-api` must never join the `docker` group (that is root-equivalent) | Socket access = root; privileged containers | docker engine | Debian/Ubuntu `docker.io`, Fedora `moby-engine`, openSUSE/Arch `docker`; Docker CE repo is third-party (approval) | M5 |
| Containers | List, state, health, ports, mounts, logs, stats | Start/stop/restart/remove (remove = **destructive** for anonymous volumes) | BR / BW | Engine API | Removing data volumes; exposing ports | — | — | M5 |
| Compose projects | Projects from labels (`com.docker.compose.project`, config files, working dir) | up/down/pull/restart of **existing** projects; edit only managed/imported files | BR / BW | `docker compose` (v2 plugin) argv templates with validated project dir | `down -v` deletes volumes (destructive, separate confirm); editing user files | compose plugin | varies | M5 |
| Images | List, size, dangling, in use | Pull, remove, prune (**destructive**) | BR / BW | Engine API | Prune removes images in use by stopped containers | — | — | M5 |
| Networks | List, drivers, attached containers | Create/remove | BR / BW | Engine API | Breaking connectivity | — | — | M5 |
| Volumes | List, usage, mountpoint, users | Create/remove (**destructive**) | BR / BW | Engine API | Data loss | — | — | M5 |
| Buildx | Builders, platforms | Create/remove builders | BR / BW | `docker buildx ls/…` | Low | buildx plugin | varies | M5 (late) |

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
| libvirt / QEMU-KVM: VMs | Domains, state, vCPU/memory, disks, NICs | start/shutdown/reboot/force-off, define/undefine, snapshots where supported | BR / BW | libvirt API via `virsh` argv templates (or pure-Go `go-libvirt`, D-11); **never raw QEMU** | Force-off data loss; undefine with storage removal (**destructive**) | libvirt, qemu | all | M8 |
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
- **Docker:** Compose projects, containers, images, networks, volumes and Buildx builders are separate views and ops.
- **libvirt:** use its APIs.
- **File sharing:** detect existing Samba and other service configs before any change.
- **Packages:** respect native tools, locks and transactions.
- **Thunderbolt:** separates authorization from connectivity and from storage/network functions.
- **SFTP:** respects the existing OpenSSH configuration and authentication policy.
