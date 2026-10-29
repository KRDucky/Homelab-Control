# Final M2-2 combined-main KVM evidence — October 28–29, 2026

**All three distributions pass** after the authorized Ubuntu U1 recovery. Read SUMMARY.md for the complete acceptance matrix and final-status/vm-final-state.txt for final VM power/autostart/storage results.

## Inputs and method
Exact main d812995e3f9e0a5ca3c12f34991c035b01e121e5, CI 40666815912, verified native DEB/RPM/Arch packages. See artifacts/ for package provenance, architecture, embedded version and binary hashes; test-scripts/ contains the approved final-main harness and preserved browser driver.
Real KVM/systemd tests used HC_M2_LONG=1; browser tests used the real guest API/broker through a workstation-only loopback SSH tunnel and checked each mutation independently over SSH. E4 and audit chains were captured. Guest test accounts and loopback HTTP are disposable test configuration.

## Evidence layout
- ubuntu-26.04/: original U1 failure, unchanged.
- ubuntu-26.04-u1-rerun/: package upgrades, clean baseline, automated passing rerun, snapshot XML and security/audit output. SUMMARY.md explains the recovery and resumed-browser-e2/ holds the complete 15-step browser run.
- fedora-44/ and arch/: complete passing final-main runs, unchanged by the Ubuntu recovery.
- security/: harness review and final E4 comparison.
- snapshots/, vm-inventory/, final-status/: recovery records and final defined/off/autostart-disabled states.
- artifacts/: GitHub artifact verification and exact package hashes.
- checksums/: full file manifest and secret scan.

## Preservation
The original archive is retained byte-for-byte as ../final-main-validation-20261028.before-u1-rerun.tar.zst (SHA-256 a9598c199f42f436dd129afde69ea4e64e709809d15801e8a466cd8972396007), with a checksum beside it.
The regenerated archive remains ../final-main-validation-20261028.tar.zst with its own adjacent .sha256. An archive cannot contain its own checksum.
Earlier top-level summaries, final-state text and checksum manifest are preserved under ubuntu-26.04-u1-rerun/prior-summary-records/. The original failed Ubuntu result and the interrupted browser screenshots remain historical evidence; neither was silently promoted to pass. Historical per-guest file hashes were checked before archival.
No application source was edited or committed. No VM or snapshot was deleted. Fedora and Arch were not started or changed during the Ubuntu recovery; only their final state was queried.

## Reuse
Final passing snapshots were taken while the guests were fully shut down: disk snapshots only, no RAM. Existing RAM snapshots resume an old clock and need the documented reboot/clock preflight when reused. Ubuntu's updated baseline keeps automatic security updates enabled; check and finish pending updates before further acceptance runs.
Ubuntu's final checkpoint contains test accounts (random browser password not retained), loopback HTTP configuration and disposable enabled test units. It is intended for isolated testing.
No consolidation was performed. Before considering it later, inventory internal snapshots and their dependencies and obtain owner authorization.
