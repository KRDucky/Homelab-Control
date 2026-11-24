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

A separate draft PR adds the unprivileged closed `apt-get --simulate` runner, Debian/Ubuntu-only `packages.simulation` broker op, audited scoped `GET /api/v1/packages/preview` endpoint, OpenAPI contract, and a read-only APT Preview tab with package/action selection. Install, remove and single-package upgrade **simulations only** are represented; purge is deliberately excluded until APT `Purg` records are handled and tested. No write execution or source-trust approval is included.

- Graphify pre-change neighbor review covered runner, package ops, API, broker client, Software page/client and impacted test files. Indexed commits inspected: `e83d4d6` (backend) and `66fccef` (frontend). The feature branch and its newly added symbols were not indexed at that time; the post-change review is recorded below.
- Review-driven fixes: added the new operation to the closed registry snapshot tests and broker-client timeout table; kept the existing Software read-only banner test contract. These are branch changes, not independently verified test passes.
- GitHub Actions workflow on PR #161 is not yet concluded at the time of this note. **Do not claim Go, frontend, contract, security, or repository-wide CI passed.**
- Local `apt-get --simulate --no-download --no-install-recommends install -- bash` ran successfully in the tool sandbox with an empty change summary. This is a **read-only command compatibility spot-check only**, not HomeControl integration evidence. It hid the `--no-download` finding below because `bash` was already installed.
- Debian/Ubuntu VMs and installed `.deb` guest acceptance: **NOT RUN**. No VM-execution interface was available.
- M4 remains **incomplete**. A working *preview* is not a safe *apply*: ADR 0014/0015 owner acceptance, trusted repository identity, transaction locking, revalidation, audit journal, privileged execution and VM proof remain required.

### Additional source-review finding and fix

The first generated patch for `backend/internal/broker/runner/runner.go` was corrupted by replacement-string interpolation around a Go regexp containing a trailing dollar sign/backtick. It duplicated repository source into the middle of the new regexp. This was caught by inspecting the actual branch file, **before accepting CI or submitting the PR for merge**. The runner file was rebuilt from the clean current `main` with only the intended allow-list and closed simulation changes (fix commit `1b6c7307d364848f0b1e0fbf6c1342c967dc6610`); inspection verified one SPDX header, one simulation method and expected file size. This is source inspection evidence, **not** a claim of compiled/tested correctness. Full CI and fresh Graphify branch review remain mandatory.

### Post-change Graphify review (2026-11-24)

| Pass | Indexed SHA | Scope | Nodes / edges | Result |
|---|---|---|---|---|
| 1 | `0f74af27` (PR head, clean worktree) | `backend/`, `frontend/app/src/`, AST only, 451 code files | 5226 / 20397 | 4 confirmed findings, 1 nit |
| 2 | `0f74af27` + the fixes below | same | 5229 / 20407 | no new finding |

Graph health on both passes: 1905 dangling-endpoint edges (stdlib and external symbols), 272 collapsed duplicate
edges, 0 missing endpoints. The extractor misses some method calls, so `RunAptSimulation` shows no test caller;
`TestRunAptSimulationExecutesFixedArgv` calls it (checked in source).

Findings, all fixed with regression tests that fail on `0f74af27`:

1. **`--no-download` broke install previews.** With it, `apt-get --simulate install` exits 100 with
   "Unable to fetch some archives" whenever the `.deb` is not already in the local archive cache, which is the
   normal case. A simulation never fetches, so the flag is removed. Reproduced on the host with a non-root
   `apt-get` 3.3.3 (`sl`: exit 100 with the flag, a clean `0 upgraded, 1 newly installed` plan without it).
2. **"Upgrade" previewed a fresh install.** `upgrade` mapped to plain `install`, so upgrading a package that
   is not installed showed it being installed. It now uses `--only-upgrade install`; APT then prints "Skipping
   …, it is not installed" and an empty plan (checked on the host). Tests: `TestAptSimulationArgvClosed`
   (exact argv per action), `TestRunAptSimulationExecutesFixedArgv` (a stand-in `apt-get` receives exactly
   that argv; a refused action never reaches exec).
3. **No test of the run-result gate.** The exit-code, stderr and parser refusals in `packagesSimulation`
   were untested. They are now `aptSimulationPreview`, tested with real captured non-root `apt-get` stdout
   (labelled fixture) and with exit 100, stderr diagnostics and an unparsable plan.
4. **Stale preview under a changed selection.** While a request was in flight only the button was disabled,
   so a late answer could appear under a different package or action. The inputs are now disabled while
   busy (test: "locks the selection while a simulation is in flight").
5. Nit: a test doc comment sat above the wrong test.

`apt` was considered instead of `apt-get` and kept out: apt(8) says its command line "may change behavior between
versions" and directs scripts to `apt-get`, and apt 3.3.3 prints `Summary: Upgrading: …` instead of the
`N upgraded, N newly installed, …` line the fail-closed parser requires.

Checks after the fixes: gofmt, `go vet`, `go test ./...` (non-root) pass; SDK, extensions and HCIS checks
pass; frontend typecheck, ESLint, Prettier clean, Vitest 31 files / 298 tests pass. Not run locally: Rust
lint/tests (no cargo on the host), `make security`, `make deb`. Debian/Ubuntu VM acceptance: **still NOT RUN**.

## APT settings and mirror writes (branch `feature/m4-apt-config-mirrors`, 2026-11-24)

Owner-authorized (2026-11-24). Software Center tabs **APT settings** and **Mirrors**; broker ops
`packages.aptconfig.*`, `packages.mirrors.inventory`, `packages.mirror.*`; opt-in root helper `hc-apt-apply`;
mirror probe in hc-api (`internal/aptmirror`); scope `packages.configure`. Design and limits: ADR 0008
Amendment 10. Unit, API, guard and UI tests pass. Installed-package VM validation on `856d0031`: Ubuntu 26.04,
Debian 13 and Ubuntu 22.04 31/31 each; Ubuntu 24.04 and Debian 12 not run. Evidence:
`docs/evidence/m4/APT-CONFIG-MIRRORS.md`. Partial checkpoint, not accepted.
