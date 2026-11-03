# Final combined-main validation — SUMMARY (2026-10-28)

Main **`d812995e3f9e0a5ca3c12f34991c035b01e121e5`** (PR #31 `3618fea6` + PR #32 `f5fcc5b0` + PR #30 `9172034e` + a DCO-record commit that changes no files).
CI run **40666815912** (push, main): success, 21/21 jobs.

## Verdict per guest
| Guest | Verdict | M2-2 (`HC_M2_LONG=1`) | Smoke + M1 | Browser B1–B15 | E4 | SSH |
|---|---|---|---|---|---|---|
| Ubuntu 26.04.1 `hc-test-m2-u2604-rerun` | **NOT PASSED: run invalidated by environment (finding U1); testing stopped** | 87 passed, **2 failed** (SSH invariants), 0 skipped | 66 passed, **1 failed** (M2-2 exit code), 0 skipped | **not run** (stopped) | **not captured** (stopped) | `ssh.service` restarted by unattended-upgrades (systemd re-exec at 10:24:40; `openssh-server` upgrade at 10:24:54); MainPID 1087 → 14668 → 14958 → socket idle (0); reachable throughout except about 1 s |
| Fedora 44 `hc-test-m2-f44-rerun` (SELinux Enforcing) | **PASS** | **89 passed, 0 failed, 0 skipped** | **67 passed, 0 failed, 0 skipped** (Webmin fixture fix effective; trigger live: 5/5 rc 141 before) | **15/15 Pass** (B16 not run) | **1.6 / 1.5, no delta** | sshd MainPID 1035 before and after the scenario and after the browser run; no sshd events |
| Arch `hc-test-m2-arch-20261028` | **PASS** | **88 passed, 0 failed, 0 skipped** | **67 passed, 0 failed, 0 skipped** | **15/15 Pass** (B16 not run) | **1.6 / 1.5, no delta** | sshd MainPID 430 before and after the scenario and after the browser run; no sshd events |

Notes on the counts:
- Fedora has 89 checks because it adds a third SSH invariant: the `sshd.socket` MainPID, which passes trivially because that socket is inactive. Arch has 88.
- The Ubuntu M2-2 functional checks all passed, but the run is **not counted** as acceptance.

## Checks inside M2-2 (all passed on Fedora and Arch)
- Authorized start, stop and restart.
- Protected-unit refusals: ssh, broker, api, dbus, journald.
- Alias refusal: `hc-test-innocent.service` → SSH unit, 403 naming the protected unit.
- Dependency-chain protection.
- Plan-token replay and cross-action refusal (409 `plan_invalid`).
- **Server-side** token expiry after 121 s (409) and step-up expiry after 5 min (403).
- Concurrent restarts (one 200, one 409).
- Interrupted operation: reported as interrupted, not retried, carries observed state.
- Audit chains: broker and API chains plus journal checkpoints verify; no plan tokens or passwords in the broker audit.
- Service journal reads are audited without copying content.

## H3 (SSH-safe cleanup)
- **14/14** regression checks: on the workstation, and inside each guest as the unprivileged `hc` user.
- The harness review found no path from the harness to host-service stop or restart (`security/harness-review.txt`).
- **H3 did not recur**:
  - Fedora and Arch SSH were unchanged.
  - Ubuntu's SSH change came from the guest's package-update automation, not the harness (`ubuntu-26.04/FINDING-U1-unattended-upgrades.md`).

## Packages (CI 40666815912; package SHA-256, not the ZIP digest)
| Distro | File | SHA-256 |
|---|---|---|
| Ubuntu | `homelab-control_0.1.0+dev.1+52+gd812995e_amd64.deb` | `53c2a5dc0c3a2be700d5993643f238b713328b15a779c33afa44dd2dad5b5a54` |
| Fedora | `homelab-control-0.1.0-0.m1.v0_1_0_dev_1_52_gd812995e.x86_64.rpm` | `9e65a979e97fd0d6256c83a9eb180d35bb889db317616148eae0ec35afa3b5da` |
| Arch | `homelab-control-0.d812995e-1-x86_64.pkg.tar.zst` | `92ae3067c93dd18dcda0253bb5ddb8cf6c3ea3894b5a4e14941d2e05d05f330c` |

All three embed version `v0.1.0-dev.1-52-gd812995e` and the Setsid runner functions. Details are in `artifacts/MANIFEST.txt`.
