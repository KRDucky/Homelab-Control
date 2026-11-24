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

## Follow-on checkpoint: broker/API/WebUI simulation (PR #161, 2026-11-24)

A separate draft PR adds the unprivileged closed `apt-get --simulate --no-download` runner, Debian/Ubuntu-only `packages.simulation` broker op, audited scoped `GET /api/v1/packages/preview` endpoint, OpenAPI contract, and a read-only APT Preview tab with package/action selection. Install, remove and single-package upgrade **simulations only** are represented; purge is deliberately excluded until APT `Purg` records are handled and tested. No write execution or source-trust approval is included.

- Graphify pre-change neighbor review covered runner, package ops, API, broker client, Software page/client and impacted test files. Indexed commits inspected: `e83d4d6` (backend) and `66fccef` (frontend). The feature branch and its newly added symbols were not indexed, so **post-change Graphify review is still pending**, not successful.
- Review-driven fixes: added the new operation to the closed registry snapshot tests and broker-client timeout table; kept the existing Software read-only banner test contract. These are branch changes, not independently verified test passes.
- GitHub Actions workflow on PR #161 is not yet concluded at the time of this note. **Do not claim Go, frontend, contract, security, or repository-wide CI passed.**
- Local `apt-get --simulate --no-download --no-install-recommends install -- bash` ran successfully in the tool sandbox with an empty change summary. This is a **read-only command compatibility spot-check only**, not HomeControl integration evidence.
- Debian/Ubuntu VMs and installed `.deb` guest acceptance: **NOT RUN**. No VM-execution interface was available.
- M4 remains **incomplete**. A working *preview* is not a safe *apply*: ADR 0014/0015 owner acceptance, trusted repository identity, transaction locking, revalidation, audit journal, privileged execution and VM proof remain required.
