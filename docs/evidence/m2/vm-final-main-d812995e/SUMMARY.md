# Final combined-main validation — completed after Ubuntu U1 recovery

Source: **d812995e3f9e0a5ca3c12f34991c035b01e121e5**. CI **40666815912**, 21/21 jobs passed. All three guests have now passed the required combined-main validation. No production enablement or M2-3 authorization is implied.

| Guest | M2-2 extended | Smoke/M1 | Browser B1–B15 | E4 API / broker | SSH during tests |
|---|---|---|---|---|---|
| Ubuntu 26.04.1, updated kernel 7.0.0-42/systemd 259.9 | **89/0** | **67/0** | **15/15** | **1.6 / 1.5** | 903→903 scenario; 916→916 resumed browser |
| Fedora 44, SELinux Enforcing | **89/0** | **67/0** | **15/15** | **1.6 / 1.5** | 1035→1035 |
| Arch, kernel 7.2.11/systemd 262.1 | **88/0** | **67/0** | **15/15** | **1.6 / 1.5** | 430→430 |

Each scenario used HC_M2_LONG=1, exited 0 and recorded zero skips. Server-side token expiry after 121 s and step-up expiry after five minutes passed. B16 (optional second browser engine) was not run.
All 14 H3 regression checks passed on every guest. Real backend start/stop/restart, protected units and aliases, dependency chains, replay/cross-target tokens, concurrency and interrupted operations were exercised. The operation journal and broker/API audit chains verified.

## Ubuntu U1 resolution and historical failures
The original run (../ubuntu-26.04/) remains unchanged: background package updates restarted SSH. It is retained as a failed run and is not counted as acceptance.
The authorized recovery (ubuntu-26.04-u1-rerun/) refreshed metadata, completed apt full-upgrade, rebooted, waited for the overdue unattended-upgrade job to complete, checked dpkg/apt integrity and created a clean disk snapshot. Automatic updates remain enabled.
Its automated rerun passed on 2026-10-28 UTC. Its browser run was interrupted after B8; those screenshots are retained but not counted. The completed resumed browser run passed B1–B15 on 2026-10-29 UTC after verifying package identity and resetting only the test account and disposable units. The SSH PID change between 903 and 916 is from the documented guest reboot/manual cold boot between phases; each test phase kept its original PID.

## Exact package SHA-256
| Package | SHA-256 |
|---|---|
| homelab-control_0.1.0+dev.1+52+gd812995e_amd64.deb | 53c2a5dc0c3a2be700d5993643f238b713328b15a779c33afa44dd2dad5b5a54 |
| homelab-control-0.1.0-0.m1.v0_1_0_dev_1_52_gd812995e.x86_64.rpm | 9e65a979e97fd0d6256c83a9eb180d35bb889db317616148eae0ec35afa3b5da |
| homelab-control-0.d812995e-1-x86_64.pkg.tar.zst | 92ae3067c93dd18dcda0253bb5ddb8cf6c3ea3894b5a4e14941d2e05d05f330c |

Artifact ZIP hashes, IDs, binary identity and architecture are in artifacts/MANIFEST.txt and package-inspection.txt. They differ from package hashes.

## Security
E4 exposure, capabilities, ambient caps, NNP and seccomp match the accepted baseline. API remains guest-loopback-only. Fedora retained SELinux Enforcing and the broker's existing init_t domain (broad, not dedicated confinement), with no Option A relocation or Option C policy. No relevant runtime MAC denials were introduced. Pre-existing Fedora user/group-creation pipe denials and Ubuntu who-profile denials are preserved and classified in security/E4-COMPARISON.md.

## Final state and retained snapshots
All three VMs are shut off, persistent, with autostart disabled. No VM, disk, backing image or historical snapshot was deleted.

| Guest | Overlay allocation | Snapshot count | Passing cold checkpoint |
|---|---|---|---|
| hc-test-m2-u2604-rerun | 14.83 GiB | 6 | m2-final-validated-ubuntu-20261028 |
| hc-test-m2-f44-rerun | 10.11 GiB | 5 | m2-final-validated |
| hc-test-m2-arch-20261028 | 7.09 GiB | 5 | m2-final-validated |

Ubuntu also retains the clean disk checkpoint pre-final-main-updated-20261028 and the interrupted browser preservation checkpoint pre-u1-browser-resume-20261028. Final-status/vm-final-state.txt contains independent libvirt results. Snapshot XML for every Ubuntu recovery point is retained in ubuntu-26.04-u1-rerun/.

Remaining separate work: P1 unloaded-unit inventory, H1/H2 VM tooling, optional dedicated SELinux policy, and repository documentation reconciliation. No source, Git history, production configuration or later milestone was changed by this validation.
