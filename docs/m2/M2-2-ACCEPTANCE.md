# M2-2 security acceptance results (service start/stop/restart)

**Product code:** `970341e6` (branch `feature/m2-systemd-service-operations`, stacked on M2-2a
`457eddf1`). **Date:** 2026-10-26. Criteria: [`M2-SECURITY-ACCEPTANCE.md`](M2-SECURITY-ACCEPTANCE.md)
§2–§3. Evidence: [`docs/evidence/m2/`](../evidence/m2/README.md).

Where each result was obtained is stated per row:
- **local** means this session's Linux workspace: Go tests as the non-root user `hcdev`, Vitest,
  and Playwright against a real `hc-api` with labelled fixtures.
- **container** means a disposable Docker container with systemd as PID 1, on a cgroup-v1 host
  with a cgroup2 hierarchy bound in. Ubuntu 26.04.1 (systemd 259.9) and Fedora 43 (systemd 258.11),
  each with the package installed and the hardened units.
- **CI** results are recorded in the PRs, not here.
- **No VM was used.**

## §2 Write framework

| # | Result | Evidence |
|---|---|---|
| W1 Closed registry | **Pass** (local) | `TestRegistryIsClosed`: exact op set, privileges, scopes, write phases, risk classes, lock domain, the four templates of `services.unit.apply`. Reviewer sign-off is the owner's. |
| W2 Plan tokens | **Pass** (local, container) | `writes`: replay, expiry, every binding field, tampered/foreign/previous-boot tokens, concurrent single use, per-actor bound. Container: replay, other unit, other action → 409 `plan_invalid`; token after 121 s refused; refusals in the API audit (`invalid_token`) and the broker audit. |
| W3 Revalidation | **Pass** (local, container) | `TestChangedStateIsConflict`, `TestAffectedSetChangeInvalidatesPlan`; container: out-of-band restart between plan and apply → 409 `conflict`, unit untouched. Apply re-reads with the read-only root template. |
| W4 Locks | **Pass** (local, container) | `TestLocks` (incl. panic), `TestLockedTargetIsBusy`, `TestConcurrentAppliesExecuteOnce`, `TestPanicAfterIntentRecordsOutcome`; container: two concurrent restarts → one 200, one `busy`/`conflict`. |
| W5 Journal | **Pass** (local, container) | Intent fsync'ed before exec, outcome after, fail closed without a journal; container: broker SIGKILLed during a slow start → reported as `interrupted` with the observed state, not retried, chain verifies with `hlctl audit verify`, file 0600 root. |
| W6 Step-up | **Pass** (local, container) | API tests (missing, wrong, stale, other session, revoked, logout, throttle shared with login, password never logged), `auth` window tests; broker refuses disruptive apply without `step_up_at`; container: stop without step-up → 403, wrong password → 403, step-up after 5 min → 403, unit untouched. |
| W7 CSRF/Origin | **Pass** (local) | `TestServiceOpsCSRFAndOrigin`, `TestStepUpRequiresOriginCSRFAndJSON`; live checks in `run-hc-e2e.sh` (scope, CSRF, foreign Origin). |
| W8 Authorization | **Pass** (local, container) | API and broker refuse without `services.operate` (`TestServiceOpsRequireOperateScope`, `TestWriteOpsRefusedWithoutScopeEndToEnd`); scope removal applies to live sessions; Vitest: no controls without the scope; container: default account refused and shown no controls; old session does not gain a newly granted scope. |
| W9 root-exec runner | **Pass** (local, container) | `TestRootTemplatesAreClosed`, `TestRootTargetValidation`, `TestRootRunUsesFixedArgvAndScrubbedEnv`, `TestRootRunTimeoutKillsGroup`, `TestRootExecHandleOnlyForRootExecOps`, guard `TestRootExecConfined`; `FuzzWriteArgs` 60 s; container: broker bounding set still `CAP_SETGID CAP_SETUID`. |
| W10 Protected units | **Pass** (local, container) | Table tests (names, every unit type, aliases, unit files, template bases, case, transitive dependencies, conflicts, triggers, isolate, `RefuseManualStop`); container: the SSH unit (`ssh.service` on Ubuntu, `sshd.service` on Fedora), both Homelab Control units, D-Bus and journald refused; alias symlinked to the SSH unit refused by the broker; a two-hop chain to a protected-name unit and a start reaching `reboot.target` two hops away refused; SSH and broker untouched. |
| W11 Outcome verification | **Pass** (local, container) | Outcome classified from the state read back; container: a disposable unit that fails after systemd accepts the start → `failed`, observed `failed`, recovery guidance; restart PID matches systemd. |
| W12 No rollback claims | **Pass** (local) | Consequences and recovery text state "no automatic rollback"; Vitest checks unknown outcomes never say "nothing was changed"; user guide. |

## §3 Environment evidence

| # | Result | Evidence |
|---|---|---|
| E1 Real systemd | **Pass in containers** (Ubuntu 26.04, Fedora 43); **CI pending** | `tests/systemd/m2-service-ops.sh`: 86/86 checks on Ubuntu 26.04 including the 121 s and 5 min expiry waits, smoke 63/63; Fedora 43: 86/86 and smoke 63/63 (SSH unit `sshd.service`). Wired into the CI jobs `systemd-ubuntu` (24.04 runner VM) and `systemd-ubuntu-2604`. |
| E2 Owner KVM VM through the browser | **Not done** | Needs the owner's workstation (`dev/vm/`, `hc-test-` namespace). Containers are not VMs, and the container runs were API-level, not through a browser. |
| E3 Playwright | **Pass locally on Chromium and WebKit** (fixtures); Firefox does not start in this workspace; CI runs all three | `tests/e2e/service-ops-ui.mjs`: 35 checks per engine. Preview → wrong password → step-up → confirm → observed result, keyboard only, focus on Close, Escape and focus return, all four themes at 390 and 1100 px at 125% text, axe WCAG 2.2 AA with 0 violations. |
| E4 `systemd-analyze security` | **Pass** (container) | hc-api 1.6 and broker 1.5, the same as the M1 VM baseline; capability bounding set unchanged. |
| E5 Threat model | **Pass** | `docs/security/THREAT-MODEL.md`, M2-2 section. |
| E6 Supply chain | **Pass** (local) | No new dependency; govulncheck v1.9.0 on Go 1.26.10 "No vulnerabilities found"; `reuse lint`, SBOM/notices drift, shellcheck and secret scan pass in `make check`. |

## Independent review
The implementation had an independent security review (2026-10-26). It found three medium issues,
which were fixed:
- policy inputs read by children that share hc-api's uid;
- protected names not matched for non-`.service` units;
- second-order effects not covered.

It also found lower-severity items, which were fixed or documented. A re-review found no new
critical, high or medium defects. Details: ADR 0011, *Security review* and *Known limitations*.

## Not met or not verified
- **E2:** no VM or browser run on real systemd.
- **CI:** not yet run on this head.
- **Firefox:** not run locally.
- **Distributions:** Debian, openSUSE and Arch real-systemd runs not done. Install-matrix coverage
  is in CI.
- **Accessibility:** no screen reader or Windows High Contrast check of the dialog.
