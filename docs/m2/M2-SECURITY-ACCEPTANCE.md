# M2 security acceptance checklist

**Date:** 2026-10-21 · **Status:** proposed. It becomes binding once the owner approves D-2…D-5 (`OWNER-DECISIONS.md`).

Every item is a test or a reviewable artefact. **A privileged write operation may be merged only when every item in §1–§3 passes**, with evidence linked in the PR. §4 applies to every later write op.

## 1. Before PR-1 (foundation, read-only)
| # | Criterion | Evidence |
|---|---|---|
| F1 | `state.db` is created 0600, owned by `homelab-control`, in `/var/lib/homelab-control` (0700). The server refuses to start if the file or directory is group/world-accessible or a symlink | Go test with fixture dirs; systemd smoke check |
| F2 | Migrations are versioned, forward-only and transactional. The DB is backed up before migrating. A failed migration leaves the old DB intact and `hc-api` refuses to start with a clear error | Go tests incl. injected failure; package upgrade test (install vN → upgrade vN+1) in CI install matrix |
| F3 | Sessions persist only SHA-256(ID); idle, absolute and cap limits unchanged; revocation takes effect on the next request; restart keeps sessions | Go tests; e2e restart test |
| F4 | Broker audit: monotonic sequence, `prev_hash` chain (SHA-256), `fsync` before the response is sent. Tampering with or truncating a record is detected by `hlctl audit verify` | Go tests (truncate, edit, reorder) |
| F5 | First argument-taking op (`services.unit.logs`): strict decode (unknown fields rejected); unit name grammar; existence check; bounded `lines` (1–500) and bytes (≤ 1 MiB); argv `journalctl --unit=<name> …` with no caller-controlled flags | Table tests incl. injection strings (`-x`, `--output=…`, `;`, `$()`, newline, NUL, 300-char names); fuzz test of the arg decoder ≥ 60 s in CI |
| F6 | Log reads need scope `services.logs` in API **and** broker; reads are audited (who, unit, lines); log content is never stored | API authz tests (403 without scope); broker re-check test; audit test |
| F7 | Guard tests still pass: exec only in `runner`; every route has a scope; `hc-api` imports no broker internals | Existing guard tests |

## 2. Write framework (PR M2-2, before any write op is enabled)
| # | Criterion | Evidence |
|---|---|---|
| W1 | **Closed registry:** each write op declares `plan` and `apply` handlers, scope, risk class (reversible / disruptive / destructive), lock domain, argv template(s), timeout. A registry test lists exactly the approved ops | Registry snapshot test; reviewer sign-off in the PR |
| W2 | **Plan tokens:** HMAC-SHA256 with a per-boot random key held only in broker memory; bound to actor, op, canonical target, state digest and expiry (≤ 120 s); single-use | Tests: replay, expired, wrong actor, wrong op, wrong target, altered state digest, token from a previous boot → all rejected and audited |
| W3 | **Revalidation:** at apply, the broker re-resolves the target and compares the state digest; any change → `conflict`, no action | Test: unit changes state between plan and apply |
| W4 | **Locks:** exclusive lock per target domain (`service:<unit>`); a second plan/apply on a locked target → `busy`; locks released on completion, timeout or panic | Concurrency tests (parallel apply, panic in handler) |
| W5 | **Journal:** intent record (fsync) before execution, outcome record after. After a broker restart, intents without outcome are reported as `interrupted` in the audit and the UI, never retried automatically | Test: kill between intent and outcome; restart; inspect |
| W6 | **Step-up:** disruptive and destructive classes need a password re-entry within the last 5 min for the same session; failures count toward login throttling | API tests: missing, stale or wrong step-up → 403 and audit; throttle interaction |
| W7 | **CSRF/Origin:** all new POST routes require Origin + CSRF token (existing middleware); JSON only; size cap | Existing negative tests extended to the new routes |
| W8 | **Authorization:** `services.operate` (start/stop/restart) checked in API and broker; users without it get no write controls in the UI (and the server still refuses) | API and broker authz tests; Vitest for hidden/disabled controls |
| W9 | **root-exec runner class:** allow-listed absolute binary (root-owned, not group/world-writable); argv from a template fixed in code with `--` before the validated target; scrubbed env; stdin `/dev/null`; process group killed on timeout; output caps; no other root child is possible from read-only ops | Runner tests; guard test that only ops marked `root-exec` can request root children |
| W10 | **Protected units:** stop/restart of every unit in the D-5 deny-list, of aliases resolving to them, and of `RefuseManualStop=yes` units is refused **in the broker** with a clear reason | Table tests against fixtures; real-systemd test for `ssh.service`/`sshd.service` and Homelab Control's own units |
| W11 | **Outcome verification:** after apply, the broker reads the unit state back and reports the observed result (`active`, `failed`, …) with the journal cursor; the UI shows the observed result, not "success" | Tests on fixtures; real-systemd test with a deliberately failing disposable unit |
| W12 | **No rollback claims:** UI and API describe stop/start/restart as runtime actions; no automatic rollback; recovery guidance is shown on failure | UI test for text; review |

## 3. Environment evidence for the first write op
| # | Criterion | Evidence |
|---|---|---|
| E1 | CI real-systemd jobs (Ubuntu 24.04 runner, 26.04 container) start, stop and restart a **disposable test unit** created by the harness (`hc-test-dummy.service`), verify the observed state, and verify refusal for a protected unit | CI job logs/annotations |
| E2 | Owner KVM VM (`dev/vm/`, `hc-test-` namespace): the same scenario through the browser, on Ubuntu 26.04 **and** one non-Debian family (Fedora or Arch) | Evidence under `docs/evidence/m2/` with commit hash |
| E3 | Playwright: plan → confirm → step-up → result flow; keyboard-only; axe 0 violations on the new dialog | CI browser-e2e job |
| E4 | `systemd-analyze security` for both units does not regress beyond an explicitly reviewed delta (root-exec does not add capabilities) | VM evidence |
| E5 | Threat model updated (new STRIDE rows for write ops); `docs/security/THREAT-MODEL.md` | PR diff |
| E6 | govulncheck clean; REUSE, SBOM and notices updated (if D-1 adds a module) | CI |

## 4. Standing criteria for every later privileged operation
1. It has a typed plan/apply pair, a risk class, a lock domain, a scope at `operate`/`configure`/`destroy` level and an argv template or file-write routine reviewed in the PR.
2. Persistent targets are referenced by stable identity: unit ID, WWN/serial/by-id for disks, pool GUID, UUIDs. **Never `/dev/sdX`** (CLAUDE.md).
3. It is revalidated at apply and uses the device/resource ownership check (`MODULE-ARCHITECTURE.md` §3).
4. Config-file writes follow the same pattern:
   - write to a temp file in the same directory with `O_NOFOLLOW`;
   - validate with the service's own validator (`testparm`, `sshd -t`, `exportfs`, …);
   - back up the previous file;
   - rename atomically;
   - reload, never restart, when the service supports it.
   - Unmanaged config is never rewritten without an explicit import or takeover.
5. Destructive class:
   - typed confirmation of the target's stable identity, with physical location shown when mapped;
   - evidence from disposable disks (loop/virtual) in VMs before enablement (spec 03 release gate);
   - **no rollback promised**;
   - recovery guidance documented.
6. Tests: argument-injection table and fuzz, authz in API and broker, replay, lock, revalidation, interrupted-journal, real-environment evidence on at least two distro families.
