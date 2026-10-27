# M2-2 security acceptance results (service start/stop/restart)

**Evidence commit:** `970341e6` (branch `feature/m2-systemd-service-operations`, stacked on M2-2a
`457eddf1`). **Date:** 2026-10-26. **PR #28 head:** `987ba011`. **Merge commit:** `a3447efa` (2026-10-26; its
tree is identical to `987ba011`'s). The local and container results below were obtained on `970341e6`.
`987ba011` differs from it by four frontend/test files (`App.tsx`, `shell.test.tsx`, `styles.css`,
`tests/e2e/hcis-ui.mjs`, from merging PR #26's changes) and documentation; those results were not re-run
on `987ba011` or `a3447efa`, which were exercised only by GitHub CI. Criteria: [`M2-SECURITY-ACCEPTANCE.md`](M2-SECURITY-ACCEPTANCE.md)
§2–§3. Evidence: [`docs/evidence/m2/`](../evidence/m2/README.md).

Where each result was obtained is stated per row:
- **local** means this session's Linux workspace: Go tests as the non-root user `hcdev`, Vitest,
  and Playwright against a real `hc-api` with labelled fixtures.
- **container** means a disposable Docker container with systemd as PID 1, on a cgroup-v1 host
  with a cgroup2 hierarchy bound in. Ubuntu 26.04.1 (systemd 259.9) and Fedora 43 (systemd 258.11),
  each with the package installed and the hardened units.
- **GitHub CI** is described by what was actually seen (next section). "Configured" means the workflow
  is set up to run a check; it is not evidence that the check ran or passed.
- **Workstation VM:** none was used. E2 is outstanding (below).

## CI evidence: what was and was not seen
| Level | What was established |
|---|---|
| Overall workflow conclusion | Observed on the public run pages: ci #83 (PR #27 head `457eddf1`), ci #84 (PR #28 head `987ba011`) and ci #85 (`main`, `a3447efa`, run 40292176133) concluded success. |
| Run annotations | Observed: 12 notices, 0 warnings, 0 errors per run. They report govulncheck "no vulnerabilities" (Go 1.26.10, v1.9.0), release dry-run reproducibility, and the theme CSSOM/CSP probe passing 9/9 on WebKit, Firefox and Chromium. None concerns the M2-2 scenario or the service-operations UI test. |
| Configured coverage | From `.github/workflows/ci.yml`: the jobs `systemd-ubuntu` (24.04 runner VM) and `systemd-ubuntu-2604` (container) run `m2-service-ops.sh` without `HC_M2_LONG`; the browser job runs `service-ops-ui.mjs` on Chromium, Firefox and WebKit; the log-argument (`FuzzLogsArgs`) and write-operation argument (`FuzzWriteArgs`) fuzzers are configured to run for 60 s each. |
| Individual job conclusions | **Not verified.** The logged-out run pages list job names and durations but no per-job conclusion, and API access was unavailable. An overall success is not used to infer that a particular job ran or passed its checks. |
| Job logs | **Not inspected** (sign-in required). |

## Merge condition and what was outstanding
[`M2-SECURITY-ACCEPTANCE.md`](M2-SECURITY-ACCEPTANCE.md) is binding: a privileged write operation may be
merged only when every item in §1–§3 passes, with evidence linked in the PR. PR #28 (merge commit `a3447efa`,
2026-10-26) was merged while:
- E2 had no evidence;
- E4's criterion requires VM evidence and only a container check exists;
- E1, E3 and E6 name CI job logs, annotations or jobs as evidence, and individual job conclusions and logs
  were not seen (above);
- no evidence is linked in the PR description, and no review was submitted on the PR page (fetched 2026-10-27),
  so reviewer sign-off in the PR (W1) is not recorded. Other locations (comments, linked discussions) were not
  inspected, and review outside GitHub is not verified. The PR's own commits do include this document and the
  evidence logs (`55422f8c`).

No owner exception to the merge condition is recorded in the repository for any of these.

**Owner decision on E2 (verified).** Source: the owner's message titled "HomeControl — Documentation
Reconciliation Decisions", sent 2026-10-27 at 00:38 America/Chicago (05:38:54 UTC), after the merge, in Claude
Code session https://claude.ai/code/session_01PcnJ5uXahpyCLU3FrjHqki. The decision, in the owner's words: "E2:
Deferred, not waived. Workstation-VM browser validation remains an open acceptance item and is required before
enabling `services.operate` in a real deployment. Do not describe E2 as passed." It names E2 only. It does not
make E2 pass, does not cover E4 or any other criterion, and does not retroactively satisfy the merge condition.
Recommendation (not an owner decision): collect E4's VM evidence in the same VM run as E2.

## §2 Write framework

| # | Result | Evidence |
|---|---|---|
| W1 Closed registry | **Registry tests passed locally; required reviewer sign-off not recorded.** | `TestRegistryIsClosed`: exact op set, privileges, scopes, write phases, risk classes, lock domain, the four templates of `services.unit.apply`. The criterion also requires reviewer sign-off in the PR; none is recorded there (no review was submitted on the PR page; review elsewhere is not verified). Passing tests do not complete this criterion. |
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
| E1 Real systemd | **Pass in containers** (Ubuntu 26.04, Fedora 43). **CI: configured (short scenario); execution not verified** | `tests/systemd/m2-service-ops.sh`: 86/86 checks on Ubuntu 26.04 including the 121 s and 5 min expiry waits, smoke 63/63; Fedora 43: 86/86 and smoke 63/63 (SSH unit `sshd.service`). CI: `systemd-ubuntu` (24.04 runner VM) and `systemd-ubuntu-2604` (container) are configured to run `m2-service-ops.sh` without `HC_M2_LONG`, so the 121 s token and 5 min step-up expiry checks are not part of CI even if those jobs ran them, and CI is not configured for any non-Debian real-systemd job. Those checks, and Fedora, are container-only evidence. The criterion names CI job logs/annotations as evidence; none were inspected. |
| E2 Owner KVM VM through the browser | **Not done: outstanding.** Deferred by the owner (decision of 2026-10-27, after the merge); not waived; not passed | Required before `services.operate` is enabled in a real deployment. The criterion asks for Ubuntu 26.04 **and** one non-Debian family, with evidence under `docs/evidence/m2/` and the commit hash. PR #28 was merged with E2 outstanding. Needs the owner's workstation (`dev/vm/`, `hc-test-` namespace). Containers are not VMs, and the container runs were API-level, not through a browser. |
| E3 Playwright | **Pass locally on Chromium and WebKit** (fixtures); Firefox does not start in this workspace. **CI: configured for Chromium, Firefox and WebKit; execution not verified** | `tests/e2e/service-ops-ui.mjs`: 35 checks per engine. Preview → wrong password → step-up → confirm → observed result, keyboard only, focus on Close, Escape and focus return, all four themes at 390 and 1100 px at 125% text, axe WCAG 2.2 AA with 0 violations. |
| E4 `systemd-analyze security` | **Container check passed; required VM evidence outstanding.** | hc-api 1.6 and broker 1.5, the same as the M1 VM baseline; capability bounding set unchanged. |
| E5 Threat model | **Pass** | `docs/security/THREAT-MODEL.md`, M2-2 section. |
| E6 Supply chain | **Pass** (local). CI: the govulncheck annotation was observed on all three runs; the other gates are configured, individual execution not verified | No new dependency; govulncheck v1.9.0 on Go 1.26.10 "No vulnerabilities found"; `reuse lint`, SBOM/notices drift, shellcheck and secret scan pass in `make check`. |

## Independent review
The implementation had an independent security review (2026-10-26) by a separate AI agent in the authoring session; it was not a human or third-party review. It found three medium issues,
which were fixed:
- policy inputs read by children that share hc-api's uid;
- protected names not matched for non-`.service` units;
- second-order effects not covered.

It also found lower-severity items, which were fixed or documented. A re-review found no new
critical, high or medium defects. Details: ADR 0011, *Security review* and *Known limitations*.

## Not met or not verified
- **E2: outstanding** (deferred by the owner on 2026-10-27, not waived): no VM or browser run on real systemd.
  Required before `services.operate` is enabled in a real deployment.
- **E4: VM evidence outstanding.** Only the container check exists; the criterion requires VM evidence.
- **Binding merge condition:** PR #28 was merged with E2 and E4's VM evidence outstanding, W1's reviewer sign-off
  not recorded, no evidence linked in the PR description (other locations not verified), and no recorded owner
  exception.
- **CI:** overall workflow conclusions and run annotations were seen; individual job conclusions and logs were
  not. Execution of the configured M2-2 checks in CI is therefore not verified.
- **CI scope:** the 121 s token and 5 min step-up expiry checks are not configured in CI; CI has no non-Debian
  real-systemd job.
- **Commits:** local and container results are for `970341e6` only, not `987ba011` or `a3447efa`.
- **Firefox:** not run locally.
- **Distributions:** Debian, openSUSE and Arch real-systemd runs not done. Install-matrix coverage is in
  CI.
- **Accessibility:** no screen reader or Windows High Contrast check of the dialog.
