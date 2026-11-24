# M4 APT transaction preview: implementation checkpoint

**State:** partial implementation only. Not a functioning Software Center transaction path, not M4 acceptance, and not authorization for package writes.

This slice adds `backend/internal/aptplan`, a bounded pure-Go parser for the `Inst`, `Remv` and `Conf` records produced by `apt-get -s`. It checks APT's summary counts against proposed install/removal actions, refuses duplicate/unknown action types, missing summaries, diagnostics, malformed names, malformed candidates and oversized output. It does not invoke programs, grant permissions, execute packages, or present the output as a trusted plan. Future broker integration must preserve the source and authorization decisions in ADR 0014/0015.

## Review and test evidence (2026-11-24)

- Graphify reviewed the existing package dependency surfaces: `backend/internal/pkgcenter/debian.go`, `backend/internal/pkgcenter/pkgcenter.go`, `backend/internal/api/packages.go`, `backend/internal/broker/ops/packages.go`, and `backend/internal/broker/runner/runner.go`. Existing code and linked tests were inspected. Graphify index SHA for this check: `55a6a7e` (pkgcenter), plus `c1e73d6` for the latest branch-file lookup.
- Graphify could **not** inspect the newly added `backend/internal/aptplan/plan.go` on the feature branch: the indexed default branch did not contain that file. **Branch-specific Graphify review is pending** after indexing; report this as a gap, not a pass.
- Local isolated parser package tests: `go test -race -count=1 ./...` **PASS**; `go vet ./...` **PASS**, executed with local Go 1.23.2 using a standalone test module with identical module-relative package source. This is **not** the repository's full `make check` (repository requires Go 1.26), and not a Debian/Ubuntu VM acceptance.
- Review finding: unhandled integer overflow in parsed APT summary counts. Corrected by explicitly checking each conversion and cap; new regression tests for giant numeric counts and mismatched summaries pass.
- A read-only host-sandbox `apt-get -s install bash` returned a zero-change transaction summary. This is a format spot-check only, not a VM test or evidence of package management functionality.
- **VM testing: NOT RUN**. No KVM/virsh or guest execution connector was available to this session; no real package install/remove/upgrade has been performed.

## Still required before M4 acceptance

The current new package is not wired to the API, broker or UI. Implement fixed reviewed APT simulation templates, typed broker and API preview operations, trusted source attribution, per-package selection UX, authorization framework, broker-only write templates, job progress, journal, state revalidation and VM-backed install/remove/update/purge tests. ADR 0014 and ADR 0015 remain proposed and must be explicitly accepted before enabling privileged transactions. Build and install the actual HomeControl `.deb` in disposable Debian/Ubuntu VMs and capture per-guest evidence. Re-run Graphify on this new code after index refresh, fix its findings and rerun tests before closing the checkpoint.
