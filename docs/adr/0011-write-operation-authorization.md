# ADR-0011: Write-operation authorization (plan → confirm → execute)

**Status:** **Accepted** 2026-10-23 (owner; decisions D-2, D-3, D-4 and D-5, all option A, in the M2-2
authorization). It covers the first privileged write operations: start, stop and restart of systemd
`.service` units. Every later write op reuses this framework and needs its own review.
**Date:** 2026-10-23 (decision); implemented in M2-2 (2026-10-26).
**Amends:** ADR 0008 (Amendment 2: the `root-exec` runner class).
**Related:** `docs/m2/OWNER-DECISIONS.md` D-2…D-5, `docs/m2/M2-SECURITY-ACCEPTANCE.md` W1–W12.

## Context
Until M2-2 every broker operation was read-only, which is why a compromised `hc-api` was acceptable:
it could only read bounded, non-secret data (ADR 0008, trust assumptions). The first write
operations change the host. They need a confirmation that the browser cannot fake, protection
against replay and stale reviews, mutual exclusion, durable records written before anything runs,
and an honest report of what happened.

## Decision

### 1. Two broker operations per write: `<op>.plan` and `<op>.apply`
For services they are `services.unit.plan` and `services.unit.apply`, both requiring scope
`services.operate` (D-7 level `operate`), checked in `hc-api` **and** in the broker.

**Plan** (read-only; `systemctl show` as `homelab-control`):
1. strictly decodes `{unit, action}`; `action ∈ {start, stop, restart}`; the unit matches the
   write grammar (`.service`, no template base, no escapes, no leading `-`);
2. requires an authenticated session (actor user and session key);
3. resolves the unit through systemd: canonical ID, every alias (`Names`), unit file, state,
   safety properties and dependencies;
4. applies the protected-unit policy (§4) and the dependency checks;
5. returns the effect, the consequences, the units also stopped or started, the risk class, the
   observed state and a **plan token**.

**Apply** (the only `root-exec` op):
1. verifies and **consumes** the plan token (§2), including the step-up check (§3), atomically;
2. takes the exclusive lock `service:<canonical id>`; a second operation on the unit gets `busy`;
3. **revalidates**: reads the unit again — with the fixed, read-only **root** template
   `systemctl.show`, so the facts are not taken from a child process running as `hc-api`'s uid,
   which a compromised `hc-api` could signal or whose output pipe it could read and write —
   recomputes the state digest and the dependency assessment, and refuses with `conflict` if
   anything changed — the unit's identity, aliases,
   unit file, state, invocation, main PID, safety properties, dependencies, or the set of units
   the operation would also affect;
4. writes the **intent** record to the operation journal (fsync); if that fails, nothing runs;
5. runs the fixed root template (`systemctl --no-ask-password --job-mode=fail <verb> -- <unit>`);
6. reads the state back (root template again) until it settles (≤ 10 s) and classifies the
   **observed** outcome:
   `succeeded`, `failed` or `uncertain`;
7. writes the **outcome** record (fsync) and returns it. No retry, no rollback.

### 2. Plan tokens (D-2)
- Minted by the broker. Format `hcp1.<base64url(payload)>.<base64url(HMAC-SHA256)>`; the key is 32
  random bytes generated at broker start, held only in broker memory, never written or logged.
- Bound to: op, action, canonical target, actor, session key (SHA-256 of the session ID, not the
  cookie), state digest (unit digest + digest of the affected-unit set), risk class, a random ID
  and the expiry (**120 s**).
- **Single use:** the broker keeps outstanding token IDs in memory (at most 16 per actor and 256 in
  total; beyond that the oldest are dropped, an actor's own first) and deletes the ID when the
  token is consumed. A replayed token is unknown. Verification, the step-up check and
  deletion happen under one lock, so concurrent applies cannot both use one token.
- A broker restart (new key, empty set) invalidates every earlier token.
- A token never authorizes another target, action, actor or session; any mismatch is
  `invalid_token` and is audited. A missing step-up does **not** consume the token, so the user can
  re-enter the password and confirm before it expires.
- Plan tokens are never written to the operation journal, the broker audit or the API audit
  (only the token ID is journaled); a real-systemd test greps both files for `hcp1.`.

### 3. Step-up authentication (D-3)
- `POST /api/v1/auth/step-up {password}` re-verifies the session user's password with the existing
  PBKDF2 store. Failures count toward the login throttle; the request needs Origin + CSRF.
- The re-entry is valid for **5 minutes** for that session only (keyed by session key and bound to
  the username). It is cleared on logout, on revocation of the session and when the account is
  disabled; it lives in memory, so an `hc-api` restart clears it too.
- The password never leaves `hc-api`. The broker receives only `step_up_at` (Unix seconds) in the
  actor and requires it to be within the window for the `disruptive` and `destructive` classes.
- Stop and restart are always `disruptive`. Start is `reversible` unless the unit (or a unit the
  start pulls in) conflicts with other non-target units, which systemd would stop; then it is
  `disruptive` and needs step-up as well. The confirmation dialog alone never replaces step-up.

### 4. Protected-unit policy (D-5)
Fixed in code (`internal/servicepolicy`), enforced by the broker, no user overrides in M2-2. Stop
and restart are refused for Homelab Control's own units, SSH, D-Bus and polkit, core systemd units
(`systemd-*`, `init`), getty/console units, user-session units, network managers and configuration,
firewalls, remote-access tunnels, display managers, storage infrastructure (ZFS, LVM, md, multipath,
iSCSI), `auditd`, and power/recovery units. The broker refuses **start** of these units as well
(stricter than D-5 requires, so that no protected unit can be operated from the UI in M2-2); no unit
is listed as restart-safe.

Bypass prevention:
- names are matched case-insensitively against the canonical ID, **every alias**, the unit-file
  basename and the **template base** (`getty@` for `getty@tty1.service`);
- systemd's own `RefuseManualStart=`/`RefuseManualStop=`, `CanStart`/`CanStop` are honoured;
- rules match the base name of **every unit type** (`ssh.socket`, `network.target` and
  `ssh.service` are all covered by the `ssh`/`network` rules);
- **dependencies are walked transitively** (at most 96 related units per operation, all
  directions together; batched `systemctl show`):
  - *every unit that would be stopped* — the stop's propagation (`RequiredBy`, `BoundBy`,
    `ConsistsOf`, `RequisiteOf`, `PropagatesStopTo`), and for a start every running unit it
    conflicts with together with that unit's own propagation — must not be protected, must not
    carry a reboot/power-off `*Action=`, and must not trigger units in `isolate` job mode; a unit
    whose state cannot be verified counts as running; one whose name cannot be queried is refused;
  - *every unit that would be started* — the unit, the inactive units its start pulls in
    (`Requires`, `Requisite`, `Wants`, `BindsTo`, `Upholds`), units triggered by a start or by a
    stop (`OnFailure=`, `OnSuccess=`), and for restart the dependents restarted with it — must not be
    a shutdown/reboot/recovery unit or pull one in, must not carry such an action or isolate, and
    gets the conflict check above;
  - a graph larger than the bound is refused, not partly checked.
- `hc-api` applies the name-level policy only to label units and disable controls; the broker
  decides.

### 5. Journal (operation state)
`/var/log/homelab-control-broker/operations.jsonl`, root 0600, the same hash-chained, fsync'ed
format as the broker audit (`internal/audit`, records carry a structured `data` field covered by
the hash). It is the authoritative record of privileged operations: `op.intent` before execution,
`op.outcome` after. At broker start, intents without an outcome are recorded as `op.interrupted`
together with the unit's state observed at that moment; they are never retried. If the journal
cannot be opened, write operations are disabled and read-only operations keep working. It is
listed by `operations.journal.list` (scope `audit.read`) and verified with
`hlctl audit verify --file …/operations.jsonl`.

### 6. API audit
`hc-api` records `services.op.plan` (with risk) and `services.op.apply` `requested` **before**
calling the broker; if that record cannot be written, nothing is planned or executed (fail closed).
The observed outcome or the refusal code is recorded afterwards. No passwords, cookies or tokens.

## Threat-model limitation (explicit)
`hc-api` authenticates to the broker by peer uid and **asserts** the actor, session key, scopes and
step-up time. A compromised `hc-api` can therefore plan and apply any operation the broker's own
policy allows, for any user name it chooses, and can claim a step-up. The plan-token mechanism
prevents replay, target or action substitution and execution on stale state; it is **not** a
substitute for strict broker authorization. What a compromised API can do is bounded by the broker:
three verbs, `.service` units only, the protected-unit and dependency policy, one lock per unit, a
journal it cannot write, and no other root child process. Stronger end-user binding (for example a
broker-verified second factor) is future work.

## Known limitations (accepted for M2-2)
- **Lock scope:** the lock is per target unit. Operations on *related* units (stop A while starting B
  that requires A) are not mutually excluded by the broker; `--job-mode=fail` makes systemd refuse
  a job that would replace one queued on the same unit, and revalidation catches state changes up to
  the moment of the lock, which only partly covers this.
- **Activation window:** an inactive protected dependent is not counted as stopped. If it is
  activated (socket, timer, login) in the milliseconds between revalidation and `systemctl`, systemd
  may stop it by propagation.
- **Name-based protection:** the deny-list is a fixed set of names and categories. Services that
  are harmful to stop but not listed (for example a reverse proxy in front of Homelab Control, a
  container runtime, a hypervisor) are the operator's decision with `services.operate`, which is
  never granted by default.
- **Broker concurrency:** many concurrent plan requests can briefly fill the broker's eight
  in-flight slots; other calls get `busy` and can be retried.
- **API result loss:** if the broker connection is lost during apply, `hc-api` reports and audits
  `outcome_unknown`; the operation journal remains authoritative.
- **Journal listing race:** if fsync of an intent fails, the record is truncated back and nothing
  runs, but a concurrent journal listing may briefly show it as *pending*.
- **Walk cost:** a graph near the 96-unit bound costs several batched `systemctl show` calls; on a
  very slow host plan or apply can time out, which refuses the operation.
- **`StopWhenUnneeded=`:** a running dependency with `StopWhenUnneeded=yes` is checked as if the
  stop stopped it, whether or not something else still needs it (conservative).

## Security review (2026-10-26)
An independent review of the implementation found no way to execute anything as root other than the
fixed templates and confirmed the token, binding, journal and step-up mechanics. Its findings were
fixed before the PR: apply's reads moved to the root `systemctl.show` template (finding: unprivileged
children share `hc-api`'s uid); protection matches every unit type; second-order effects (stop
triggers, restarted dependents, conflicted units' propagation, `isolate` job modes) get the full
checks; the deny-list gained Arch `netctl`, OpenVPN, strongSwan/IPsec, Nebula, Open vSwitch,
`dm-event`/`lvm2` and identity services (SSSD, nslcd, Winbind, nscd, slapd); per-actor token bounds;
an outcome is journaled if apply panics; a failed journal write is truncated back; repeated
`systemctl show` keys are refused; lost apply results are `outcome_unknown`. A re-review of these
fixes found no new critical, high or medium defects; its low items were fixed as well
(`StopWhenUnneeded=` dependencies, uninspectable `.service` dependencies refused, template instances
of the `nebula`/`ipsec` rules) or listed above. The limitations above remain.

## Options considered
| Option | Verdict |
|---|---|
| A. Broker-minted tokens + revalidation + locks + journal (chosen, D-2) | Replay-proof; TOCTOU bounded to the revalidation-to-exec window; audit outside API control |
| B. API-only confirmation | Broker trusts the API completely; rejected |
| C. polkit interactive authorization | Not headless-friendly, distro-divergent; rejected |
| D-Bus `org.freedesktop.systemd1` instead of `systemctl` | Needs a D-Bus client (stdlib policy); revisit if parsing becomes a problem (D-4 option B) |

## Consequences
- Later write ops reuse `internal/broker/writes` (tokens, locks, journal) and add their own
  plan/apply pair, risk classes, lock domain and reviewed root templates.
- The registry snapshot test (W1) and the root-template snapshot test (W9) must be updated, and
  reviewed, for every new write op.
- Not authorized here: enable/disable, mask/unmask, unit-file edits, daemon-reload, any other unit
  type, host shutdown or reboot, arbitrary commands.
