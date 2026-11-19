# M2-3 KVM evidence: PR #119 Phase B on Fedora 44

**Status:** recorded evidence only. This is not owner acceptance of M2 or M2-3.

Captured 2026-11-19 from baremetal job `job_30873bf60abe01f7d94eadc89ac38011`
(model `sonnet`, completed, updated 2026-11-19T14:57:27Z). No replacement job
was started. Sanitized files stay on the host at
`/mnt/tank/claude-vms/artifacts/m23-pr119-91d79d30-phaseb-fedora-fe636a10/`.
`sha256sum -c SHA256SUMS` matched those nine files. This note does not copy
guest logs, cookies, or session token ids.

## Guest

- Name: `m23-pr119-f44-a1a2-20261118b-corrected`
- Pool: `claude-vms-zfs` on `qemu:///system`
- `dominfo.txt`: State shut off, Autostart disable, Persistent yes
- Fedora Linux 44 (Cloud Edition), systemd 259 (259.5-1.fc44), kernel `6.19.10-300.fc44.x86_64`
- SELinux was Enforcing before and after. It was not switched off.

The guest was already shut off when this job started. The job started that
guest's existing NAT network, then the guest. It reported a DHCP lease and
SSH, and it did not use `virsh destroy`. This session did not re-check that
network.

## Package and harness

- Release-candidate zip SHA-256
  `030dc62edc50cf73df1278fff7ef1cc6a6bd6a573b188b7e93490f7a38d14d71`
- RPM SHA-256 `3b59b7e041269d34e44ec4642affac4591fd1ead24f2afb24c83ef8548b0a1e5`
- `build.zip` was not used
- `/opt/hc/tests` is `tests/systemd` from `fe636a101ac4c0651bb2a6f147accd132e3b6cf4`
- The previous tree was moved to `/opt/hc/tests.pre-phaseb-1795100166`
- The four script hashes are in the host file `hashes-and-start.txt`

## Result

Exit 0. `phase-b-run.log` has 37 lines that start with `PASS` and no line that
starts with `FAIL`.

| Counter | Result |
|---|---|
| harness `systemd-smoke` | 16 passed, 0 failed |
| harness `m2-3-interrupt` | 20 passed, 0 failed |
| pre-step `dnf -y remove homelab-control` | rc 0 in 3 seconds, then `PASS /usr/bin/hc-api gone` |

The host job summary said 36 because it left the remove check out of the
total. The log includes that line. The Ubuntu note counted the same way:
smoke 16, interrupt 20, and the purge check.

Wrapper window: 2026-11-19T14:56:21Z–14:56:37Z. Before the call: one intent,
one interrupted record, outcome unchanged (still disabled), nothing retried.
After the call: one intent, one interrupted record, outcome changed and not
verified (enabled), nothing retried. The journal hash chain check passed.

## systemctl identity

The before and after identity files match. `phase-b-diff.txt` is empty.

- size 326504, mode 755, owner `0:0`, inode 3976
- SHA-256 `ff08482b390de8adec4bb716290f8069bfa9e353481fd896218b369850de9719`
- SELinux label `system_u:object_r:systemd_systemctl_exec_t:s0`
- `restorecon -n -v` printed nothing (rc 0)
- `rpm -V systemd` printed only `.M.......  c /etc/machine-id` before and after
  (rc 1). That line is a pre-existing config difference. It is not the
  `systemctl` binary.

## AVC split

Taken from the sanitized artifacts. This session did not run `ausearch` on the
guest again.

- Inside the wrapper window: none. The run log's `ausearch` from 14:56:17Z
  has no matches.
- Whole boot: none. `guest-post.txt` records `ausearch-boot` with no matches.
- The job report says 0 inside the window and 0 outside it, so no product AVC
  findings.
- `getenforce` was Enforcing before and after.

## Power

`dominfo.txt`: shut off, autostart disable. The disk was kept. The job says no
snapshots existed. The job also says it left the guest NAT network active.
This session did not confirm that network with `virsh`.
