# M2-3 acceptance results (boot-time enable/disable, unit views, module registry)

**Status:** implementation and evidence ready for owner review on branch
`feature/m2-3-expanded-service-management`. **Not accepted, not merged, not activated in production.** The
owner's authorization of 2026-11-04 covers development and isolated testing only (`OWNER-DECISIONS.md`).

**Revisions.** The first evidence round tested product commit `5a14c727` (`../evidence/m2/m2-3/README.md`,
"Final round"). The owner's review assignment of 2026-11-04 found defects R1–R9 (`M2-3-DESIGN-REVIEW.md` §7);
they are fixed in product commit **`82c24a74`**, which **supersedes** the `5a14c727` results. The current
evidence is `../evidence/m2/m2-3/review-82c24a74/`. Later commits on the PR head change only documentation and
the disposable-container harness unless stated in the PR. KVM validation is a separate, unauthorized step:
`M2-3-KVM-ACCEPTANCE-PROCEDURE.md`.

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
| Target resolution from disk (R1, R4) and affected links (R2) | Pass | `TestEnablementRefusesStaleLoadedView`, `TestEnablementDropInAfterPlanIsRefusedAtApply`, `TestEnablementSharedDropIns`, `TestDisableRefusesUndescribedLinks`; container: new drop-in on a running unit refused, undescribed link refused and nothing removed |
| Unsafe/unsupported cases refused | Pass | Same table: `Also=`, `RequiredBy=`, `UpheldBy=`, specifiers, SysV, generated, transient, linked, runtime, symlinked paths, template instances; `TestEnablementCollisionsAndRequisite`: `Alias=` naming another unit (probe 4), foreign files at link paths, disabling a requisite of a protected unit; container: alias collision and protected `Requisite=` refused |
| Secrets in unit contents | Pass (heuristic, **not a secrecy guarantee**; continuation lines fixed, R6; known gaps in design review §4; O-2) | `TestRedactLine`, `TestParseCat`; container: root-only drop-in reported unreadable, fixture secrets absent from API responses, audit, journal and logs |
| No mask/unmask, editing, creation, generic daemon-reload, arbitrary commands, new dependencies | Pass | Template snapshot; `go.mod` and `package-lock.json` unchanged; ADR 0006 policy check |

## Validation items (product commit `82c24a74`, `../evidence/m2/m2-3/review-82c24a74/`)
| Item | Result |
|---|---|
| M2-2 regression | M2-2 real-systemd scenario with `HC_M2_LONG=1` 89/0 on Ubuntu 26.04, 24.04, 22.04 and Fedora 44; Go and Vitest suites pass; CI's browser suites incl. `service-ops-ui.mjs` 298/0 locally (Chromium, Firefox) |
| Authorization, CSRF, input/fuzz, token expiry/replay, stale plan, concurrency, journal recovery, protected units | Unit/API tests; fuzz 60 s per decoder (incl. new `FuzzParseCat`); real-systemd expiry waits; real broker kill (19/0 per distribution) |
| Module-disabled enforcement | API, broker, container and browser |
| Real systemd only in new disposable environments; cleanup cannot affect SSH or access services | Docker containers created and removed per run; workstation unchanged; cleanup touches only fixtures and directories the run created and verifies the symlink set, SSH/API/broker enablement and the SSH main PID: all pass; the broker-kill wrapper is restored and checked |
| Ubuntu and a non-Debian family | Ubuntu 26.04.1, 24.04.5, 22.04.5 (systemd 259, 255, 249) and Fedora 44 (containers; **SELinux disabled**) |
| Real-backend browser workflow, keyboard, responsive HCIS | 18/18 on Ubuntu 26.04 and Fedora 44 (Chromium), incl. the "may apply before a reboot" text |
| E4 hardening comparison | API 1.6 / broker 1.5 on all four (container values) |
| `make check`; Go tests as non-root | exit 0, nothing skipped; uid 1000: 275 tests + 97 subtests, 0 failed |
| GitHub CI | Run 41793837318 on `9862a8dd` (= `5a14c727` code): 21/21 success. CI for the new head: see the PR |

## Open items and limits (for the owner)
- **Owner decisions O-1…O-5** (`OWNER-DECISIONS.md`): D-10 registry approach and file-based setting, scope of unit
  contents, `disruptive` classification, `--no-reload`, refused constructs.
- **No VM evidence** for M2-3 (containers only, SELinux disabled). The KVM procedure, including Fedora with
  SELinux Enforcing and real reboots, is prepared (`M2-3-KVM-ACCEPTANCE-PROCEDURE.md`) and needs owner authorization.
- **CI wiring:** both existing systemd CI jobs now run the M2-3 scenario; a Fedora/Arch systemd CI job (roadmap §3) was **not** added.
- **Not run:** Debian, openSUSE, Arch; the M2-3 browser workflow on Firefox/WebKit or on Ubuntu 24.04/22.04; screen reader;
  CI does not run the expiry waits or the broker kill (opt-in, disposable hosts).
- **Vitest timing:** load-sensitive `findBy…` timeouts observed under CPU saturation (including untouched M2-2/M1
  tests) and once without load; not resolved.
- **Residual risks:** see `M2-3-DESIGN-REVIEW.md` §3.9 and the M2-3 section of `docs/security/THREAT-MODEL.md`.
- Production `services.configure` (like `services.operate`) needs a separate owner deployment decision.
