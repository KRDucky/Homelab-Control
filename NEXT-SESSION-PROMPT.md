# Next session prompt — Homelab Control (finish M1 verification)

You are continuing **Homelab Control**, a Webmin fork. Read `CLAUDE.md`, `docs/STATUS.md`,
`docs/M1-REPORT.md`, `docs/evidence/m1/README.md`, ADR 0008 and `docs/security/THREAT-MODEL.md` first.

## Where we stopped (2026-10-15)
- M0 merged to `main` (PR #1, `e5c52c17`). Local `main` has since moved to `007cf1dc7` ("Network Services
  context") — not created by Claude; check what it changed before merging anything.
- M1 is on branch **`m1-broker-foundation`** (pushed, head `13417c8c`, no PR yet). It contains the
  privileged broker (4 read-only, argument-free ops), Services/Hardware/Legacy pages, capability-aware
  navigation, packaging (deb/rpm/Arch), systemd smoke + install + browser/axe tests, cross-distro CI.
- Evidence was collected on `d1077e4a`. The final code fix (`25a0ac16`: no HDD/SSD label for virtual
  disks; DMI note shown once) passed `make check` only.

## Do this, in order
1. Check out `m1-broker-foundation`; compare against current `main` (`git log main..`, `git diff e5c52c17 main`)
   and rebase/merge if `main` moved. Do not rewrite `compat/webmin` (run `scripts/verify-upstream.sh`).
2. Inspect the first GitHub Actions run on the branch (jobs: check, arch-package, install-matrix
   [Debian stable/oldstable, Ubuntu 22.04/24.04, Fedora, openSUSE Tumbleweed/Leap, Arch], systemd-ubuntu,
   browser-e2e, upstream-perl-tests). Fix failures honestly; record which distros actually passed.
3. Re-run the full evidence suite on the final commit (in a disposable environment only):
   `make check`, `scripts/repro-check.sh`, Go tests as a non-root user,
   `dev/disposable/systemd-container.sh noble|jammy <deb> …` (see `make systemd-smoke`),
   `tests/packaging/install-test.sh`, `tests/e2e/run-m1-e2e.sh`, `tests/integration/run-hc-e2e.sh`.
   Write output outside the repo first, then copy into `docs/evidence/m1/` (don't commit half-written evidence).
4. Update `docs/STATUS.md`, `docs/M1-REPORT.md`, `docs/evidence/m1/README.md` and
   `docs/project/09-PROJECT-MEMORY.md` with verified results only; then open a PR for M1.

## Constraints
- **Do not start M2.** No write operations, no storage changes, no host modifications.
- New broker ops require review and must stay read-only and argument-free; only
  `internal/broker/runner` may execute programs (guard tests enforce this).
- Stdlib-only Go; npm exact pins with `--ignore-scripts`; never weaken the secret scan or CSP.
- Never present unverified results as verified; label container-vs-VM testing accurately.

## Open owner decisions (ask, don't assume)
ADR 0001/0003/0005 (license)/0008 approval; PAM login (deferred); move Webmin pin to 2.680 when released;
whether to run the Vagrant VM on the CachyOS workstation.
