# Implementation Roadmap and Acceptance Tests

## Dependency-aware milestones (do not override current M1)
- **A0 Research**: read-only source audits and distro package capability matrix; design ADRs, no privileged writes. Can run in parallel with M1.
- **A1 Inventory**: detect installed server/client software, existing exports, sessions, services, listeners, remote mounts, proxy daemons. No modification.
- **A2 Shared identity/access model**: principal and ACL inspector; import-only config readers; plan/diff UX; secrets abstraction.
- **A3 Mount manager**: read-only inventory, then approved SMB/NFS mount creation; systemd mount/automount; safe disconnect. SFTP/WebDAV/FTP/FUSE later.
- **A4 File services**: Samba and NFS first, then SFTP, WebDAV, FTPS/FTP. Native validation and backups.
- **A5 iSCSI**: initiator inventory/discovery, then target inventory, then carefully reviewed create/update operations; never auto-format LUN.
- **A6 Proxy adapters**: Caddy first, Nginx next, Traefik third; NPM-inspired UX; ACME and Cloudflare DDNS independent.
- **A7 Polish**: permissions simulations, import migrations, advanced mounts, HA/multipath, logs, alerting, docs, localization.

## Required integration test matrix
Ubuntu LTS, Debian stable, Fedora current, openSUSE Leap/Tumbleweed where practical, Arch rolling. Use ephemeral VMs with systemd, network namespaces, disposable users/groups and data directories; for iSCSI use loop-backed virtual block devices and isolated initiator/target VMs. Test SELinux enforcing and AppArmor where present.

## Protocol acceptance
- Samba: create authenticated share, user/group grant, SMB3 connect from Linux client, effective ACL tests, config validation, removal leaves data intact.
- NFS: restricted client export, root_squash, v4 mount, idmap/permissions, reload, offline handling.
- WebDAV: HTTPS + auth, read/write/lock test, failed credentials, rollback.
- SFTP: SSH key auth, chroot ownership validation, Match isolation, shell restriction, host key trust.
- FTP/FTPS: explicit TLS, passive ports, no anonymous write, chroot, firewall preview.
- iSCSI: isolated target + initiator, CHAP, discovery, session login, LUN stable identity, disconnect; verify no data corruption and no autoformat.
- Remote mounts: offline server, DNS failure, expired credentials, unmount busy, boot ordering, no UI hang.
- Proxies: backend config lint, TLS renew staging, port conflicts, access list, safe reload, unmanaged-config preservation, SSRF defense.

## Security and release gates
Threat model, RBAC/CSRF, fuzz malformed input, symlink race tests, privilege escalation tests, secret redaction, reproducible package tests, dependency provenance, config backup/restore, test evidence committed. Features are not marked complete until tested on real disposable OS environments.
