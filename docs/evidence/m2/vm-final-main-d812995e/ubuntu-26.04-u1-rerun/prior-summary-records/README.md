# Final M2-2 combined-main KVM validation — evidence (2026-10-28, Claude Code on cachyos-um890)

Results are in **`SUMMARY.md`**. This file covers inputs, method, deviations and final state.
The evidence is not committed to Git and was not pushed. Earlier M2-2 directories (`../rerun-20261028/`, `../hc-test-m2-u2604/`, `../p2-h3-investigation-20261028/`, `../arch-validation-20261028/`, `../fedora-p2h3-validation-20261028/`, `../pr30-correction-20261028/`, `../merge-readiness-20261028/`, `../ci-fix-grandchild-20261028/`) were not modified.

## Inputs
- **Source:** main `d812995e3f9e0a5ca3c12f34991c035b01e121e5`. Test harness from a clean detached worktree at that commit (`tests/systemd/*`, hashes in `security/harness-review.txt`). Webmin fixture from `git archive d812995e compat/webmin`.
- **Packages:** CI run 40666815912 artifacts `build` (id 12807130547) and `arch-package` (id 12806179769). The ZIP digests match GitHub; the package SHA-256s are in `artifacts/MANIFEST.txt`.
  CI #87 and the PR #31-only RPM were **not** reused.
- **Browser driver:** `../rerun-20261028/tooling/e2_browser.py`, sha256 `fea16be6…8e16`, unchanged.

## Method (per guest, sequentially: Ubuntu, then Fedora, then Arch)
1. Inventory (`vm-inventory/`). Preserve the current state as internal snapshot `pre-final-main-20261028` (`snapshots/*-preserve.txt`).
2. Revert to the clean baseline: Ubuntu and Fedora `pre-m2`, Arch `pre-hc`. Reboot for a current clock, then preflight: identity, OS, systemd, marker, no package, SSH, clock, update timers (`*/restore.txt`).
3. Stage the package and harness in `/opt/hc`, install test dependencies, run the 14 H3 checks as `hc` (`*/02-stage-and-h3.txt`).
4. `systemd-smoke.sh` with M1 and M2-2 extra checks and `HC_M2_LONG=1`. The exact command and SSH MainPID before and after are in each `01-systemd-smoke-m2.log` header; SSH journal and MAC denials are in `scenario-ssh-and-mac.txt`.
5. Move the scenario's leftover state aside to `/root/scenario-state` in the guest, fresh install (`-dev-insecure-http`, loopback), `e2-operator`, enabled disposable units (fixture F1), then a workstation-local tunnel bound to 127.0.0.1.
6. Browser B1–B15 (`browser-e2/E2-BROWSER-RECORD.md`, screenshots, JSON), post-checks, E4 (`e4-security.txt`, `security/E4-COMPARISON.md`).
7. Disable autostart, graceful `virsh shutdown`, then the final snapshot taken while shut off (passing guests only).

## Deviations and findings
- **U1 (Ubuntu, environment):** unattended-upgrades fired during the scenario and restarted SSH. Testing on Ubuntu stopped after the scenario, per the assignment. Browser and E4 were not run there.
  The state is preserved as snapshot `post-ssh-stop-failure-20261028`. Details and the proposed correction (needs approval) are in `ubuntu-26.04/FINDING-U1-unattended-upgrades.md`.
- **Fedora browser setup attempt 1** installed on top of the scenario's leftover state. It was redone with the state moved aside before any browser step (`fedora-44/browser-e2/setup.attempt1-over-leftover-state.txt`).
- **Fedora AVC:** 4 `useradd_t`/`groupadd_t` pipe-write denials from the RPM scriptlet over SSH. These are pre-existing and not HomeControl (`security/E4-COMPARISON.md`).
- **AppArmor (Ubuntu):** 81 denials from the distro `who` profile. Not HomeControl.
- The post-check `records:` line failed because its redirect lacked root (harmless). The chain verify output already reports 15 records.
- The Ubuntu guest now holds about 50 newly installed updates, including kernel 7.0.0-38, pending reboot; `pre-m2` is unchanged.

## Snapshots
| Guest | Kept (unchanged) | New in this run |
|---|---|---|
| Ubuntu | `pre-m2` | `pre-final-main-20261028` (running, memory and disk: state before this run), `post-ssh-stop-failure-20261028` (running: U1 evidence). **No `m2-final-validated`** (not passed) |
| Fedora | `pre-m2`, `post-p2-failure`, `post-fix-validation` | `pre-final-main-20261028` (running), **`m2-final-validated`** (shut off, disk-only) |
| Arch | `pre-m2`, `pre-hc`, `post-browser-e2` | `pre-final-main-20261028` (running), **`m2-final-validated`** (shut off, disk-only) |

All are internal qcow2 snapshots in the guest disk, on top of the read-only base images in pool `hc-test`. None were deleted, committed or consolidated.
`m2-final-validated` holds HomeControl installed as tested: `-dev-insecure-http` on loopback, the `e2-operator` user, and the `hc-test-dummy`/`hc-test-fail` units.

## Storage (pool `hc-test`, guest qcow2 allocation)
| Guest | Before this run | After | Growth |
|---|---|---|---|
| Ubuntu | 3.38 GiB | 11.64 GiB | +8.26 GiB (mostly the unattended upgrades and kernel, plus two memory snapshots) |
| Fedora | 7.36 GiB | 10.11 GiB | +2.75 GiB |
| Arch | 5.18 GiB | 7.09 GiB | +1.91 GiB |
| Base images (shared, unchanged) | 1.93 GiB | 1.93 GiB | 0 |

Pool allocation went from 933.27 GiB to 947.71 GiB; 881.30 GiB is available.

**Consolidation recommendation (not performed):**
- Once the owner no longer needs the investigation states, delete the running-state snapshots with memory that only document past states (`post-p2-failure`, `post-fix-validation`, `pre-final-main-20261028`, `post-ssh-stop-failure-20261028` after U1 is closed).
- Keep `pre-m2`/`pre-hc` and `m2-final-validated`.
- Internal snapshot deletion frees space only after a `qemu-img convert` rewrite of the guest disk while it is shut off.

## Final state (`final-status/vm-final-state.txt`, queried from libvirt)
| Guest | Power | Autostart | Defined | Snapshots |
|---|---|---|---|---|
| hc-test-m2-u2604-rerun | shut off | disabled | yes | 3 |
| hc-test-m2-f44-rerun | shut off | disabled | yes | 5 |
| hc-test-m2-arch-20261028 | shut off | disabled | yes | 5 |

libvirt reports no running and no autostart-enabled domains on this host. Every shutdown was graceful (about 4 s each); `virsh destroy` was not used.

## Archive
`../final-main-validation-20261028.tar.zst`. Its SHA-256 is in `../final-main-validation-20261028.tar.zst.sha256`, beside the archive, because an archive cannot contain its own checksum. Per-file hashes are in `checksums/SHA256SUMS`.
