# M2-3 KVM evidence: PR #119 A3–A5 (Phase B not run)

**Status:** partial evidence only. Not M2 acceptance, not M2-3 acceptance, and not production readiness.

Captured 2026-11-19 from disposable guests in pool `claude-vms-zfs`. Host job `job_081e973ecda011c468a4d9482ca0f644` (model sonnet, cwd `/mnt/tank/claude-vms`) completed at 2026-11-19T06:18:30Z. Raw files stay on the host. This summary was taken from `STATUS.txt`, `README.md`, `INPUTS.txt`, and `virsh dominfo` in that directory. No passwords, cookies, or keys are copied here.

## Artifact

`/mnt/tank/claude-vms/artifacts/m23-pr119-91d79d30-a3a5-phaseb-20261119/`

The artifact README names CI run [44340874717](https://github.com/KRDucky/Homelab-Control/actions/runs/44340874717) and commit `91d79d30` (merged PR #119). Package version `0.0.0-dryrun.1f9f0722`.

| Input | SHA-256 |
|---|---|
| `release-candidate.zip` | `030dc62edc50cf73df1278fff7ef1cc6a6bd6a573b188b7e93490f7a38d14d71` |
| release-candidate RPM | `3b59b7e041269d34e44ec4642affac4591fd1ead24f2afb24c83ef8548b0a1e5` |
| release-candidate DEB | `541694d8d2ad1c8c5b11fa56816793acacb109797cab185e4fbec37674c71970` |

Installed `hc-api`, `hc-broker`, and `hlctl` hashes matched each package's own payload on all three guests. `build.zip` (`250ed73eb32f332dc0b3addcdf0762d411b1d53539341998dd1bd8767012e1d3`) was verified and not used for the recorded phases. It has no `tests/` or `compat/`; those came from the source tarball inside the release candidate. `compat/webmin` was absent there and was taken from upstream webmin at the pin in `webmin.lock`.

## Results

| Guest | OS | A3 | A4 | A5 | Phase B |
|---|---|---|---|---|---|
| `m23-pr119-f44-a1a2-20261118b-corrected` | Fedora 44, systemd 259.5, SELinux Enforcing | 7/0, exit 0 | 6/0, exit 0 | 19 pass / 1 fail, exit 1 | not run |
| `m23-pr119-91d79d30-u2604` | Ubuntu 26.04.1, systemd 259.9 | 7/0, exit 0 | 6/0, exit 0 | 19 pass / 1 fail, exit 1 | not run |
| `m23-pr119-91d79d30-u2204` | Ubuntu 22.04.5, systemd 249.11 | 7/0, exit 0 | 6/0, exit 0 | 19 pass / 1 fail, exit 1 | not run |

A1 and A2 were also recorded on the release-candidate package before A3: 74 pass / 0 fail and 16 pass / 0 fail, exit 0, on all three guests. On the Fedora guest that replaced an earlier A1/A2 that had installed the `build.zip` RPM (`54d9be704e1d1b0afee1aa55f482fb840ee8ea6edbb050a0142d21f56afa59ea`), not the release-candidate RPM.

A3: port 9180 was loopback-only, a host request to the guest NAT address on 9180 was refused, and the SSH forward bound `127.0.0.1` only.

A4: enable and disable of `hc-test-boot` through the product plan survived both reboots on every guest. The unloaded-change refusal was checked on the disabled unit. In the procedure's order, after the first reboot the unit is already enabled, so planning enable refuses as already enabled.

A5 failure is step B20 on every guest. The harness expects `[403, 200]` and observed `[403, 403]`. Its plain fetch sends no `X-CSRF-Token`, and this build treats GET contents and GET logs as CSRF-checked reads. With the session CSRF token, `nofiles` got contents 403 `forbidden` and logs 200, and `e2-operator` got 200 on both. The executor recorded B20 as Fail and treated that as a stale harness line. Phase B did not run because a failed phase stops later phases on that guest.

Post-phase checks on all three: exposure scores 1.6 (API) and 1.5 (broker), broker capability set `cap_setgid` `cap_setuid`, `hlctl audit verify` exit 0. Fedora stayed Enforcing with 0 AVC.

## Deviations

- The browser ran inside each guest. Playwright 1.65.2 came from PyPI and axe-core 4.14.0 from npm. Ubuntu used Playwright's Chrome headless shell. Fedora used distro Chromium, which also pulled avahi, rtkit, upower, and logrotate unit links. Firefox was not run.
- Ubuntu 26.04 A5 attempts 1 and 2 were disturbed by `unattended-upgrade` and a per-user rate limit. Attempt 3 is the record. Fedora A5 attempt 1 stopped at B12 because the A4 reboots cleared `hc-test-en-ui.timer`; it was started again before the recorded A5.
- The harness leftover `webmin.service` fixture was removed after A1/A2 on each guest.
- On Ubuntu 26.04, `ssh.service` MainPID changed within the boot because the upgrade restarted services. The product did not cause that.

## State left

`virsh dominfo` for all three guests shows `State: shut off` and `Autostart: disable`. Disks and snapshots were retained. The package is still installed. Accounts `e2-operator` and `nofiles` remain. The password file was removed from the guests. Nothing in this job was pushed or merged.
