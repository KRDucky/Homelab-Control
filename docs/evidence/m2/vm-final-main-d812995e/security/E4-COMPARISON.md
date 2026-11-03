# E4 comparison — final main d812995e, Ubuntu U1 rerun completed

| Property | Accepted baseline | Ubuntu updated | Fedora 44 | Arch |
|---|---|---|---|---|
| API / broker exposure | 1.6 / 1.5 | 1.6 / 1.5 | 1.6 / 1.5 | 1.6 / 1.5 |
| API effective/bounding/ambient capabilities | zero | zero | zero | zero |
| Broker effective/bounding capabilities | c0 (setgid/setuid) | c0 | c0 | c0 |
| Broker ambient capabilities | zero | zero | zero | zero |
| API / broker identities | homelab-control / root | same | same | same |
| NoNewPrivileges both | 1 | 1 | 1 | 1 |
| Seccomp both | 2 (filtered) | 2 | 2 | 2 |
| API listener | guest loopback 127.0.0.1:9180 | same | same | same |
| Operation journal | 0600 root | same | same | same |
| systemd | distro-specific | 259.9 | 259.5 | 262.1 |
| MAC | differs by distribution | AppArmor active, both HC processes unconfined | SELinux Enforcing: broker init_t, API unconfined_service_t | no SELinux/AppArmor |

No exposure or capability regression was observed across systemd versions. Fedora init_t is broad and is not dedicated HomeControl confinement. Broker remains /usr/lib/homelab-control/hc-broker with lib_t; no Option A relocation or Option C policy load.
Fedora's four first-install useradd/groupadd pipe-write AVCs are pre-existing and outside HC runtime; browser phase had zero AVCs. Ubuntu's scenario logged four denials in the distro who profile; the resumed boot logged 22 who-profile denials and none from hc-api/hc-broker. No enforcement was relaxed. Arch cannot establish compatibility with Fedora's enforcing SELinux environment.
Sources: ubuntu-26.04-u1-rerun/e4-security.txt, mac-denials-final.txt, audit-final.txt; fedora-44/e4-security.txt and selinux-avc-scenario.txt; arch/e4-security.txt. Earlier comparison preserved under ubuntu-26.04-u1-rerun/prior-summary-records/.
