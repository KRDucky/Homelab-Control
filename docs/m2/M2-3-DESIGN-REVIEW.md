# M2-3 design and ADR 0011 review: expanded service management

**Date:** 2026-11-04 · **Status:** implemented on branch `feature/m2-3-expanded-service-management` for
owner review. **Owner acceptance, merge and production activation are pending**; nothing here is accepted
until the owner says so.

## 0. Authorization (recorded verbatim in scope)
On 2026-11-04 the owner wrote: "M2-2 is complete. I authorize M2-3 development and isolated testing within
the scope below. Production activation and merge remain separate owner decisions." The scope:
1. service enable/disable at boot, with a separate `services.configure` permission that is never granted by
   default, an explanation of boot-time vs runtime, and no implicit `--now`;
2. read-only timer and socket views;
3. unit details: dependencies, unit-file path, last exit, restart counters, bounded read-only unit-file
   contents; unloaded, missing, static, generated, template and alias units handled without inventing state;
4. a minimal Services module registry: capability-aware navigation, disabled module routes 404 and broker
   ops refused, the D-10 choice documented for review, no executable plugin framework.

Excluded by the same instruction: mask/unmask, unit editing or creation, a generic daemon-reload
operation, arbitrary commands, production changes, new dependencies, M2-4, M2-5, M2-6 and M3. This
instruction supersedes earlier statements that M2-3 is unauthorized, for this scope only.

## 1. Observed systemd behaviour (disposable containers, 2026-11-04)
Measured in throwaway containers (Ubuntu 26.04, systemd 259.9; Fedora 44, systemd 259.10) before the
design was fixed. Scripts and raw output: `docs/evidence/m2/m2-3/probes/`.

| # | Question | Observation (both families) | Design consequence |
|---|---|---|---|
| P1 | Does `enable`/`disable` reload the manager? | Yes: without `--no-reload` the journal shows a full daemon-reload; with it, none | Templates use `--no-reload`. No implicit global reload (that would be the excluded generic daemon-reload) |
| P2 | Who writes the symlinks? | PID 1, over D-Bus: `enable` succeeds with `/etc` read-only in the caller's mount namespace | `ProtectSystem=strict` neither blocks nor bounds the change; the broker's own checks must |
| P3 | Is `show -p UnitFileState` fresh after `--no-reload`? | No: it stays stale; `systemctl is-enabled` (computed from disk) is fresh | Plan, revalidation and read-back use `is-enabled`, never `UnitFileState` |
| P4 | Side effects of `--no-reload` | The unit reports `NeedDaemonReload=yes` afterwards | Shown as a notice, not a refusal (a refusal would block undoing a change) |
| P5 | What does `disable` remove? | The unit's `WantedBy` links **and** its `Alias=` links; `Also=` units are enabled/disabled with it | Aliases are protected-checked; `Also=` is refused (§3.4) |
| P6 | `[Install]` in drop-ins | Honoured (`Alias=` from a drop-in was created) | The broker parses the fragment **and** every drop-in |
| P7 | Static unit | `enable` prints a warning and exits **0**, changing nothing | Allowed states come from observed state, never from exit codes |
| P8 | SysV | Ubuntu 26.04 still ships `systemd-sysv-install` (an unaudited root script run by `systemctl` itself) | Units with a `SourcePath=` or a fragment outside the vendor/admin unit directories are refused |
| P9 | `systemctl cat` as an unprivileged user with a 0600 drop-in | Exit 1; the file's header is printed, its content is not; stderr names the file | Contents are read unprivileged, so root-only files never appear; the gap is reported |
| P10 | `show` on an unloaded unit | Answers `LoadState=loaded`; the unit is not in `list-units --all` before or after | Detail reports that systemd loaded the definition only to answer |
| P11 | Template `show` | Fails ("neither a valid invocation ID nor unit name") | Templates are described from `list-unit-files` and `cat` only |
| P12 | `list-timers`/`list-sockets --output=json` | Supported; fields `next,left,last,passed,unit,activates` (µs, may be null) and `listen,unit,activates`; `activates` can be null | Typed parsers tolerate null |
| P13 | `TimersMonotonic=` in `show` | Printed once **per** trigger (repeated key) | A separate multi-value parser; `ParseShow` (which refuses repeated keys) is not reused |

## 2. New operations (ADR 0008 registry)
| Op | Scope | Privilege | Args | Notes |
|---|---|---|---|---|
| `services.timers.list` | `services.read` | unprivileged-exec | none | `list-timers --all --output=json` + batched `show` of fixed timer properties |
| `services.sockets.list` | `services.read` | unprivileged-exec | none | `list-sockets --all --output=json` |
| `services.unit.detail` | `services.read` | unprivileged-exec | `{unit}` | Inventory membership first (`list-units`/`list-unit-files -- <name>`), then a fixed, allow-listed property list; no `Environment=`, `Exec*` command lines or credentials are ever requested |
| `services.unit.contents` | `services.logs` | unprivileged-exec | `{unit}` | `systemctl cat` as `homelab-control`; redacted; 64 KiB / 2,000 lines (named `contents` because the registry test forbids op names ending in `file`) |
| `services.enablement.plan` | `services.configure` | root-read | `{unit, action}` | Read-only; mints the plan token |
| `services.enablement.apply` | `services.configure` | root-exec | `{unit, action, token}` | The only op that receives the root handle for the new templates |

All take strictly decoded typed arguments, are validated in `hc-api` and again in the broker, and are
audited with validated arguments only.

## 3. ADR 0011 review of the new write: boot-time enable/disable

### 3.1 Typed plan/apply and fixed templates (ADR 0008 Amendment 3)
Four new root templates, each one `.service` target after `--`, validated by the runner's own grammar:
- `systemctl.enable`: `systemctl --no-ask-password --no-reload enable -- <unit>`
- `systemctl.disable`: `systemctl --no-ask-password --no-reload disable -- <unit>`
- `systemctl.is-enabled` (read-only): `systemctl is-enabled -- <unit>`
- `systemctl.show-install` (read-only): `systemctl show --no-pager --property=<fixed list> -- <unit>`
  (Id, Names, LoadState, FragmentPath, SourcePath, DropInPaths, Transient, NeedDaemonReload, UnitFilePreset)

There is no `--now`, `--runtime`, `--force`, `--root`, `mask`, `reenable`, `preset`, `link`, `edit` or
`daemon-reload` template. The M2-2 templates and their property list are unchanged.

### 3.2 Authorization in API and broker
New scope `services.configure` (D-7 level `configure`), in `KnownScopes`, **not** in `DefaultScopes`
(test). `hc-api` checks it on both routes; the broker re-checks it. The token op name is
`services.enablement`, distinct from M2-2's `services.unit`, so a start/stop token can never authorize an
enablement change or vice versa.

### 3.3 Confirmation, risk class and step-up
- Both actions are classified **`disruptive`**: the change persists across reboots and can stop a service
  from coming up at the next boot (or start one that conflicts). The roadmap row said "persistent →
  `reversible`, still confirmed"; this is a deliberate, stricter deviation recorded for owner review, chosen
  so that the existing `Consume` rule (step-up for non-reversible classes) applies unchanged.
- `hc-api` refuses apply without a step-up in the 5-minute window before calling the broker; the broker
  checks `step_up_at` again inside `Consume`.
- The preview states that boot enablement differs from runtime start/stop, that the running state is not
  changed, that systemd is not reloaded and the change takes effect at the next boot, the links that will be
  created or removed, and that there is no automatic rollback (undo = the opposite action, reviewed again).

### 3.4 Supported cases; everything else is refused
Accepted only when all hold (checked at plan **and** again at apply with root reads):
- canonical ID is a non-template, non-instance `.service` (`servicepolicy.ValidUnit`, no `@`), `LoadState=loaded`;
- the requested name and every name systemd knows the unit by (ID, `Names`, fragment basename) and every
  install `Alias=` are not protected; no `WantedBy=` target is protected or a shutdown/reboot/recovery unit;
- **enable:** no `Alias=` names a unit systemd already knows (`is-enabled` must say `not-found`, `show` must say
  `not-found` or this unit): otherwise the new alias link would replace that unit's definition at the next reload
  (measured, `probes/probe4-alias-collision.out`); no file that is not a symbolic link sits at any link path;
- **disable:** no protected unit declares `Requisite=` on it (systemd's `RequisiteOf`);
- fresh `is-enabled` is exactly `disabled` (for enable) or `enabled` (for disable). Refused: `static`,
  `indirect`, `generated`, `transient`, `masked`, `masked-runtime`, `linked`, `linked-runtime`,
  `enabled-runtime`, `alias`, `bad`, `not-found` and anything unknown;
- `SourcePath=` is empty (no SysV script), `Transient=no`, and the fragment lies in `/etc/systemd/system`,
  `/usr/lib/systemd/system`, `/lib/systemd/system` or `/usr/local/lib/systemd/system` (no generator or
  runtime units);
- the fragment and every drop-in parse (§3.5) and the combined `[Install]` section has no `RequiredBy=`,
  `UpheldBy=`, `Also=` or `DefaultInstance=`, no specifiers (`%`), no empty (reset) assignments, no line
  continuations and no unknown keys. `Also=` and `RequiredBy=`/`UpheldBy=` are refused rather than walked:
  they extend the change to other units or make a boot target fail with the unit;
- **enable only:** the boot-time start of the unit passes the M2-2 start checks (no shutdown/reboot/recovery
  units pulled in or triggered, no reboot/power-off `*Action=`, no `isolate` triggers, no protected
  `Conflicts=`/`ConflictedBy=` names, no uninspectable `.service` dependency), using the same bounded
  dependency walk (96 units). Running units it conflicts with are listed.

### 3.5 Reading `[Install]` as root (disclosure and path safety)
`[Install]` is not available from systemd's API, and drop-ins may be root-only (0600) and contain secrets.
The broker therefore reads the fragment and the drop-ins **in-process as root**, and:
- accepts only absolute, clean paths below the allow-listed unit directories; no `..`; every path component
  is `lstat`ed and must not be a symlink, except a top-level `/lib` that is the merged-usr link to `usr/lib`;
  the file is opened `O_NOFOLLOW|O_NONBLOCK`, must be a regular file and ≤ 64 KiB; a fragment must end in
  `.service` and a drop-in in `.conf`;
- extracts only `[Install]` directives whose values are unit names; the content never leaves the broker and
  the token digest covers the paths and the parsed directives, not a hash of the file (no content oracle);
- at plan the paths come from the unprivileged `show-install`; a compromised `hc-api` that tampers with that
  output can at most point the reader at another `.service`/`.conf` file inside the unit directories, and
  apply repeats everything with the root template.

### 3.6 Tokens, locks, revalidation, journal
- Token: bound to actor, session, op `services.enablement`, action, canonical target, risk and a state digest
  = M2-2 unit digest + install digest (show-install properties, fresh `is-enabled`, parsed directives) +
  boot-walk digest. 120 s, single use, broker-minted (unchanged framework).
- Locks: `service:<id>` (shared with start/stop, so the two kinds never overlap on one unit) **and** the
  global `unit-files` lock (enablement changes are serialized host-wide). Plan reports `busy` if either is held.
- Apply: consume token (step-up inside) → take both locks → revalidate everything with root reads (show,
  show-install, is-enabled, `[Install]` re-parse, boot walk) → `conflict` on any difference → intent (fsync)
  → root template → independent read-back → outcome (fsync).

### 3.7 Independent read-back and honest outcomes
Read-back does not trust `systemctl`'s exit status or output:
1. root `is-enabled` must report `enabled` / `disabled`;
2. the broker `lstat`s every expected link itself: `/etc/systemd/system/<target>.wants/<unit>` for each
   `WantedBy=` and `/etc/systemd/system/<alias>` for each `Alias=` must all exist as symlinks after enable and
   all be absent after disable.

Outcomes: `succeeded` only if both agree with the request and `systemctl` exited 0. `failed` if nothing
changed or `systemctl` failed; when some links changed and others did not, the message says the change is
**partial** and lists what was observed. `uncertain` when the state cannot be read back or `systemctl` timed
out. The links `systemctl` reports ("Created symlink …", "Removed …") are recorded as reported changes, next
to the observed ones. Interrupted operations are recorded at broker start with `is-enabled` as observed then
(the journal inspector now receives the whole record, so enablement is not described by run state).

### 3.8 Protected-unit enforcement for persistent changes
Extends D-5 to enablement: the same fixed deny-list, applied to the canonical ID, aliases, unit file, the
`[Install]` aliases, the `WantedBy=` targets, and (enable) the units the boot-time start touches. SSH, the
broker and API units, D-Bus, systemd, network, firewall, remote access, display managers, storage and power
units can therefore not be enabled or disabled from the UI, by any name.

### 3.9 Residual risks (accepted for review)
- **Compromised `hc-api`** can still request any enable/disable the broker policy allows (ADR 0011 limit).
- **No manager reload:** until the next boot or a reload by someone else, `systemctl show`/`status` on the
  host show the old enablement, and the running manager does not know the new `Wants=` links. This is the
  price of not running a global reload. The UI reads enablement from disk and says so.
- **Boot-time conditions** differ from the plan-time walk (other units' states at boot are not modelled);
  the walk is conservative for protected names and dangerous targets, not a boot simulation.
- **Not protected by name:** services that are harmful to disable but not listed (reverse proxy,
  hypervisor). `services.configure` is never granted by default.
- **Concurrent edits** by root on the host between revalidation and the template are not prevented; the
  read-back reports what was observed.
- **Requisite= dependents:** disabling a unit is refused when a protected unit declares `Requisite=` on it,
  but only for dependents systemd has loaded (the relation comes from the loaded graph).

## 4. Read-only views
- **Unit detail** never invents state: it reports `requested` vs canonical ID (alias), whether the unit was in
  the runtime list or loaded only to answer the query (P10), templates (no runtime state; P11), `not-found`,
  `static`, `generated`, `transient`, `masked`. Last exit is shown only when `ExecMainExitTimestamp` is set;
  `NRestarts` only for services. Dependencies come from systemd, capped at 64 names per list.
- **Unit-file contents** (P9) are read by `systemctl cat` running as `homelab-control`, so root-only files are
  never shown (the response lists them as unreadable). Redacted before leaving the broker: every `Environment=` value (variable names are kept); the whole value of `SetCredential*=` and of any key containing `passw`, `passphrase`, `secret`, `token`, `apikey`, `api_key`, `privatekey`, `private_key` or `credential`; in `Exec*=` lines, the value of arguments whose name contains `pass`, `secret`, `token`, `apikey`, `auth` or `key` (`--password=…`, `--token …`, `api_key=…`); and the user-info part of any URL (`scheme://user:pass@`). `EnvironmentFile=` paths are shown, never the file. Bounded to 64 KiB and 2,000 lines, control characters
  stripped. Gated by `services.logs` (the existing non-default scope for sensitive content; a dedicated scope
  is an open owner item O-2) and audited fail-closed (who, which unit, never content). Redaction is
  heuristic; this is stated in the UI.
- **Timers and sockets** are listed with systemd's own fields; unknown or null values are shown as unknown.

## 5. D-10: minimal module registry (proposed implementation, for owner review)
D-10 remains **open**. This implements its recommended option (built-in, compiled-in modules) minimally:
- `internal/modules` (no privileged code; imported by `hc-api` and `hc-broker`): one module, `services`,
  declaring its title, scopes, API route prefix `/api/v1/services` and broker op prefix `services.`.
- **Setting:** `/etc/homelab-control/modules.json` (`{"version":1,"disabled":["services"]}`), root-owned,
  not group/world-writable, not a symlink, strictly decoded. Missing file = defaults (all enabled). Any
  invalid file **disables every optional module** (fail closed) and is logged. The design note said "owner
  setting in `state.db`"; that database belongs to `hc-api`, which the broker must not trust, so the setting
  is a root-owned file that both processes read at start. There is no API or UI to change it (that would
  be an unauthorized configuration write); change it as root and restart both units.
- **Enforcement:** `hc-api` answers every route under a disabled module's prefix with the same 404 body as
  an unknown route (before authentication, so it is indistinguishable); capabilities omit the module's
  entries and list the module as disabled; the UI hides its navigation. The broker refuses its ops with
  `module_disabled` (audited) and omits them from `broker.info`. Operation-journal recovery still runs.
- Not included: runtime loading, third-party modules, per-module packages, an install flow, or a plugin SDK.

## 6. Tests required before owner review
Registry/template snapshots (W1, W9); authz in API and broker; CSRF/Origin; strict decode and injection
tables; fuzz of the new decoders; token expiry, replay, wrong op/action/target/actor/session; stale plan
(state, `[Install]`, `is-enabled`, walk); concurrency on both locks; journal intent/outcome and interrupted
recovery; protected-unit tables for every enablement path; module-disabled 404 and broker refusal; real
systemd in disposable Ubuntu and Fedora containers with cleanup invariants (SSH/broker `is-enabled` and
`/etc/systemd/system` symlink snapshot unchanged); real-backend browser flow with keyboard and phone layout;
E4 comparison. Results: `M2-3-ACCEPTANCE.md`.
