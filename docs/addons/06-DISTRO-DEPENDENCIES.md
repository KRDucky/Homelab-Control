# Distro Dependencies and Installation Policy

## Supported families
Ubuntu/Debian APT, Fedora DNF, openSUSE Zypper, Arch/CachyOS pacman. Package names and availability vary by release; resolve dynamically against enabled repositories and verified vendor catalogs. This table is illustrative, **not a promise of package availability**.

| Capability | Debian/Ubuntu examples | Fedora/openSUSE examples | Arch examples |
|---|---|---|---|
| Samba | samba | samba | samba |
| NFS server | nfs-kernel-server | nfs-utils | nfs-utils |
| NFS client | nfs-common | nfs-utils | nfs-utils |
| CIFS client | cifs-utils | cifs-utils | cifs-utils |
| WebDAV server | apache2 + modules | httpd + modules | apache + modules |
| WebDAV mount | davfs2 | davfs2 | davfs2 |
| iSCSI target | targetcli-fb | targetcli / targetcli-fb (verify) | targetcli-fb (verify) |
| iSCSI initiator | open-iscsi | iscsi-initiator-utils / open-iscsi (verify) | open-iscsi |
| FTP/FTPS | vsftpd | vsftpd | vsftpd |
| SFTP | openssh-server | openssh-server / openssh | openssh |
| FUSE SFTP | sshfs / rclone | sshfs / rclone | sshfs / rclone |
| Reverse proxy | caddy/nginx/traefik (availability varies) | same | same |

## Resolver behavior
Detect OS ID/ID_LIKE/version, active package manager, installed binaries and versions, services, kernel modules, SELinux/AppArmor, systemd, ports and repositories. Show **Install missing component** only when a verified source is available. Present native transaction plan and package provenance. Offer vendor repo only if curated, signed, distro/version compatible and explicitly approved; otherwise show official download docs. Do not use arbitrary AUR packages or curl-pipe-shell automatically. Uninstall must never remove data, users or secrets without separate approval.

## SELinux, firewall and systemd
Fedora/openSUSE often require SELinux/firewalld-specific handling; openSUSE may use AppArmor and different service names. Detect rather than assume. Preview firewall changes, contexts/booleans, systemd unit dependencies and port ranges. Default to no public exposure.
