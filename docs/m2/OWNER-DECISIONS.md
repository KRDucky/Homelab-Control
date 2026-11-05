# M2 owner decision register

**Date:** 2026-10-21. Each entry is a decision **not** already settled by an accepted ADR or recorded
owner decision.

Already settled, and not asked again:
- the broker model (ADR 0001/0008);
- no Webmin bridge or proxy (ADR 0003);
- secrets in root-owned broker files (ADR 0004 secrets section);
- licensing (ADR 0005);
- stdlib-first (ADR 0006);
- release phases (ADR 0009);
- independent modules plus optional coordinated workflows, the technology scope, and the drive layout designer (owner, 2026-10-21).

| ID | Needed before | Status |
|---|---|---|
| D-1 | PR-1 | **Approved 2026-10-21 (option A)** |
| D-6 | PR-1 | **Approved 2026-10-21** |
| D-7 | PR-1 | **Approved 2026-10-21** |
| D-2, D-3, D-4, D-5 | PR M2-2 (first write) | **Approved 2026-10-23 (option A each)** |
| D-8, D-9, D-10 | Roadmap shape | Open |
| D-11 | Before any third-party library or tool adapter (M3+) | Open (policy only) |
| D-12 | PR M2-5 (drive locator) | Open (HBA Viewer source to be provided) |

---

## D-1 Persistence engine for non-secret state
**Decision:** how to provide the SQLite state store that ADR 0004 assigns to M2, given ADR 0006 (stdlib only).

| Option | Pros | Cons / risks |
|---|---|---|
| A. `modernc.org/sqlite` (pure Go) | `CGO_ENABLED=0` and reproducible builds unchanged; real SQL, WAL, migrations; BSD-3 | Large transitive module set (modernc libc translation); larger binary; dependency review and govulncheck coverage needed |
| B. `mattn/go-sqlite3` (cgo) | Most used; uses real SQLite C | Requires cgo and a C toolchain per distro; breaks the static, `CGO_ENABLED=0` reproducible build verified in Phase 1 |
| C. No SQL: stdlib append-only journals (JSONL + fsync) and JSON snapshot files | No dependency | Queries (audit search, jobs, layouts, resource history) re-implemented by hand; this would likely be replaced later, so the work is done twice |

**Owner decision 2026-10-21: option A approved.** CGO-disabled reproducible builds preserved; ADR 0006 amended for the reviewed exception; dependency and transitive modules pinned and audited; licensing, SBOM and scanning requirements kept; secure state storage, transactional migrations and migration backups.

**Recommended: A.**
- Amend ADR 0006 with a recorded review: licence check, pinned version, `go.sum`, govulncheck, and inclusion in the SBOM and notices generators.
- The DB is owned by `hc-api` at `/var/lib/homelab-control/state.db`, mode 0600.
- The broker's authoritative audit and operation journal stay stdlib files owned by root (see D-2), so a compromised API cannot rewrite history.

**Roadmap impact:**
- A: PR-1 as proposed.
- C: PR-1 shrinks, but every later persistence feature (layouts, jobs, audit search) costs more.
- B: reopens the reproducibility work.

## D-2 Write-operation authorization model (proposed ADR 0011)
**Decision:** how a privileged write is requested, confirmed and executed.

**Proposed model (plan → confirm → execute):**
1. The UI asks the API for a plan.
2. The API calls `<op>.plan` on the broker. The broker:
   - validates arguments;
   - resolves the target (for example, the unit's canonical ID and current state);
   - applies the policy (protected units);
   - returns a preview, the risk class (reversible / disruptive / destructive) and a **plan token**.
3. The plan token is minted by the broker: an HMAC with a per-boot random key, never leaving root memory. It is bound to actor, op, canonical target, an observed-state digest and an expiry (120 s). It is single-use.
4. The user confirms. For disruptive ops the API also requires step-up (D-3).
5. The API calls `<op>.apply` with the token.
6. The broker then:
   - verifies the token and consumes it;
   - takes the target lock;
   - **revalidates** the target and state;
   - writes an intent journal record (fsync);
   - executes;
   - records the outcome (fsync);
   - returns.

| Option | Pros | Cons |
|---|---|---|
| A. Broker-minted plan tokens + revalidation + locks + journal (above) | Replay-proof; TOCTOU bounded; audit outside API control | More moving parts; honest limit: a compromised API can still plan and apply allowed ops, so policy and narrow ops carry the real safety |
| B. API-only confirmation (token in API session) | Simpler | Broker trusts API completely; no replay or TOCTOU guarantees at the root boundary |
| C. polkit interactive authorization | OS-native | Not headless-friendly; distro-divergent; no web UX |

**Recommended: A**, recorded as ADR 0011 before PR M2-2.

**Owner decision 2026-10-23: option A approved.** Tokens minted and verified inside the broker,
120 s, single use, bound to operation, canonical target, actor, session and observed state; target
lock, revalidation, durable intent before execution, outcome after. The threat-model limitation (a
compromised API can still request operations the broker's policy allows) is documented in ADR 0011.

**Roadmap impact:** PR M2-2 implements the generic plan/apply framework once; later write ops reuse it.

## D-3 Step-up authentication for disruptive operations
| Option | Pros | Cons |
|---|---|---|
| A. Password re-entry, valid 5 minutes per session, required for disruptive and destructive classes | No new dependency; uses existing PBKDF2 path and throttling | Protects against session theft or an unattended browser, not against an API compromise |
| B. TOTP (stdlib HMAC-SHA1, RFC 6238) enrollment via `hlctl` | Real second factor | Enrollment UX and recovery codes; more scope for M2 |
| C. None (confirmation dialog only) | Simplest | B3 stays open |

**Recommended:** A in PR M2-2. B as a follow-up PR, in M2 if wanted.

**Owner decision 2026-10-23: option A approved.** Password re-entry, 5 minutes, bound to the session
and actor, existing throttling preserved, never sent to the broker or logged, cleared on revocation
or expiry. Required for stop and restart, and for start when dependencies make it disruptive.

## D-4 How the broker runs root-level service commands
| Option | Pros | Cons |
|---|---|---|
| A. New runner class `root-exec`: allow-listed binary, argv template fixed in code (e.g. `systemctl [start\|stop\|restart] -- <validated unit>`), child runs as root, same timeout, output caps and scrubbed env | Small change, auditable, no dependency | First root child processes; argv templates must stay fixed |
| B. systemd D-Bus API (`org.freedesktop.systemd1`) | Typed API, no parsing | Needs a D-Bus client: a third-party module, or a hand-written protocol implementation |
| C. polkit rule letting `homelab-control` manage units | No root child | polkit not always installed; per-distro rules; grants the API's uid the power directly (defeats the broker) |

**Recommended: A.** It amends ADR 0008 (closed registry; argv templates reviewed per op). B can be revisited if parsing `systemctl` becomes a problem.

**Owner decision 2026-10-23: option A approved** (`root-exec`; ADR 0008 Amendment 2). Fixed
`systemctl` argv with the validated unit as a separate argument; no generic root execution API.

## D-5 Protected-unit policy for service operations
| Option | Pros | Cons |
|---|---|---|
| A. Built-in deny-list enforced by the broker, not configurable in M2. Covers: Homelab Control's own units; `ssh`/`sshd`; `dbus*`; `systemd-*`; `getty@`/`serial-getty@`; `user@`; network managers (`NetworkManager`, `systemd-networkd`, `wpa_supplicant`, `networking`); display managers; and any unit with `RefuseManualStop=yes`. Stop is refused for these; restart is refused unless listed as restart-safe (none in M2-2) | Prevents lockout and self-termination | Admins cannot manage these units from the UI in M2 |
| B. Deny-list plus owner-editable allow-overrides in `/etc/homelab-control/policy.json` (root-owned) | Flexible | Policy file becomes another root-trust input |
| C. No policy, confirmation only | Simplest | Lockout risk |

**Recommended:** A now; B later with its own review.

**Owner decision 2026-10-23: option A approved.** Built-in, broker-enforced, no user-editable
overrides in M2-2; aliases, templates, alternate names and dependencies must not bypass it.

## D-6 Persistent sessions
ADR 0002 schedules "persisted sessions and per-session revocation UI" for M2, but the move from memory to disk changes behaviour.
- **Owner decision 2026-10-21: approved.** Hashes only; existing idle, absolute and count limits; view and revoke own sessions; fixation protections and per-request checks kept.
- **Recommended: yes.** Store only SHA-256(session ID) in `state.db`, keeping the existing timeouts; add a session list and revoke-other-sessions in the UI. Throttle state is persisted too.
- **Alternative:** keep in-memory sessions; restarts log everyone out (availability only).
- **Impact:** part of PR-1.

## D-7 Scope catalogue v1
**Owner decision 2026-10-21: approved**, compatible with existing scopes; logs and audit access are **not** granted automatically to existing accounts.

**Recommended convention** `<module>.<level>`. Levels:
- `read`: inventory and status.
- `logs`: may include sensitive content.
- `operate`: reversible or disruptive runtime actions, e.g. start, stop, restart.
- `configure`: persistent configuration, e.g. enable or disable, edit.
- `destroy`: irreversible data loss, e.g. wipe, destroy pool.

Examples:
- **M2:** `services.read`, `services.logs`, `services.operate`, `services.configure`, `storage.read`, `layout.manage`, `audit.read`, `sessions.manage`.
- **Later:** `storage.plan`, `storage.execute` and `storage.destroy` per technology, `docker.operate`, `vm.operate`, `shares.configure`.

Existing scope names stay valid. Specs 03/addon-03 names (`storage.plan`, `storage.execute`, `shares.*`) map onto this.
- **Alternative:** one coarse `admin` scope. Rejected: it defeats the least-privilege intent of spec 03.

## D-8 Milestone order after M2
`docs/project/04-ROADMAP.md` orders M3 Theme/Plugin SDK → M4 Software Center → M5 disk inventory and bay map → … → M11 containers/VMs.

**Recommended, to deliver usable modules sooner:**
- (a) M2 as in `M2-ROADMAP.md`. This pulls **drive locator Stage A** and the **read-only storage inventory** forward from M5; both are non-privileged.
- (b) Defer the Theme/Plugin SDK (old M3) until after the first storage and container modules. Modules ship as built-in first-party modules (D-10).
- (c) Defer the ADR 0003 parity harness to the first milestone that replaces a Webmin parser.
- (d) Adopt the module milestone sequence in `M2-ROADMAP.md` §4.

**Alternative:** keep 04-ROADMAP as written. Theme/plugin work comes before any module; useful functions arrive later.

**Impact:** 04-ROADMAP would be revised in a follow-up docs PR once approved. It is unchanged here.

## D-9 Host administration in M2
- **Recommended:** hostname and timezone as an **optional** M2 slice (PR M2-6). These are low-risk, through `hostnamectl`/`timedatectl`, and they exercise the write framework on a second module.
- Users and groups move to their own milestone: lockout, sudo and UID/GID ownership risks, and they are tied to identity work (PAM is still deferred).
- **Alternative:** no host administration in M2.

## D-10 Module packaging: built-in modules vs plugins
- **Recommended:** first-party modules are **compiled into** `hc-api`/`hc-broker` as independent packages behind a module registry: capability detection, enable/disable, scopes, routes, broker ops.
- Third-party plugins wait for the Plugin SDK (spec 06). This keeps one reviewed broker binary and avoids runtime code loading, which ADR 0001 forbids for privileged code anyway.
- **Alternative:** design the plugin SDK first, so modules are plugins from day one. This is slower and adds a privileged-plugin review problem before any module exists.

## D-11 Third-party integration libraries (policy for later milestones)
Not needed for M2; recorded so it is not decided ad hoc.
- **Docker:** Engine API over the Unix socket with stdlib `net/http`; no SDK.
- **libvirt:**
  - `virsh` argv templates with XML/`--print-xml` parsing (stdlib), or
  - the pure-Go `digitalocean/go-libvirt` (Apache-2.0; ADR 0006 review).
- **ZFS:** `zpool`/`zfs` JSON (`-j`, OpenZFS ≥ 2.3) or `-Hp` output.
- **Broadcom `storcli`:** proprietary; invoke only if the user installed it, never bundle.

**Recommended:**
- the stdlib approach for Docker;
- `virsh` first for libvirt, re-evaluated at the virtualization milestone;
- `storcli` optional.

## D-12 Source of the "HBA Viewer" concept and reuse terms
**Owner direction (2026-10-21):** reuse the existing HBA Viewer concept inside Homelab Control rather than
building a separate drive-location system.

**Status:** pending.
- **Correction:** the owner clarified that the HBA Viewer and the Linux Storage Manager are **distinct projects**. The Storage Manager is not the intended source.
- **Needed:** the HBA Viewer's location (repository or folder) and the reuse terms: concept-only re-implementation (recommended), or code contributed by the owner under GPL-3.0-or-later.
- **Blocks:** PR M2-5 (drive locator). Nothing in PR-1.

---

## Post-merge record: M2-2 acceptance item E2 (2026-10-27)
**Source:** the owner's message titled "HomeControl — Documentation Reconciliation Decisions", sent 2026-10-27 at
00:38 America/Chicago (05:38:54 UTC), in Claude Code session https://claude.ai/code/session_01PcnJ5uXahpyCLU3FrjHqki.

**Decision, in the owner's words:** "E2: Deferred, not waived. Workstation-VM browser validation remains an open
acceptance item and is required before enabling `services.operate` in a real deployment. Do not describe E2 as
passed."

**Scope note (not part of the owner's text):** the decision names E2 only. It does not cover E4 or any other acceptance
criterion, and it was given after PR #28 merged (2026-10-26), so it does not retroactively satisfy the merge condition
in `M2-SECURITY-ACCEPTANCE.md`. Current status of the criterion: `M2-2-ACCEPTANCE.md`.

## Post-merge acceptance closure (2026-10-29)

**Source:** the owner's project conversation: “I approve closing E2/E4 and preparing the documentation PR”. E2 and E4 are closed on [final combined-main KVM evidence](../evidence/m2/vm-final-main-d812995e/README.md), source `d812995e`, CI 40666815912: Ubuntu 26.04.1, Fedora 44 and upstream Arch. The earlier October 27 deferred-not-waived quotation remains the decision at that time.

Earlier owner validation instructions explicitly record E2 acceptance and W1 review/sign-off on October 27 based on the owner's Ubuntu run, with non-Debian validation deferred. The owner also explicitly authorized the updated Ubuntu U1 baseline and retest in “Final Ubuntu 26.04.1 Acceptance Rerun (U1)”. These sources are conversation instructions, not inferred GitHub review events.

Closure does not retroactively satisfy PR #28's pre-merge condition, enable production `services.operate`, authorize another privileged operation, or authorize M2-3. The evidence index preserves the Ubuntu phase-gap and reused-install caveats. This approval permits a documentation PR; merge remains an owner action.

## M2-3 development authorization (2026-11-04)

**Source:** the owner's instruction "Implement Homelab Control M2-3: Expanded Service Management", 2026-11-04,
section *Owner authorization*: "M2-2 is complete. I authorize M2-3 development and isolated testing within the
scope below. Production activation and merge remain separate owner decisions." The instruction states that it
supersedes older statements that M2-3 is unauthorized, only for its scope (boot-time enable/disable with a
separate `services.configure` permission, read-only timer and socket views, unit details, a minimal Services
module registry). It excludes mask/unmask, unit editing or creation, a generic daemon-reload, arbitrary commands,
production changes and new dependencies, and M2-4, M2-5, M2-6 and M3 (M2-5 and M2-6 remain mandatory before M3).

**Not decided by it:** acceptance of the M2-3 implementation, merge, production activation of
`services.configure`, and the items below. Implementation and evidence: `M2-3-DESIGN-REVIEW.md`,
`M2-3-ACCEPTANCE.md`.

### Items for the owner's review of M2-3
| ID | Question | Implemented proposal (pending) |
|---|---|---|
| O-1 (D-10) | How first-party modules are packaged and toggled | Built-in, compiled-in modules behind `internal/modules`; the setting is the root-owned `/etc/homelab-control/modules.json`, not `state.db`, because the broker must enforce it without trusting `hc-api`; no UI/API to change it; invalid file disables all modules. D-10 itself stays **open** |
| O-2 | Scope for unit-file contents | Reuses `services.logs` (existing non-default scope for sensitive content). Alternative: a dedicated scope, e.g. `services.files` |
| O-3 | Risk class of enable/disable | `disruptive` (step-up always), stricter than the roadmap's "persistent → reversible" |
| O-4 | `--no-reload` | No manager reload (the excluded generic daemon-reload). The setting is written to disk at once and applied at the next boot **or earlier**, at any later daemon-reload or first load of a target (corrected 2026-11-04, probe 7 P19/P20); host `systemctl status` shows the old value until then |
| O-5 | Refused constructs | `Also=`, `RequiredBy=`, `UpheldBy=`, `DefaultInstance=`, template instances, `.socket`/`.timer` enablement, SysV/generated/linked/runtime units are refused, not supported; each could be a later, separately reviewed extension |

### Design proposals for the owner's final approval (P-1…P-9) — PROPOSALS, NOT DECISIONS
Status of every item below: **proposed, not approved.** The owner asked (2026-11-04) for these to be prepared
for final approval; until the owner approves an item in their own words it is not a decision, and D-10
remains open. Each item states what the branch implements now, so that approval or rejection has a concrete
effect. Evidence: `M2-3-DESIGN-REVIEW.md` §1, §1.1, §1.2, §7, §8.

| ID | Proposal | Implemented on the branch now | If approved | If rejected |
|---|---|---|---|---|
| P-1 (O-1, D-10) | Root-owned module configuration `/etc/homelab-control/modules.json` | Yes | D-10 can be closed with this mechanism | Choose another source; the broker must still not trust `hc-api` for it |
| P-2 (O-2) | A separate permission for unit-file contents (e.g. `services.files`), never default, **not implied by** `services.logs` | **No** (contents currently need `services.logs`) | Implement as a focused change (scope, broker registry, API route, OpenAPI, UI gating, tests) | Keep `services.logs`; heuristic redaction stays the only extra control |
| P-3 (O-3) | Password confirmation (step-up) for every boot-time change (`disruptive`) | Yes | Keep | Re-class as `reversible`; a stolen session could change boot settings without the password |
| P-4 (O-4) | `--no-reload`, with the measured semantics below | Yes | Keep | Adding a reload is excluded by the M2-3 instruction; it would need its own authorization |
| P-5 (O-5) | Explicit refusal of unsupported constructs | Yes | Keep the list; extensions reviewed separately | Name constructs to support; each needs its own review |
| P-6 (new) | Enable (and, since `ad725d43`, disable) fails closed while unit files changed after systemd's last load (§8). Its former second half — refusal whenever runtime unit configuration exists — was replaced under P-8 in `00fb7f2b` | Yes | Keep | The only safe alternative found is to reload (excluded) or to rebuild the boot graph from disk (not feasible here) |
| P-7 (B-1) | Disable refuses when the affected boot dependency relationships reach a protected unit | Yes, since `ad725d43`/`ae937d8a` (implementation pending owner review) | **Safety objective approved by the owner, 2026-11-05** (below); the implementation is not approved by it | — |
| P-8 (B-2) | How to treat runtime unit configuration in `/run/systemd/system` | Option 3 since `00fb7f2b` (implementation pending owner review) | **Approach (option 3) approved by the owner, 2026-11-05** (below); the implementation is not approved by it | — |
| P-9 (new, final review) | Enable is judged against the **next boot**, not against what is running now (boot mode; `M2-3-DESIGN-REVIEW.md` §11) | Yes, since `f8d3af2e`, completed in `eb2fc2d4` (correction for owner review) | Keep | Revert to the M2-2 start walk: enabling can then break a protected unit at the next boot (reproduced) |

### Consolidated decision table (2026-11-05) — PROPOSALS, NOT DECISIONS
Status of the branch: product `eb2fc2d4`. "Approval needed" is what the owner would decide; nothing below is
decided unless quoted verbatim as an owner decision.

| ID | Recommendation | Trade-off | Implementation status | Approval needed |
|---|---|---|---|---|
| D-10 | Close D-10 with P-1's mechanism (built-in modules; root-owned file) | No UI/API toggle; root and a restart to change | Implemented as proposed | Owner decision: close D-10 (or choose another mechanism) |
| P-1 | Approve root-owned `/etc/homelab-control/modules.json`, read by both processes at start | Invalid file disables all optional modules; changes need root + restart | Implemented | Approve the mechanism |
| P-2 | Approve a separate `services.files` permission for unit-file contents (scope below) | One more permission to grant; downgrade needs the scope removed first | **Not implemented** (contents need `services.logs`) | Approve the scope so it can be implemented, or keep `services.logs` |
| P-3 | Keep step-up for every boot-time change | Password at most every 5 minutes | Implemented | Approve the `disruptive` risk class |
| P-4 | Keep `--no-reload`; document "on disk now, effective at the next reload or boot" | Host `systemctl status` shows the old setting until then; any later reload applies it | Implemented | Approve the semantics |
| P-5 | Keep refusing unsupported constructs | Static, template, socket/timer-enabled, `RequiredBy=`/`Also=` units cannot be managed | Implemented | Approve the list |
| P-6 | Keep the change gate for both actions | After an unreloaded edit or an interrupted operation, both actions wait for a reload or boot | Implemented | Approve |
| P-7 | Accept the boot-pull implementation (with R21–R23 of §11) | Over-refuses when the unit is the only persistent puller of a protected unit started otherwise (e.g. generated or runtime relations) | Objective approved 2026-11-05; implementation pending | Accept the implementation |
| P-8 | Accept option 3 as implemented (with R21–R23) | Residual unmodelled boot sources (§10, §11) | Approach approved 2026-11-05; implementation pending | Accept the implementation |
| P-9 | Accept the enable boot mode | Refuses where the M2-2 start model would allow (running dependencies, socket activation, conflicts with stopped boot units are checked). Measured cost on stock images and three homelab fixtures: no decision changed on any of five images; plans 139–977 ms instead of 17–55 ms | Implemented (`f8d3af2e`, `eb2fc2d4`) | Accept the correction |
| KVM A | Authorize Phase A on new, retained guests (`M2-3-KVM-ACCEPTANCE-PROCEDURE.md`) | VM time; no guest deletion assumed | Procedure ready; provenance from the final CI | Authorize Phase A |
| KVM B | Authorize Phase B (broker interruption, SELinux denials review) separately, after Phase A | Deliberate broker kills on the guests | Procedure ready | Authorize separately |

**P-2 exact scope (prepared for approval; not implemented).** A new permission `services.files` —
"read unit file and drop-in contents" — required instead of `services.logs` by exactly one route and one broker
operation: `GET /api/v1/services/units/{unit}/contents` (`backend/internal/api/server.go`) and
`services.unit.contents` (`backend/internal/broker/ops/ops.go`; the broker checks the scope itself). Never in
`DefaultScopes`, not implied by `services.logs`, `services.read` or any other scope. Changes: `auth.KnownScopes`
and the scope constant; the route and op registry entries; the OpenAPI scope enum and `x-required-scope`; the
Services module's scope list (`internal/modules`); the UI's unit-file gating and its explanation
(`UnitDetailPanel.tsx`); the scope catalogue (`docs/project/03-SECURITY-AND-OPERATIONS.md`), user guide and threat
model; tests (guard route/OpenAPI scope consistency, broker scope refusal, API 403, browser checks, scenario
grants). Unchanged: unprivileged `systemctl cat` as the service account, root-only files never shown, heuristic
redaction, limits, audit-before-release. No new privileged operation or template.
*Compatibility:* the contents route exists only on this unmerged branch (not on `main`, not in
`v0.1.0-dev.1`), so no released client or account changes. On a host running the branch, an account with
`services.logs` keeps journal logs but loses unit-file contents until granted `services.files`
(`hlctl user scopes`). A users file containing `services.files` is rejected by builds that do not know it, and
`hc-api` then does not start (`users file: … unknown scope`) — the same already applies to `services.configure`;
remove the scope before a downgrade.

**P-8 owner decision (2026-11-05), verbatim:** "I approve investigating and implementing P-8 option 3: replace the
blanket /run/systemd/system refusal with narrowly scoped, fail-closed checks. This approves the approach, not
acceptance of the implementation. Preserve P-7's protected-access safety objective."
Implemented in `00fb7f2b` (`M2-3-DESIGN-REVIEW.md` §1.4, §3.4, §10, including the review of what the relevance
calculation can miss). Acceptance of the implementation remains the owner's.

**P-8 options as presented before the decision (B-2).** Measured on the GitHub Ubuntu 24.04 runner VM: netplan's
entries in `/run/systemd/system` make every enable and disable refused. Options:
1. *Keep refusing* (current): fail closed; the feature is unusable on netplan-based Ubuntu hosts.
2. *Exclude `/run/systemd/system{,.attached}` like the generator directories*: usable; residual — manual
   runtime configuration (`systemctl --runtime mask|enable`, tools writing there) is loaded now but absent at
   the next boot and is not evaluated (for example a runtime mask hiding a conflicting unit from the review).
3. *Refuse only runtime entries that relate to the review's units* (masks or links naming a unit in the enable
   start walk or in the disable boot-pull sets): usable where netplan's entries concern only network units the
   review does not reach; needs design and real-host measurement; still not a guarantee.
4. *Allow-list known generator-produced runtime entries* (netplan): narrow but distribution-specific and
   fragile.
Recommendation for review: option 3, measured on the KVM guests, with option 1 retained until then. The
choice belongs to the owner.

**P-1 detail.** Validation: strict decoding (`version` 1, known IDs, no unknown fields, size limit); refused if a
symlink (`O_NOFOLLOW`), not a regular file, not root-owned, or group/world-writable; any error disables every
optional module (logged). Enforced independently by the broker (`module_disabled`, before the scope check) and
by `hc-api` (404 identical to unknown routes, before authentication). `/etc/homelab-control` is
`root:homelab-control 0750` in the packages, so `hc-api` cannot delete the file (deleting it re-enables modules).
Trade-offs: root and a restart of both units are needed to change it; a process reads it at start only.

**P-2 detail.** Heuristic redaction is not a secrecy guarantee: known gaps are positional secrets
(`-u user:pass`, `-pSECRET`), quoted header values, URL query strings and secret settings under other names;
a continuation-line gap was found and fixed (R6). Root-only files are never shown; every read is audited.

**P-3 detail.** A boot-time change is persistent, can take effect before a reboot (P-4) and is observed later,
when nobody watches the dialog. Cost: one password entry per 5-minute window.

**P-4 detail (measured on systemd 249, 255 and 259; probe 7 P19/P20, probe 8 P24).**
- *Immediately:* the links are created or removed on disk; `systemctl is-enabled` reports the new state; the
  running manager's dependency graph is unchanged (`multi-user.target` does not `Want=` a newly enabled unit,
  still `Wants=` a disabled one); `show -p UnitFileState` is stale; `NeedDaemonReload` may become `yes`.
- *At a later reload* (by anyone; package installs and updates commonly run one): the manager applies the new
  links; a later start of the target then starts (or no longer starts) the unit. The first load of a target
  that was not loaded yet already reads the new links, without a reload.
- *At reboot:* the new setting is used.
- *Unchanged:* the running state (no `--now`), and other ways a unit starts (`Requires=`/`Wants=` from other
  units, socket/timer/path/D-Bus activation, manual starts).
- Never promised: "takes effect only at the next boot".

**P-5 detail.** Refused, each with a reason in the plan error: `Also=`, `RequiredBy=`, `UpheldBy=`,
`DefaultInstance=`, specifiers, continuations, resets and unknown keys in `[Install]`; templates and instances;
`.socket`/`.timer` enablement; SysV, generated, transient, linked, runtime, masked, static, indirect and alias
states; `Alias=` naming an existing unit; non-symlink files at link paths; unit files on disk that differ from
systemd's loaded view; links to the unit not described by `[Install]`; runtime links; links pointing elsewhere;
shared drop-ins with an `[Install]` section; (enable) any unit-file change systemd has not loaded (P-6);
(disable) `Requisite=` on disk from a protected unit or a shared drop-in.

**P-6 detail.** Measured (probe 8): changes written without a reload — a new shared drop-in, the unit's own
edited fragment, or **another** unit's edited file — are invisible in systemd's loaded graph but used at the
next boot; three of the four isolated cases stopped a stand-in protected service. Behaviour: enable is refused
until the next reload or boot whenever anything in the unit directories changed after the last load, except
Homelab Control's own journaled enablement changes. Trade-offs: after an administrator's unreloaded change, a
package that does not reload, runtime unit configuration, or an interrupted Homelab Control operation, enable is
unavailable until a reload or reboot (disable stays available). Since `ad725d43` the gate applies to **disable as well**
(its boot-pull check uses the loaded graph); own journaled changes stay excused, but after an interrupted
operation or another unreloaded change neither enable nor disable is possible until a reload or reboot.
Measured: a fresh install with the documented first steps is **not** affected (probe 9, all four versions; an earlier rule with a 100 ms margin would have
been, R14). On systemd 249 the last reload time is the generator directory's change time (measured on 249,
255 and 259, but not a documented interface). Not yet measured: cloud-image boots (cloud-init, netplan).

**P-7 owner decision (2026-11-05), verbatim:** "I approve the safety objective behind P-7: disable must refuse
when the affected boot dependency relationships reach a protected unit, including remote-access units. This
does not approve a particular implementation or broader privileged operations."
Implemented in `ad725d43` and corrected in `ae937d8a` (`M2-3-DESIGN-REVIEW.md` §1.3, §3.4, §9): the boot graph from `default.target`
without the unit, through `Requires=`/`Wants=`/`BindsTo=`/`Upholds=`/`Triggers=`; refusal when a unit
reached only through the disabled unit is protected by any name; fail closed on unknown boot target,
uninspectable services and walk budget; disable also requires that nothing changed on disk after the last
load. The implementation, its limits and its over-refusal behaviour remain for the owner's review.

**P-7 background (before the decision).** Measured with a unit test: disabling a unit that `Wants=`/`Requires=`/`BindsTo=`/`Upholds=`
`wg-quick@wg0.service` is accepted today. A remote-access or other protected unit started only through that
unit no longer starts at the next boot. A strict rule (refuse whenever such a dependency is protected) would
also refuse units that merely require an always-started protected unit (for example `dbus.socket`); the
proposal therefore refuses only when the protected dependency is not itself enabled at boot (`is-enabled` other
than `enabled`), and needs the owner's choice.
