# Remote Connections & Mount Manager

## Protocol clients
| Remote protocol | Client/backend | Notes |
|---|---|---|
| SMB/CIFS | `mount.cifs` / `cifs-utils` | SMB3, credentials file/systemd credentials, uid/gid, ACL semantics |
| NFSv3/v4 | kernel NFS client (`mount.nfs`) | v4 preferred, sec= options, idmap, server export visibility |
| WebDAV | `davfs2` or `rclone mount` | HTTPS, credentials, FUSE limits, caching/locking differences |
| iSCSI | `open-iscsi` / `iscsiadm` | discovery, CHAP, session login, multipath optional, **block device not filesystem** |
| SFTP | `sshfs` where maintained/available; `rclone mount` alternative | SSH host-key pinning, key auth, FUSE and disconnect behavior |
| FTP/FTPS | `rclone mount` with supported remote config, or other verified FUSE client | Do not pretend native kernel mount exists; warn about weak semantics |

## Workflow
1. New connection: choose protocol, host/IP, port, security mode, share/export/remote path.
2. Credential wizard: store password/token/key in OS-protected secret store; require SSH host key and TLS verification, no insecure default.
3. Probe with timeout, list available shares where permitted; never scan arbitrary networks without explicit target.
4. Select local mount path and conflict check (symlinks, existing files, nested mounts, container bind dependencies).
5. Select manual, on-demand, at-boot, or automount; prefer systemd `.mount`/`.automount` or validated fstab entries according to distro capabilities.
6. Preview mount options: `_netdev`, `nofail`, `x-systemd.automount`, timeouts, read-only vs read-write, UID/GID mapping, caching; avoid blind defaults for iSCSI.
7. Apply through typed broker, verify with mountinfo and read-only test, show service/automount status and audit.
8. Disconnect with open-file checks and dependent-service warnings; never force unmount by default.

## iSCSI initiator workflow
Discover portals/targets → authenticate → establish session → identify stable `/dev/disk/by-path`/WWID → **stop before partition/format** → explicit separate storage wizard if user owns the LUN. Include multipath and stale session handling. Never assume discovered LUN is safe to overwrite.

## Operational hazards
- Remote offline must not freeze dashboard: bound `statfs`/filesystem probes; avoid synchronous traversal of FUSE and dead NFS mounts.
- Remote mounts can be shared into containers only after warning about restart/order/dependencies.
- Provide reconnect/backoff, network-online ordering, health diagnostics, DNS/TLS/SSH fingerprint errors, stale mount warnings.
- Different protocols do not provide equivalent POSIX permissions, locking, sparse files, hardlinks, fsync or atomic rename. Surface capability differences in UI.
- Never put credentials in fstab inline, URL, argv, logs, or export archives.
