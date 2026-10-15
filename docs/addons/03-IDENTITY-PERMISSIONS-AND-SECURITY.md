# Identity, Share Access & Security

## Identity sources
Local Unix users/groups (NSS/PAM), Samba account database, SSH authorized keys, WebDAV/HTTP auth identities, FTP virtual users, iSCSI initiator IQNs and CHAP credentials, optional LDAP/FreeIPA/AD connectors. Do not equate all these identities or silently synchronize passwords.

## Permission engine
Represent effective access as the intersection of: host filesystem owner/group/mode and POSIX ACL; protocol-specific grants; export client restrictions; authentication; SELinux/AppArmor policy; mount options; network/firewall reachability. Show why an access check passes/fails and which layer denies it. Keep source of truth per backend; no speculative chmod/chown.

## Wizard safeguards
- Create dedicated service groups/users with explicit UID/GID and home directory planning.
- Avoid recursively changing ownership or ACLs unless scope, file count, and consequences are previewed; large trees require cancellable background jobs and progress.
- Samba shares default authenticated, no guest, SMB1 disabled; NFS root squash; WebDAV HTTPS; SFTP host key verification; FTPS TLS; iSCSI CHAP where appropriate.
- Restrict exports by CIDR, host, user, group, IQN and service capabilities; warn about broad grants.
- Separate `shares.read`, `shares.configure`, `shares.permissions`, `shares.credentials`, `remote-mounts.read`, `remote-mounts.configure`, `iscsi.target.configure`, `iscsi.initiator.configure`, `network.firewall.configure` scopes.
- Require extra confirmation for world-writable exports, public listeners, recursive ACL changes, iSCSI block writes, and actions risking lockout.

## Security review checklist
Symlink/TOCTOU defense, canonical paths and directory handles; stable disk IDs; host/path/port input validation; service config parser validation; credentials redaction; authorization per resource and operation; audit before/after; lock domains; SELinux/AppArmor policy compatibility; TLS/SSH trust; rate limits; rollback from config backup when valid; refusal to touch unmanaged config without import approval.
