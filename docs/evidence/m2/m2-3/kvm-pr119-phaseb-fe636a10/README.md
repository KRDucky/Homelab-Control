# M2-3 KVM evidence: PR #119 Phase B (partial)

**Status:** partial evidence only. This is not owner acceptance of M2 or M2-3.

Captured 2026-11-19 from baremetal job `job_42be66063b4adee02530fda179b2ab29`
(model `sonnet`, status completed at 2026-11-19T08:50:08Z). No second job was
started. Raw files stay on the host at
`/mnt/tank/claude-vms/artifacts/m23-pr119-91d79d30-phaseb-fe636a10/`.
This note does not copy guest logs, cookies, or session token ids.

## What ran

- Product package: release-candidate zip SHA-256
  `030dc62edc50cf73df1278fff7ef1cc6a6bd6a573b188b7e93490f7a38d14d71`
  (recorded product commit prefix `91d79d30`). `build.zip` was not used.
- RPM SHA-256 `3b59b7e041269d34e44ec4642affac4591fd1ead24f2afb24c83ef8548b0a1e5`
- DEB SHA-256 `541694d8d2ad1c8c5b11fa56816793acacb109797cab185e4fbec37674c71970`
- Harness deviation: `/opt/hc/tests` is `tests/systemd` from commit
  `fe636a101ac4c0651bb2a6f147accd132e3b6cf4` (harness repair, not merged here).
  The previous guest tree was moved aside as `tests.pre-phaseb-*`.
- Webmin was already on both Ubuntu guests (44162 files). It was not restaged.
- Phase B is the broker-kill and `systemctl` restoration check. Each Ubuntu
  log has 37 `PASS` lines and no `FAIL`: smoke 16, interrupt checks 20, and
  the purge check. `exit 0`.

| Guest | Phase B | systemctl identity | State / autostart |
|---|---|---|---|
| Ubuntu 26.04.1 `m23-pr119-91d79d30-u2604` | 37 / 0, exit 0 | match: inode 1892, 302112 bytes, mode 755, root:root, SHA-256 `639bce3ce5c06b605079ea56f74e3f08216ddffcfa76e11f62073aa775180082` | shut off / disable |
| Ubuntu 22.04.5 `m23-pr119-91d79d30-u2204` | 37 / 0, exit 0 | match: inode 1693, 1119856 bytes, mode 755, root:root, SHA-256 `de4c5cdd03758593036da105a57f959b76bd1ad6e0d1e567d29d16458aeaddf6` | shut off / disable |
| Fedora 44 `m23-pr119-f44-a1a2-20261118b-corrected` | not run | n/a | running / disable |

Ubuntu wrapper windows: u2604 08:24:22Z–08:25:22Z, u2204 08:46:12Z–08:47:00Z.
Neither Ubuntu guest uses SELinux, so there are no AVC denials to classify.
`dpkg --verify systemd` was clean before and after on both. The interrupt
checks covered the before-call and after-call broker kill, journal records,
no retry, hash chain, and restored `systemctl`.

## Fedora 44

The guest booted and then stayed stuck for about an hour: no transmitted
packets, no disk writes, no DHCP lease, and no response to two ACPI shutdowns.
No console or screenshot was captured. It was not force-stopped, because it is
a retained guest. Autostart stays disabled. Disk and SELinux mode were not
changed. Phase B on this guest needs an owner decision (hard reset or destroy)
before another run.

## Observations

- The first `apt-get purge homelab-control` printed `Error: Timeout was reached`
  on both Ubuntu guests (about 9.5 minutes on u2604, about 4 minutes on u2204).
  `/usr/bin/hc-api` was gone afterwards. The purge checks still passed.
- u2604 took a new DHCP address after boot. SSH used the newest lease.
- After purge, the guest-post note records `degraded` on u2604 and `running`
  on u2204. The smoke log says package removal kept state and audit files under
  `/var/lib/homelab-control` and `/var/log/homelab-control*`.
