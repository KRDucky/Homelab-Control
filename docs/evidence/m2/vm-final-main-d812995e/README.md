# M2-2 E2/E4 closeout — owner approved 2026-10-29

Owner source: the project conversation, “I approve closing E2/E4 and preparing the documentation PR”. This records closure after validation, not a retroactive exception to PR #28's pre-merge requirements. W1 owner review/sign-off was accepted on October 27, as recorded in the owner's earlier validation instructions; no GitHub review is inferred.

Validated source: `d812995e3f9e0a5ca3c12f34991c035b01e121e5` (merged corrections #31, #32, #30). [CI 40666815912](https://github.com/KRDucky/Homelab-Control/actions/runs/40666815912): 21/21 jobs succeeded; individual conclusions are preserved in [the CI inventory](artifacts/ci-40666815912-jobs.txt). These are later results, distinct from the original October 26 evidence.

| KVM guest | Extended M2-2 passed / failed | Smoke + M1 | Browser B1–B15 | E4 API / broker |
|---|---|---|---|---|
| Ubuntu 26.04.1 | 89 / 0 | 67 / 0 | 15 / 15 | 1.6 / 1.5 |
| Fedora 44 | 89 / 0 | 67 / 0 | 15 / 15 | 1.6 / 1.5 |
| Upstream Arch Linux | 88 / 0 | 67 / 0 | 15 / 15 | 1.6 / 1.5 |

Each extended scenario used `HC_M2_LONG=1`, exited zero with no skips, and covered server-side token and step-up expiry. B9 alone tests browser expiry; B16 was not run. Each guest passed 14 H3 cleanup regressions. Browser operations used the real API/broker through a workstation-loopback SSH tunnel, with independent systemctl observations.

## Evidence navigation

- Ubuntu: [passing scenario](ubuntu-26.04-u1-rerun/01-systemd-smoke-m2.log), [completed browser record](ubuntu-26.04-u1-rerun/resumed-browser-e2/E2-BROWSER-RECORD.md), [E4](ubuntu-26.04-u1-rerun/e4-security.txt). The separate `ubuntu-26.04/` directory is the original U1 **failure**, not acceptance evidence.
- Fedora: [scenario](fedora-44/01-systemd-smoke-m2.log), [browser record](fedora-44/browser-e2/E2-BROWSER-RECORD.md), [E4](fedora-44/e4-security.txt).
- Arch: [scenario](arch/01-systemd-smoke-m2.log), [browser record](arch/browser-e2/E2-BROWSER-RECORD.md), [E4](arch/e4-security.txt).
- [Artifact manifest](artifacts/MANIFEST.txt), [security comparison](security/E4-COMPARISON.md), [final VM states](final-status/vm-final-state.txt), [independent AI review](REVIEW-20261029-claude.md), [prior summaries](ubuntu-26.04-u1-rerun/prior-summary-records/), [original source index](SOURCE-README.md), [original source summary](SUMMARY.md).

## Method, limitations and preservation

The Ubuntu recovery was explicitly authorized in the owner's “Final Ubuntu 26.04.1 Acceptance Rerun (U1)” instructions. It updated the baseline and completed pending upgrades, with automatic updates left enabled. The interrupted browser attempt remains historical evidence. The completed attempt restarted at B1 with a fresh browser profile, reset account scopes and disposable units, and reused the existing HomeControl installation and audit history. It was not a fresh package installation.

The next Ubuntu cold boot is recorded at 2026-10-29 04:57:20Z; the preceding shutdown time and method are **not recorded**. The original summary's “documented ... reboot/manual cold boot” wording overstates that evidence. SSH MainPID remained 903 during the scenario and 916 during the completed browser phase. Recorded package versions and binary identities agree across phases; no complete dpkg log was captured across the gap, so this does not prove absence of every package change. The independent review records both caveats.

Audit chains verify internal consistency; without an external checkpoint they do not prove absence of tail truncation or rollback. Fedora remained SELinux Enforcing: the broker stayed at `/usr/lib/homelab-control/` in broad `init_t`, **not dedicated confinement**. No Option A relocation or Option C policy was applied. Relevant runtime AVCs were absent; existing user/group-creation pipe denials and Ubuntu `who` AppArmor denials are retained and classified. Arch does not provide equivalent MAC enforcement.

All three guests were recorded shut off with autostart disabled; disks and historical snapshots remain. This documentation task performs no guest operations. Production `services.operate` remains ungranted by default and requires a separate deployment decision. M2-3 remains unauthorized. P1 inventory, H1/H2 VM tooling, optional dedicated SELinux confinement, Debian/openSUSE real-systemd validation, second browser engine and assistive-technology checks remain separate work.

Files were copied byte-for-byte from the local evidence directory, retaining its layout. Execution tooling was omitted: the final harness is available at the validated Git revision. Source records may mention those local tooling paths or external archives; these are historical references, not files promised by this copy. `checksums/SOURCE-SHA256SUMS` is the original layout manifest and includes omitted tooling; use `checksums/SHA256SUMS` to verify this published copy. No packages, private keys, passwords or session stores are included.

Local archive provenance (not committed): `final-main-validation-20261028.tar.zst`, SHA-256 `b8b9ed6b0c2ef05995acc5ce040e1c7336e09ea670ab069dad556a40a07a486a`; original pre-U1 archive `a9598c199f42f436dd129afde69ea4e64e709809d15801e8a466cd8972396007`. The archives predate the independent review and proposal; this copy includes the review. The original raw evidence and both archives remain unchanged.
