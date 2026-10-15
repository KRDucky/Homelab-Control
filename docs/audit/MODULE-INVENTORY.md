# Webmin 2.670 module inventory and fork disposition

Generated from every `*/module.info` in `compat/webmin` at 2.670 (126 modules). Disposition is a *plan*,
not a statement of current capability: in M0 **no** Webmin module is reachable through the new API.

Legend: **Replace**: a new typed adapter + UI supersedes it (legacy stays until parity). **Bridge**: candidate for an audited, typed, read-only endpoint backed by the broker. **Keep in legacy UI**: shipped and usable through upstream's interface only. **Never bridged**: functionality is arbitrary execution/file access by design. **Drop from default build**: excluded from the homelab package module list (still in source).
## Summary

| Disposition | Modules |
|---|---|
| Keep in legacy UI | 54 |
| Drop from default build | 28 |
| Replace | 19 |
| Never bridged | 8 |
| Legacy admin | 7 |
| Bridge (read-only) | 7 |
| Bridge (later) | 3 |

Notable gap: upstream Webmin has **no ZFS, Btrfs, mergerfs, SnapRAID, Docker/Podman, libvirt, Caddy, Traefik, Tailscale, cooling or bay-mapping module**. These are new work, not migrations.


| Module | Category | Description | Disposition | Notes |
|---|---|---|---|---|
| `acl` | webmin | Webmin Users | Legacy admin | Webmin's own user/ACL/config/RPC administration; not mapped to the new RBAC |
| `adsl-client` | net | ADSL Client | Drop from default build | Obsolete for homelab targets / demo |
| `apache` | servers | Apache Webserver | Replace | M10 reverse-proxy manager (Caddy/Nginx/Traefik adapters); apache legacy-only |
| `at` | system | Scheduled Commands | Bridge (later) | Scheduled jobs view; write paths need broker ops + audit |
| `backup-config` | webmin | Backup Configuration Files | Keep in legacy UI | M12 backups (Restic/Borg/ZFS send) are new; backup-config needed for legacy migration |
| `bacula-backup` | system | Bacula Backup System | Keep in legacy UI | M12 backups (Restic/Borg/ZFS send) are new; backup-config needed for legacy migration |
| `bandwidth` | net | Bandwidth Monitoring | Drop from default build | Obsolete for homelab targets / demo |
| `bind8` | servers | BIND DNS Server | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `bsdexports` | net | NFS Exports | Drop from default build | Non-Linux or obsolete |
| `bsdfdisk` | hardware | Partitions on Local Disks | Drop from default build | Non-Linux or obsolete |
| `change-user` | webmin | Change Language and Theme | Legacy admin | Webmin's own user/ACL/config/RPC administration; not mapped to the new RBAC |
| `cluster-copy` | cluster | Cluster Copy Files | Drop from default build | Multi-host is M12+ and will not reuse Webmin RPC |
| `cluster-cron` | cluster | Cluster Cron Jobs | Drop from default build | Multi-host is M12+ and will not reuse Webmin RPC |
| `cluster-passwd` | cluster | Cluster Change Passwords | Drop from default build | Multi-host is M12+ and will not reuse Webmin RPC |
| `cluster-shell` | cluster | Cluster Shell Commands | Never bridged | Arbitrary command execution / root file access / outbound fetch by design |
| `cluster-software` | cluster | Cluster Software Packages | Drop from default build | Multi-host is M12+ and will not reuse Webmin RPC |
| `cluster-useradmin` | cluster | Cluster Users and Groups | Drop from default build | Multi-host is M12+ and will not reuse Webmin RPC |
| `cluster-usermin` | cluster | Cluster Usermin Servers | Drop from default build | Multi-host is M12+ and will not reuse Webmin RPC |
| `cluster-webmin` | cluster | Cluster Webmin Servers | Drop from default build | Multi-host is M12+ and will not reuse Webmin RPC |
| `cpan` | — | Perl Modules | Never bridged | Arbitrary command execution / root file access / outbound fetch by design |
| `cron` | system | Scheduled Cron Jobs | Bridge (later) | Scheduled jobs view; write paths need broker ops + audit |
| `custom` | — | Custom Commands | Never bridged | Arbitrary command execution / root file access / outbound fetch by design |
| `dfsadmin` | net | NFS Shares | Drop from default build | Non-Linux or obsolete |
| `dhcpd` | servers | DHCP Server | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `dovecot` | servers | Dovecot IMAP/POP3 Server | Keep in legacy UI | Mail stack is out of homelab core scope |
| `exim` | servers | Exim Mailserver | Keep in legacy UI | Mail stack is out of homelab core scope |
| `exports` | net | NFS Exports | Keep in legacy UI | Future SMB/NFS/iSCSI sharing plugins |
| `fail2ban` | net | Fail2Ban Intrusion Detector | Keep in legacy UI | Bridge selectively later (catalog: System security / Identity) |
| `fdisk` | hardware | Partitions on Local Disks | Replace | M5/M6 disk inventory + partition planner (typed adapter; lsblk/udev identity, never /dev/sdX) |
| `fetchmail` | servers | Fetchmail Mail Retrieval | Keep in legacy UI | Mail stack is out of homelab core scope |
| `filemin` | — | File Manager | Never bridged | Arbitrary command execution / root file access / outbound fetch by design |
| `filter` | mail | Filter and Forward Mail | Keep in legacy UI | Mail stack is out of homelab core scope |
| `firewall` | net | Linux Firewall (iptables) | Replace | Networking/firewall with timed-rollback safeguard |
| `firewall6` | net | Linux Firewall (ip6tables) | Replace | Networking/firewall with timed-rollback safeguard |
| `firewalld` | net | FirewallD | Replace | Networking/firewall with timed-rollback safeguard |
| `format` | hardware | Partitions on Local Disks | Replace | M5/M6 disk inventory + partition planner (typed adapter; lsblk/udev identity, never /dev/sdX) |
| `fsdump` | system | Filesystem Backup | Keep in legacy UI | M12 backups (Restic/Borg/ZFS send) are new; backup-config needed for legacy migration |
| `grub2` | hardware | GRUB 2 Boot Loader | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `hardware-info` | hardware | Hardware Information | Bridge (read-only) | M1 candidate: new in 2.670, read-only by nature; reference for M5 inventory |
| `heartbeat` | cluster | Heartbeat Monitor | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `help` | webmin | Webmin Help | Legacy admin | Webmin's own user/ACL/config/RPC administration; not mapped to the new RBAC |
| `hpuxexports` | net | NFS Exports | Drop from default build | Non-Linux or obsolete |
| `htaccess-htpasswd` | — | Protected Web Directories | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `htpasswd-file` | servers | Manage HTPasswd File | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `idmapd` | net | idmapd daemon | Keep in legacy UI | Future SMB/NFS/iSCSI sharing plugins |
| `inetd` | net | Network Services and Protocols | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `init` | system | Bootup and Shutdown | Replace | Services module (systemd D-Bus adapter); init for non-systemd stays legacy |
| `inittab` | system | Initial System Bootup | Drop from default build | Non-Linux or obsolete |
| `ipfilter` | net | IPFilter Firewall | Drop from default build | Non-Linux or obsolete |
| `ipfw` | net | BSD Firewall | Drop from default build | Non-Linux or obsolete |
| `ipsec` | net | IPsec VPN Configuration | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `iscsi-client` | hardware | iSCSI Client | Keep in legacy UI | Future SMB/NFS/iSCSI sharing plugins |
| `iscsi-server` | hardware | iSCSI Server | Keep in legacy UI | Future SMB/NFS/iSCSI sharing plugins |
| `iscsi-target` | hardware | iSCSI Target | Keep in legacy UI | Future SMB/NFS/iSCSI sharing plugins |
| `iscsi-tgtd` | hardware | iSCSI TGTd | Keep in legacy UI | Future SMB/NFS/iSCSI sharing plugins |
| `kea-dhcp` | servers | Kea DHCP Server | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `krb5` | net | Kerberos5 | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `ldap-client` | system | LDAP Client | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `ldap-server` | servers | LDAP Server | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `ldap-useradmin` | system | LDAP Users and Groups | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `logrotate` | system | Log File Rotation | Keep in legacy UI | Bridge selectively later (catalog: System security / Identity) |
| `logviewer` | system | System Logs | Bridge (read-only) | M1/M2 read-only views (journal, processes, status) via typed adapters |
| `lpadmin` | hardware | Printer Administration | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `lvm` | hardware | Logical Volume Management | Replace | LVM discovery adapter (catalog: staged safety gates) |
| `mailboxes` | servers | Read User Mail | Keep in legacy UI | Mail stack is out of homelab core scope |
| `mailcap` | system | MIME Type Programs | Keep in legacy UI | Mail stack is out of homelab core scope |
| `man` | system | System Documentation | Drop from default build | Obsolete for homelab targets / demo |
| `minecraft` | servers | Minecraft Server | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `mount` | system | Disk and Network Filesystems | Replace | M6 mount planner; read-only mount view already in M0 overview |
| `mysql` | servers | MySQL Database Server | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `net` | net | Network Configuration | Replace | Networking module (NetworkManager/systemd-networkd/netplan aware) |
| `nftables` | net | Linux Firewall | Replace | Networking/firewall with timed-rollback safeguard |
| `nginx` | servers | Nginx Webserver | Replace | M10 reverse-proxy manager (Caddy/Nginx/Traefik adapters); apache legacy-only |
| `nis` | net | NIS Client and Server | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `openslp` | servers | OpenSLP Server | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `package-updates` | system | Software Package Updates | Replace | M4 Software Center (APT/DNF/zypper/pacman adapters with transaction preview) |
| `pam` | system | PAM Authentication | Keep in legacy UI | Bridge selectively later (catalog: System security / Identity) |
| `pap` | net | PPP Dialin Server | Drop from default build | Obsolete for homelab targets / demo |
| `passwd` | system | Change Passwords | Replace | Identity module; useradmin parsers are a parity reference |
| `phpini` | — | PHP Configuration | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `postfix` | servers | Postfix Mail Server | Keep in legacy UI | Mail stack is out of homelab core scope |
| `postgresql` | servers | PostgreSQL Database Server | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `ppp-client` | net | PPP Dialup Client | Drop from default build | Obsolete for homelab targets / demo |
| `pptp-client` | net | PPTP VPN Client | Drop from default build | Obsolete for homelab targets / demo |
| `pptp-server` | net | PPTP VPN Server | Drop from default build | Obsolete for homelab targets / demo |
| `proc` | system | Running Processes | Bridge (read-only) | M1/M2 read-only views (journal, processes, status) via typed adapters |
| `procmail` | servers | Procmail Mail Filter | Keep in legacy UI | Mail stack is out of homelab core scope |
| `proftpd` | servers | ProFTPD Server | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `qmailadmin` | servers | QMail Mail Server | Keep in legacy UI | Mail stack is out of homelab core scope |
| `quota` | system | Disk Quotas | Keep in legacy UI | Bridge selectively later (catalog: System security / Identity) |
| `raid` | hardware | Linux RAID | Replace | M7 mdraid adapter |
| `rbac` | system | RBAC and Projects | Drop from default build | Non-Linux or obsolete |
| `samba` | servers | Samba Windows File Sharing | Keep in legacy UI | Future SMB/NFS/iSCSI sharing plugins |
| `sarg` | servers | Squid Report Generator | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `sendmail` | servers | Sendmail Mail Server | Keep in legacy UI | Mail stack is out of homelab core scope |
| `servers` | webmin | Webmin Servers Index | Legacy admin | Webmin's own user/ACL/config/RPC administration; not mapped to the new RBAC |
| `sgiexports` | net | NFS Exports | Drop from default build | Non-Linux or obsolete |
| `shell` | — | Command Shell | Never bridged | Arbitrary command execution / root file access / outbound fetch by design |
| `shorewall` | net | Shorewall Firewall | Drop from default build | Obsolete for homelab targets / demo |
| `shorewall6` | net | Shorewall6 Firewall | Drop from default build | Obsolete for homelab targets / demo |
| `smart-status` | hardware | SMART Drive Status | Replace | M5 SMART/NVMe health via smartctl JSON adapter |
| `smf` | system | Service Management Facility Configuration | Drop from default build | Non-Linux or obsolete |
| `software` | system | Software Packages | Replace | M4 Software Center (APT/DNF/zypper/pacman adapters with transaction preview) |
| `spam` | servers | SpamAssassin Mail Filter | Keep in legacy UI | Mail stack is out of homelab core scope |
| `squid` | servers | Squid Proxy Server | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `sshd` | servers | SSH Server | Keep in legacy UI | Bridge selectively later (catalog: System security / Identity) |
| `status` | — | System and Server Status | Bridge (read-only) | M1/M2 read-only views (journal, processes, status) via typed adapters |
| `stunnel` | net | SSL Tunnels | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `syslog-ng` | system | System Logs NG | Bridge (read-only) | M1/M2 read-only views (journal, processes, status) via typed adapters |
| `syslog` | system | System Logs RS | Bridge (read-only) | M1/M2 read-only views (journal, processes, status) via typed adapters |
| `system-status` | system | System Status | Bridge (read-only) | M1/M2 read-only views (journal, processes, status) via typed adapters |
| `systemd` | system | Systemd Services and Units | Replace | Services module (systemd D-Bus adapter); init for non-systemd stays legacy |
| `tcpwrappers` | net | TCP Wrappers | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `time` | hardware | System Time | Keep in legacy UI | Bridge selectively later (catalog: System security / Identity) |
| `tunnel` | — | HTTP Tunnel | Never bridged | Arbitrary command execution / root file access / outbound fetch by design |
| `ui-demo` | — | UI Demo | Drop from default build | Obsolete for homelab targets / demo |
| `updown` | — | Upload and Download | Never bridged | Arbitrary command execution / root file access / outbound fetch by design |
| `useradmin` | system | Users and Groups | Replace | Identity module; useradmin parsers are a parity reference |
| `usermin` | webmin | Usermin Configuration | Legacy admin | Webmin's own user/ACL/config/RPC administration; not mapped to the new RBAC |
| `webalizer` | servers | Webalizer Logfile Analysis | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `webmin` | webmin | Webmin Configuration | Legacy admin | Webmin's own user/ACL/config/RPC administration; not mapped to the new RBAC |
| `webmincron` | webmin | Scheduled Webmin Functions | Bridge (later) | Scheduled jobs view; write paths need broker ops + audit |
| `webminlog` | webmin | Webmin Actions Log | Legacy admin | Webmin's own user/ACL/config/RPC administration; not mapped to the new RBAC |
| `xinetd` | net | Network Services | Keep in legacy UI | No new-UI plan in v1 roadmap |
| `xterm` | — | Terminal | Never bridged | Arbitrary command execution / root file access / outbound fetch by design |
| `zones` | system | Solaris Zones | Drop from default build | Non-Linux or obsolete |
