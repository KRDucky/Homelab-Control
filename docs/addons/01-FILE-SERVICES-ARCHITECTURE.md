# File Services Architecture — Server-side

## Purpose
A cohesive **Shares & Services** center supporting Samba/SMB, NFS, WebDAV, iSCSI targets, FTP, FTPS and SFTP. Each protocol is an optional adapter with capability discovery, installation, config import, read-only inspection, plan/apply, status, logs, client testing, backup and restore. Do not implement protocol servers from scratch.

## Recommended backends
| Service | Default native implementation | Alternatives / notes |
|---|---|---|
| SMB | Samba (`smbd`, `testparm`, `pdbedit`) | Existing Webmin Samba config may be imported carefully |
| NFS | Linux kernel NFS server (`nfs-utils` / `nfs-kernel-server`, `exportfs`) | NFS-Ganesha optional advanced adapter |
| WebDAV | Apache HTTP Server `mod_dav` + `mod_dav_fs` | Caddy WebDAV plugin only when verified; proxy TLS separate |
| iSCSI target | LIO via `targetcli-fb` / `rtslib-fb` | Fileio/block backstores; no raw destructive defaults |
| FTP/FTPS | vsftpd | ProFTPD optional; explicit TLS for FTPS |
| SFTP | OpenSSH `sshd` internal-sftp | Use Match blocks and dedicated groups, not FTP daemon |

## Common domain model
`Share`: stable UUID, display name, backend, source path/backstore ID, enabled state, mount/dependency graph, access policy, network listeners, service instance, managed-config provenance, health and audit references. `Export` is protocol-specific configuration of a share; one directory may have multiple exports only after ACL conflict analysis. `ClientGrant`: principal/group, client host/CIDR, rights, auth mechanism, effective permissions. `ServiceInstance`: backend/version/capabilities/config ownership. `StorageTarget`: validated canonical local path or iSCSI backstore.

## UX
- Shares list grouped by protocol, storage path, and clients; separate **Server Shares** and **Remote Mounts**.
- Wizard: choose path or new dataset/directory → protocol → principals/client CIDRs → read/write → authentication → service validation → network exposure preview → apply → test.
- Import existing unmanaged shares read-only; require opt-in takeover of explicitly selected config fragments.
- Display filesystem ACL vs protocol ACL effective-access check, symlink traversal, UID/GID mappings, SELinux/AppArmor status, service ports and firewall rules.
- Link share to ZFS dataset, Btrfs subvolume, mdraid filesystem, mergerfs branch, or plain directory using resource graph.
- Prevent deletion of underlying data by default; removing an export only removes export configuration, never files.

## Protocol-specific requirements
**Samba:** workgroup, standalone server role initially, share browsing, guest disabled by default, valid users/write list, SMB3 preference, disable SMB1 by default, Samba password management through supported APIs, `testparm` validation, status via `smbstatus`. Warn about Samba ACL vs POSIX ACL and symlink escape.

**NFS:** v4-first where feasible, explicit client CIDR/hostname, `root_squash` default, `no_subtree_check` policy based on distro and use, `exportfs -ra` after validation, no unrestricted `*` by default, explain UID/GID and NFSv4 idmap; NFS-Ganesha separate provider.

**WebDAV:** HTTPS by default, Apache auth integration, path-level write limits, DAV lock database, protect against path traversal and accidental source disclosure. Never expose an unauthenticated writable DAV endpoint.

**iSCSI:** distinguish target vs initiator. Support IQN naming, portals, target ACLs, CHAP/mutual CHAP where supported, LUN/backstore planning, persistent configuration and session visibility. Treat block LUNs as exclusive storage: warn strongly against simultaneously mounting the same writable filesystem on server and initiator; no automatic formatting.

**FTP/FTPS:** prefer SFTP when possible; FTP is opt-in. FTPS requires valid TLS, passive port range and firewall preview, chroot policies, per-user paths, no anonymous writes, distinguish explicit vs implicit FTPS. Credential policy and transfer logging.

**SFTP:** SSHD Match User/Group, `internal-sftp`, chroot ownership constraints, authorized keys, deny unintended shell access, validate with `sshd -t`, reload carefully with emergency session preservation.

## Boundaries
No direct root from web tier. No automatic service restarts if unmanaged settings could be disrupted. No unsupported guarantee of atomic rollback. No secret leakage or generic shell execution. Only enable listeners with explicit authorization.
