# M2-3 acceptance results (boot-time enable/disable, unit views, module registry)

**Status:** implementation and evidence ready for owner review on branch
`feature/m2-3-expanded-service-management`. **Not accepted, not merged, not activated in production.** The
owner's authorization of 2026-11-04 covers development and isolated testing only (`OWNER-DECISIONS.md`).

**Revisions.**
- `5a14c727`: first evidence round (superseded).
- `82c24a74`: first review round, findings R1–R11 (superseded).
- **`3bec1294b0523565199fafd12c437bef5fcbbffd`: current product commit** (second review round, R12–R13:
  unloaded unit-file changes). Every commit after it changes only tests, harness or documentation:
  `git diff --stat 3bec1294 HEAD -- backend frontend schemas packaging cmd` is empty.
- Evidence: `../evidence/m2/m2-3/review-3bec1294/` (containers, browser, local gates) and the CI run named in
  `M2-3-KVM-ACCEPTANCE-PROCEDURE.md`. KVM validation is a separate, not yet authorized step.

"Pass" below means the stated evidence exists and passed; it is not the owner's acceptance.

## Scope items (owner instruction)
| Item | Result | Evidence |
|---|---|---|
| Enable/disable at boot, separate `services.configure`, never granted by default | Pass | `TestServicesConfigureNotGrantedByDefault`; API/broker scope tests; container scenario "services.operate alone cannot plan enablement" |
| Boot enablement explained as different from runtime; no implicit `--now` | Pass | Plan consequences; UI text; template snapshot (no `--now`); container: unit not started after enable, still running with the same PID after disable |
| Read-only timer and socket views | Pass | `TestTimers`, `TestSockets` (systemd 259 output); container and browser B12–B13 |
| Unit details: dependencies, unit-file path, last exit, restart counters | Pass | `TestBuildDetailDoesNotInventState`; container; browser B10 |
| Bounded, read-only unit-file contents | Pass | `TestParseCat` (64 KiB / 2,000 lines, control characters); container; browser B10 |
| Unloaded, missing, static, generated, template, alias units without invented state | Pass | Detail notes for each case (unit tests); container: template, alias, not-in-inventory 404 |
| Module registry, capability-aware navigation, disabled routes 404, broker ops refused | Pass | `TestDisabledModuleRoutes404`, `TestDisabledModuleOpsRefused`; container: 404 identical to unknown routes, `broker.info` without services ops, invalid file fails closed; browser B17 |
| D-10 design choice documented for review | Done (open decision) | `M2-3-DESIGN-REVIEW.md` §5; `OWNER-DECISIONS.md` O-1 |
| No executable plugin framework | Pass | Compiled-in registry only; no loading |

## ADR 0011 review items for the new write
| Item | Result | Evidence |
|---|---|---|
| Typed broker plan/apply, fixed argument templates | Pass | `TestRegistryIsClosed`, `TestRootTemplatesAreClosed` (no `--now`, `--runtime`, `mask`, reload…), `TestRootExecConfined` |
| Authorization in API and broker | Pass | `TestEnablementRequiresConfigureScope`, `TestEnablementOpsNeedConfigureScopeEndToEnd` |
| Explicit confirmation, risk class, step-up | Pass | `disruptive`, step-up always (O-3); `TestEnableFlowStepUpAndIndependentReadBack`, `TestEnablementApplyAlwaysNeedsStepUp`; browser B4, B7 |
| Actor/session/target/state-bound expiring single-use tokens | Pass | `TestEnablementTokensNeverCrossOperations` (op, action, actor, session), `TestEnablementTokenExpiry`, replay; container replay and cross-op checks; **`HC_M2_LONG=1` on real systemd**: plan older than 120 s refused, step-up older than 5 min refused, nothing changed |
| Locking, apply-time revalidation | Pass | `TestEnablementLocks`, `TestEnablementConcurrentAppliesExecuteOnce`, `TestEnablementStalePlansConflict`; container: `[Install]` edited / enabled on the host after review → refused, nothing done; 8 parallel applies → exactly one |
| Durable intent/outcome journal; interrupted recovery | Pass (containers) | Journal assertions; `TestEnablementInterruptedIsRecordedNotRetried`, `TestEnablementRecoveryDescribesObservedState`; **real broker SIGKILL** during an enable, before and after `systemctl` ran (`m2_3_interrupt.py`): one intent + one `interrupted` record, observed state "unchanged" / "changed, not verified", nothing retried, hash chain verifies |
| Independent read-back; honest partial/interrupted outcomes | Pass | `is-enabled` + broker `lstat` and target of every link + fresh scan for other links (R3); `TestEnablementHonestOutcomes`, `TestEnablementReadBackScansLinks`; container: links point to the unit file, no unexpected links |
| Protected-unit enforcement for persistent changes (aliases, canonical identities, affected units) | Pass | `TestEnablementRefusals` (41 cases) and `TestEnablementCollisionsAndRequisite`; container: SSH, API and broker units, protected install alias, wanted by SSH, boot conflict with SSH |
| Unloaded unit-file changes (R12, R13): enable fails closed; disable checks `Requisite=` on disk | Pass (containers) | Probe 8 (four versions, isolated cases); `TestEnableRefusedAfterUnloadedChanges`, `TestEnableEditBetweenPreviewAndApply`, `TestEnableGateExcusesOwnJournaledChanges`, `TestEnableGateForeignChangeDuringOwnChange`, `TestEnableGateDoesNotExcuseInterruptedChanges`, `TestDisableRefusesRequisiteOnDisk`; container checks 6d on all four; reproduction with the old build |
| Target resolution from disk (R1, R4) and affected links (R2) | Pass | `TestEnablementRefusesStaleLoadedView`, `TestEnablementDropInAfterPlanIsRefusedAtApply`, `TestEnablementSharedDropIns`, `TestDisableRefusesUndescribedLinks`; container: new drop-in on a running unit refused, undescribed link refused and nothing removed |
| Unsafe/unsupported cases refused | Pass | Same table: `Also=`, `RequiredBy=`, `UpheldBy=`, specifiers, SysV, generated, transient, linked, runtime, symlinked paths, template instances; `TestEnablementCollisionsAndRequisite`: `Alias=` naming another unit (probe 4), foreign files at link paths, disabling a requisite of a protected unit; container: alias collision and protected `Requisite=` refused |
| Secrets in unit contents | Pass (heuristic, **not a secrecy guarantee**; continuation lines fixed, R6; known gaps in design review §4; O-2) | `TestRedactLine`, `TestParseCat`; container: root-only drop-in reported unreadable, fixture secrets absent from API responses, audit, journal and logs |
| No mask/unmask, editing, creation, generic daemon-reload, arbitrary commands, new dependencies | Pass | Template snapshot; `go.mod` and `package-lock.json` unchanged; ADR 0006 policy check |

## Validation items (product commit `3bec1294`, `../evidence/m2/m2-3/review-3bec1294/`)
| Item | Result |
|---|---|
| M2-2 regression | M2-2 real-systemd scenario with `HC_M2_LONG=1` 89/0 on Ubuntu 26.04, 24.04, 22.04 and Fedora 44; Go and Vitest suites pass; CI's browser suites incl. `service-ops-ui.mjs` 298/0 locally (Chromium, Firefox) |
| Long expiry checks | `HC_M2_LONG=1` on all four (M2-2 and M2-3): plan older than 120 s and step-up older than 5 min refused, nothing changed |
| Interruption recovery | Real broker SIGKILL before and after `systemctl` ran: 19/0 on all four (one intent + one `interrupted` record each, observed state reported, nothing retried, hash chain verifies, `systemctl` restored with identical inode, size, mode, owner, group and SHA-256) |
| Unloaded changes, edits between preview and apply | Container checks 6d on all four; unit tests; reproduction with the old build |
| Module-disabled enforcement | API, broker, container and browser |
| Real systemd only in new disposable environments; cleanup cannot affect SSH or access services | Docker containers created and removed per run; workstation unchanged; cleanup touches only fixtures the run created and verifies the link set, SSH/API/broker enablement and the SSH main PID; the protected stand-ins are `homelab-control-hctest-*` fixtures, never real access services |
| Ubuntu and a non-Debian family | Ubuntu 26.04.1, 24.04.5, 22.04.5 (systemd 259, 255, 249) and Fedora 44 (containers; **SELinux disabled**) |
| Real-backend browser workflow, keyboard, responsive HCIS | 18/18 on Ubuntu 26.04 and Fedora 44 (Chromium) |
| `make check`; Go tests as non-root | exit 0, nothing skipped; uid 1000: 285 tests + 116 subtests, 0 failed |
| GitHub CI | See the run named in `M2-3-KVM-ACCEPTANCE-PROCEDURE.md` and the PR |

## Open items and limits (for the owner)
- **Owner approval of proposals P-1…P-6** (`OWNER-DECISIONS.md`; not decisions): root-owned module setting,
  a separate unit-file-content permission (not implemented), password for every boot-time change,
  `--no-reload` semantics, refused constructs, and the fail-closed gate for unloaded changes. D-10 open.
- **No VM evidence** for M2-3 (containers only, SELinux disabled). The KVM procedure, including Fedora with
  SELinux Enforcing and real reboots, is prepared (`M2-3-KVM-ACCEPTANCE-PROCEDURE.md`) and needs owner authorization.
- **CI wiring:** both existing systemd CI jobs now run the M2-3 scenario; a Fedora/Arch systemd CI job (roadmap §3) was **not** added.
- **Not run:** Debian, openSUSE, Arch; the M2-3 browser workflow on Firefox/WebKit or on Ubuntu 24.04/22.04; screen reader;
  CI does not run the expiry waits or the broker kill (opt-in, disposable hosts).
- **Vitest timing:** load-sensitive `findBy…` timeouts observed under CPU saturation (including untouched M2-2/M1
  tests) and once without load; not resolved.
- **Residual risks:** see `M2-3-DESIGN-REVIEW.md` §3.9 and the M2-3 section of `docs/security/THREAT-MODEL.md`.
- Production `services.configure` (like `services.operate`) needs a separate owner deployment decision.
