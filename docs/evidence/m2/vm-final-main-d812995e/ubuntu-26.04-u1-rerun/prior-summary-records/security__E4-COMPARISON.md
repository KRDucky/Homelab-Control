# E4 security comparison — final main d812995e vs accepted baseline

Sources: `../fedora-44/e4-security.txt` and `../arch/e4-security.txt`.
Baselines: `../../rerun-20261028/` (Ubuntu and Fedora, CI #87), `../../fedora-p2h3-validation-20261028/e4-systemd-analyze.txt` (PR #31) and `../../arch-validation-20261028/e4-systemd-analyze.txt`.
**Ubuntu: E4 not captured in this run**, because testing on that guest stopped under the SSH rule (finding U1).

| Property | Accepted baseline | Fedora 44, final main | Arch, final main | Delta |
|---|---|---|---|---|
| `systemd-analyze security` hc-api / broker | 1.6 / 1.5 | **1.6 / 1.5** | **1.6 / 1.5** | none |
| hc-api identity | user `homelab-control` | `homelab-control` | `homelab-control` | none |
| hc-api CapEff / CapBnd / CapAmb | 0 / 0 / 0 | 0 / 0 / 0 | 0 / 0 / 0 | none |
| broker identity | root | root | root | none |
| broker CapEff / CapBnd / CapAmb | c0 / c0 / 0 (setgid, setuid) | c0 / c0 / 0 | c0 / c0 / 0 | none |
| NoNewPrivileges (both services) | 1 | 1 | 1 | none |
| Seccomp (both services) | mode 2 (filter) | mode 2, 31 filters | mode 2, 31 filters | none |
| Listening | hc-api `127.0.0.1:9180` only; broker on the unix socket `/run/homelab-control/broker.sock` (socket-activated) | same | same | none |
| Network exposure of 9180 | not reachable from the network | not reachable (`browser-e2/tunnel.txt`) | not reachable | none |
| File permissions | operations journal `600 root` | `600 root` (other paths in `e4-security.txt`) | `600 root` | none |
| MAC | Fedora: SELinux Enforcing; Arch: no MAC LSM | **Enforcing**; broker `init_t` (file `lib_t`); hc-api `unconfined_service_t` (file `bin_t`); 0 HomeControl policy modules | no SELinux or AppArmor (`capability,landlock,lockdown,yama,bpf`) | none |
| MAC denials from HomeControl | 0 after P2 fix | **0** (scenario and browser) | n/a | none |

## Fedora SELinux statements (explicit)
- SELinux stayed **Enforcing** throughout. The mode, policy and labels were not changed.
- The broker runs in **`init_t`**, the existing domain, unchanged. `init_t` is a broad domain, **not dedicated SELinux confinement**. Option C (a confining policy) remains an open owner/security decision.
- **No Option A:** the broker stays at `/usr/lib/homelab-control/hc-broker` with label `lib_t`. **No Option C:** `semodule -l` shows 0 HomeControl modules.
- **AVC denials in the scenario window:** 4, all `groupadd_t`/`useradd_t` writing to the SSH session pipe (`sshd_session_t` fifo) during the RPM user-creation scriptlet on first install.
  The same 4 occurred in the original CI #87 run. Not hc-api or hc-broker; no functional effect (`../fedora-44/selinux-avc-scenario.txt`).
- **Browser phase:** 0 AVC denials.
