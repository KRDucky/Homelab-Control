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

### 1.1 Probe 7: review of PR #36 (systemd 259.9, 249.11 and Fedora 259.10)
Plain systemd, no Homelab Control installed; `docs/evidence/m2/m2-3/probes/probe7*`. The container is
restarted between phases, so "after reboot" is a real fresh boot of PID 1 (not a VM boot).

| # | Question | Observation (all three unless noted) | Design consequence |
|---|---|---|---|
| P14 | New `<unit>.d` drop-in or new `/etc` fragment written while the unit is **loaded** (running), no reload | `systemctl show` still lists the old `FragmentPath`/`DropInPaths`; `systemctl --no-reload enable` honours the files on disk and created a link to a target the loaded view does not mention. `NeedDaemonReload` was `yes` on 259 but **`no` on 249** | The broker resolves the fragment and `<id>.d/*.conf` along systemctl's search path itself and refuses when they differ from the loaded view (§3.4). `NeedDaemonReload` alone is not a usable gate |
| P15 | Same for an **unloaded** unit | `show` loads it from disk and is accurate | (explains why the first container round did not show P14) |
| P16 | Prefix (`hcp-.service.d`) and type-level (`service.d`) drop-ins | PID 1 lists them in `DropInPaths`; `systemctl enable` **ignores** their `[Install]` (Fedora ships `service.d/10-timeout-abort.conf`) | `[Install]` is read from `<id>.d` only; a shared drop-in that has an `[Install]` section is refused as ambiguous; its `[Unit]` hazards are still checked |
| P17 | What does `disable` remove? | **Every** symbolic link under `/etc/systemd/system`, at any depth, whose name or target names the unit: links in other targets' `.wants/`, in another unit's `.requires/`, a renamed link, a manual alias and `deep/er/<unit>`, none of them described by `[Install]` | The broker scans the tree (bounded) and refuses enable/disable when any link to the unit is not described by `[Install]`; runtime links are refused; read-back is a fresh scan (§3.7) |
| P18 | `WantedBy=sysinit.target`/`basic.target`/`local-fs.target`/`sockets.target` with default dependencies, then reboot | No ordering cycle and no deleted job in the journal; the units start after `basic.target` (systemd does not add the implicit target `After=` that would close the loop) | Not refused; recorded. Early-boot ordering is not an M2-3 defect |
| P19 | `--no-reload enable`: when does it take effect? | Immediately on disk (`is-enabled` = enabled); the running manager's `multi-user.target` `Wants=` does not contain the unit. Starting a **target that was not loaded yet** started the newly wanted unit at once, without a reload. After any `daemon-reload`, `systemctl start multi-user.target` started it. After reboot it started | "Takes effect at the next boot" was wrong. The plan and UI now say: written to disk now; used at the next boot **or** as soon as systemd reloads its configuration for another reason (package installs/updates commonly do) or first loads a target; so it can take effect before a reboot |
| P20 | `--no-reload disable` of a running, enabled unit | Link removed, unit keeps running; the running `multi-user.target` still `Wants=` it until a reload; not started after reboot | Disable consequences say the running manager still treats the unit as wanted until a reload or reboot |

### 1.2 Probe 8: unreloaded [Unit] changes (second review round; 249.11, 255.4, 259.9, Fedora 259.10)
`docs/evidence/m2/m2-3/probes/probe8*`. Each fixture was loaded (running) before the edit, then enabled with
`systemctl --no-reload enable`, then the container restarted (fresh PID 1). The first run combined the
changes in one boot (`probe8-unreloaded-*.out`, `hcp-prot` stopped on all four, cause not isolated); the
isolated runs `probe8-g{1,2,3,4}-*.out` make one change per boot. `hcp-prot.service` stands in for
a protected service.

| # | Change written without a reload | Loaded view (`systemctl show`) | At the next boot (all four versions) |
|---|---|---|---|
| P21 | New prefix drop-in `hcp-a-.service.d/` with `Conflicts=hcp-prot.service` | `Conflicts=` and `DropInPaths=` lack it; `NeedDaemonReload=no` on 249 and 255, `yes` on 259 | `hcp-prot` **stopped** (all four, isolated run g1) |
| P22 | The enabled unit's own fragment edited to `Wants=` a unit that conflicts with `hcp-prot` | `Wants=` empty; `NeedDaemonReload=yes` | `hcp-prot` **stopped** (all four, g2) |
| P23 | `hcp-prot`'s own new drop-in `Conflicts=` the enabled unit | the enabled unit's `ConflictedBy=` empty; `NeedDaemonReload=no` on 249 | `hcp-prot` stayed active; the **enabled unit** did not start (all four, g3) |
| P23b | **Another** unit edited: the enabled unit `Wants=hcp-dep` (loaded), `hcp-dep`'s fragment gains `Conflicts=hcp-prot.service` | `hcp-dep` `Conflicts=` lacks it | `hcp-prot` **stopped** (all four, g4) |
| P24 | Manager timestamps | `UnitsLoadStartTimestamp` is set at boot only (unchanged by `daemon-reload`, all versions). `UnitsLoadTimestamp` (≥ 255) is set at the **start** of every reload, before units load; 249 has no such property. `/run/systemd/generator` is recreated (new change time) at the start of every boot and reload, before units load, on all versions | The broker's "last load" threshold (§3.4) |
| P26 | Fresh install, **no harness reload** (probe 9): boot, install the package (its scripts run `daemon-reload`), `systemctl enable --now` the broker socket | Unit files and links are written 0.4–100 ms before their own reload; `/run/systemd/system` is created during boot (after the load on 249) and stays empty | With the first rule (100 ms margin) all of these counted as unloaded, so enable would have been refused on every new host until a reboot. With the exact rule (§3.4) nothing counts, on all four versions (`probe9-fresh-install-*.out`; first rule: `probe9-margin100ms-*.out`) |
| P25 | Files PID 1 writes itself | `/run/systemd/transient/*` (systemd-run) and `/etc/systemd/system.control/*` (set-property) change after the last load but are applied by PID 1 at once; generator output is rewritten at each reload | Excluded from the change scan |

Conclusion: both gaps allowed an enablement whose boot-time activation affects a protected service (P21,
P22), including through a change to **another** unit's file (P23b), which a review of the unit's own files
can never see. P23 shows a case where the protected unit wins instead; it is refused all the same (the
outcome depends on the configuration). Reproduced
through the product with the old build `82c24a74` (`docs/evidence/m2/m2-3/review-3bec1294/repro-old-82c24a74-ubuntu-2604.log`):
all three unloaded changes were accepted for a loaded fixture, an edit between preview and apply was
**enabled**, and a disable was accepted although a protected-name unit's unloaded drop-in declared
`Requisite=` on it.

### 1.3 Probe 10: disabling a unit that pulls in a protected unit (B-1; 249.11, 255.4, 259.9, Fedora 259.10)
`docs/evidence/m2/m2-3/probes/probe10-*.out`. `hcp-vpn.service` stands in for a remote-access unit and has no
`[Install]` section (it starts only when pulled in). The puller `hcp-b1.service` (`WantedBy=multi-user.target`)
is enabled, then disabled with `--no-reload`, then the container restarts (fresh PID 1). Identical on all four:

| Case | `hcp-vpn` after the reboot |
|---|---|
| control: puller left enabled | active |
| direct: puller `Wants=hcp-vpn.service` | **inactive** |
| transitive: puller `Wants=` a helper that `Requires=hcp-vpn.service` | **inactive** |
| alias: puller `Wants=` an alias name (symlink) of `hcp-vpn.service` | **inactive** |
| anchored: a second enabled unit also `Wants=hcp-vpn.service` | active |
| socket: `hcp-vpn.socket` enabled (`Service=hcp-vpn.service`) | service inactive, socket listening (starts on demand) |

The `exit 1` recorded for the non-socket cases comes from the script's last line (a `[ … = socket ] &&` test),
not from a failure. Through the product (real systemd, Ubuntu 26.04 container, the new scenario against the build before the
correction, `63979c1b`; `docs/evidence/m2/m2-3/review-ad725d43/repro-old-63979c1b-ubuntu-2604.log`): all three
disables (direct, transitive, alias) were **accepted** (200), and when the other puller went away between
preview and apply the unit was **disabled**; the disable-side change gate was absent (6 checks failed as
expected).

### 1.4 Probe 11: netplan's runtime unit configuration (B-2; Ubuntu 24.04, systemd 255.4, netplan.io 1.1.2)
`docs/evidence/m2/m2-3/probes/probe11-netplan-runtime-ubuntu-2404-netplan.out`, disposable image
`dev/disposable/images/ubuntu-2404-netplan.Dockerfile` (like the GitHub `ubuntu-24.04` runner image 20261025.353:
netplan with the networkd renderer and a cloud-init style `/etc/netplan/50-cloud-init.yaml`; its own network
namespace, the workstation's networking untouched). netplan's systemd generator writes, at boot **and at every
reload** (after `UnitsLoadTimestamp`, during the generator phase):
- the runtime unit `/run/systemd/system/netplan-ovs-cleanup.service` (`UnitFileState=enabled-runtime`),
- its link `/run/systemd/system/systemd-networkd.service.wants/netplan-ovs-cleanup.service`,
- the drop-in `/run/systemd/system/systemd-networkd-wait-online.service.d/10-netplan.conf` (a condition and an
  `ExecStart=` override; no dependency or safety-relevant setting).
These are exactly the three entries the CI runner reported. Through the product, `d9946a4a` in this image fails
like CI (`m2-3-enablement: 72 passed, 54 failed`; `docs/evidence/m2/m2-3/review-00fb7f2b/repro/`). Because the
entries are rewritten at every reload, judging runtime configuration by change time cannot work either.

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
- **the files on disk are the ones systemd has loaded** (P14): the broker finds the fragment (first match along
  systemctl's search path) and the `<id>.d/*.conf` drop-ins itself and compares them with `FragmentPath` and the
  `<id>.d` entries of `DropInPaths` (merged-usr `/lib` = `/usr/lib`); any difference is refused ("review after the
  next boot or after systemd reloads its configuration"). Shared (prefix/type-level) drop-ins with an `[Install]`
  section are refused (P16);
- **no link to the unit that `[Install]` does not describe** (P17): every symbolic link under
  `/etc/systemd/system` (bounded: 50,000 entries, depth 8) whose name or target names the unit or an install
  alias, or whose target is the fragment, must be one of the planned links and point to the fragment; any link in
  `/run/systemd/system` is refused (runtime enablement);
- **enable and disable (disable since `ad725d43`) — nothing changed on disk since systemd last loaded its unit files** (P21–P26, second review
  round): every entry (file, link, directory) under `/etc/systemd/system`, `/etc/systemd/system.attached`,
  `/usr/local/lib/systemd/system`, `/usr/lib/systemd/system` (and a split-usr `/lib/systemd/system`) must have
  a change time (ctime, which cannot be set from user space) **before the last-load threshold**, without a
  margin. The threshold is the earliest of: the change time of `/run/systemd/generator` (recreated by PID 1
  before units are read at every boot and reload, P24, with the same coarse kernel clock as every other file
  time, so any change made after the units were read has a change time at or after it); on systemd ≥ 255 the
  `UnitsLoadTimestamp` stamp (or the boot stamp before any reload) and its monotonic form converted with
  `/proc/uptime` (so a realtime clock step after the load cannot move the threshold later). On 249 only the
  generator directory exists as a reload marker. Files and links written just before their own reload
  (package scripts, `systemctl enable`) are therefore not counted (P26). Runtime unit configuration: any entry
  in `/run/systemd/system` or `/run/systemd/system.attached` refuses enable (loaded now, gone at the next boot;
  the directories themselves are created during boot and are not judged by change time). Not scanned: PID 1's
  own `system.control`, `transient` and generator directories (P25). Changes the operation journal attributes
  to Homelab Control's own completed enablement changes are not counted: the outcome record lists the
  **planned** link paths and the start/finish times, and only those paths and their directories, with a change
  time inside that window (± 20 ms for the coarse clock), are excused. This is safe because those links are
  `.wants`/alias symlinks to a unit file that was reviewed, `RequiredBy=`/`UpheldBy=` are refused (own links
  never add such dependencies), and they change no unit's settings; links systemctl created beyond the plan
  and interrupted operations are never excused. Limit: separately reviewed own changes are not re-evaluated
  together. If the threshold cannot be read, enable is refused. Bounded: 100,000 entries, depth 8;
- **runtime unit configuration** (B-2, §1.4; owner-approved approach P-8 option 3): nothing under `/run` (runtime
  unit files, drop-ins, `.wants`/`.requires`/`.upholds` links, aliases, masks, generator output, transient
  units, `system.control`) is assumed to exist at the next boot, and only configuration of units the review
  reaches is evaluated — unrelated runtime entries do not matter:
  - *disable:* the anchoring graph R (below) counts only relations backed by persistent unit files and
    drop-ins (`Requires=`/`Wants=`/`BindsTo=`/`Upholds=`), persistent links, activation by persistent
    socket/timer/path units, and names that resolve at boot (a persistent alias link or unit file); a unit
    whose fragment is runtime, or whose persistent files are uncertain (specifiers, resets, unreadable),
    anchors nothing. Units the disabled unit reaches that are masked only until the next boot, or whose runtime
    unit file shadows a persistent one, are refused (their boot-time relations are unknown);
  - *enable:* every unit the start walk evaluated must not be masked only until the next boot, have a runtime
    unit file shadowing a persistent one, or be reached through a runtime alias over a persistent unit name;
    for units with runtime drop-ins the safety-relevant settings (`FailureAction=`, `SuccessAction=`,
    `StartLimitAction=`, `JobTimeoutAction=`, `OnFailureJobMode=`, `OnSuccessJobMode=`) are re-derived from the
    persistent files and must be safe; a unit masked only until the next boot whose persistent file has
    `Conflicts=` with an evaluated unit is refused;
  - bounds: 4,096 top-level runtime entries, 1,024 entries per link directory, 64 KiB per file; beyond them, or
    on unreadable files where it matters, the operation is refused;
- **disable only — no protected unit started at boot only through it** (B-1, §1.3; safety objective approved
  by the owner 2026-11-05): the broker walks the loaded graph from `default.target` (resolved to its canonical
  ID) through `Requires=`, `Wants=`, `BindsTo=`, `Upholds=` and `Triggers=` (socket, timer and path
  activation), with the unit removed as if it never started (set R), then the units the unit pulls in through
  the same relations that are not in R (set C, walked transitively, aliases resolved by systemd to canonical
  IDs). Refused when a unit in C is protected by its canonical ID or any of its names, when C contains a
  `.service` that cannot be inspected, when the boot target cannot be resolved, or when either walk exceeds
  4,000 units. The units in C (none protected) are listed in the plan and covered by the token digest, so a
  change of the graph between preview and apply invalidates the plan. The change gate below applies to
  disable as well, so the loaded graph equals the files on disk — except for Homelab Control's own
  `--no-reload` changes, which the gate excuses: the walk therefore applies an **overlay** of those operations'
  planned link paths, judged by each link's state on disk now (present: the target wants the unit; absent: it
  does not; an absent own alias link makes the name unresolvable). Own links are only `<target>.wants/<unit>`
  and aliases (`RequiredBy=`/`UpheldBy=` are refused). Removing an edge that the target also declares in its own
  file (`Wants=` in `[Unit]`) can only cause more refusals;
- **disable only:** no unit file on disk (in any unit directory, loaded or not, including drop-ins) declares
  `Requisite=` on the unit or an alias when that file belongs to a protected unit or to a shared drop-in
  directory (whose units cannot be named); continued values are joined; values with specifiers or quotes
  count as a match;
- **enable only:** `Conflicts=` and `FailureAction=`/`SuccessAction=`/`StartLimitAction=`/`JobTimeoutAction=` read
  from the files on disk (the next boot uses them, not the loaded copy) must not name a protected unit or a
  reboot/power-off action; a continued (`\`) value of these keys is refused;
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
   `WantedBy=` and `/etc/systemd/system/<alias>` for each `Alias=` must all exist as symlinks **pointing to the
   unit file** after enable and all be absent after disable;
3. a fresh scan of `/etc/systemd/system` (as in §3.4) must find no other link to the unit; any is reported as
   `unexpected_links` and the outcome is not `succeeded`.

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
- **No manager reload (P19, P20):** the setting is on disk at once; the running manager does not know the new
  `Wants=` links (or still has the removed ones) until it reloads. Any later `daemon-reload` (package installs
  and updates commonly run one), or the first load of a target that wants the unit, applies it before a
  reboot; starting such a target can then start (or, after disable, still start) the unit. Until then
  `systemctl show`/`status` on the host show the old setting. The UI reads enablement from disk and says so.
- **Unit-file changes without a reload (second review round):** closed for enable by the change gate (fail
  closed; §3.4); for disable by the on-disk `Requisite=` scan. Remaining races and limits:
  - **Race window:** a change made after the apply-time revalidation and before `systemctl` runs (milliseconds,
    under both locks) is not seen; a change made inside an own operation's ±20 ms window *to one of that
    operation's planned link paths or their directories* is excused.
  - **Coarse clock:** on kernels with a 10 ms tick (HZ=100) and without fine-grained file times, a change made
    within a few milliseconds after a reload's generators finished, and after its file was read, can carry a
    change time just below a `UnitsLoadTimestamp`-based threshold; on 249 (generator directory only) this
    cannot happen. After a suspend, the monotonic form makes the threshold earlier (more refusals, never fewer). Changes made afterwards are root's own boot-time
    configuration, reported by nothing.
  - **Clock:** change times and thresholds use the realtime clock. Steps after the last load are covered on
    255+ by the monotonic form; on 249 (generator-directory time, no monotonic form) a backward step after a
    reload can hide a change made during the stepped interval.
  - **249 heuristic:** systemd 249 records no reload time; the generator directory's change time is used
    (measured, P24). Writing into `/run/systemd/generator` needs root.
  - **Usability:** after any change on disk not loaded by systemd — including an administrator's
    `systemctl --no-reload enable`, a package that installs a unit without reloading, any runtime unit
    configuration, or an interrupted Homelab Control operation — enable is refused until the next reload or
    boot. Disable (undo) stays available. A fresh install followed by the documented first steps is not
    affected (P26). Not measured: cloud-init/netplan on real cloud-image boots (KVM procedure step). Proposal
    P-6 in `OWNER-DECISIONS.md`.
  - **Disable boot-pull check (B-1) — limits:** it models the boot as the start of `default.target`; a
    kernel command line `systemd.unit=` or `emergency`/`rescue` boot is not modelled. D-Bus activation is not
    an edge, so a protected unit started only on D-Bus demand and pulled in by the disabled unit is refused
    (over-refusal, never under). Units the disabled unit still pulls in because something else starts *it*
    are treated as no longer started (more refusals). `Requisite=`-only relations do not start anything and
    are covered by the separate `Requisite=` checks. Over-refusal, **measured** with the plan-only survey
    (`docs/evidence/m2/m2-3/review-*/survey/`): on all four container images every enabled, unprotected
    service was allowed (protected names refused as before); but a service with `Wants=network-online.target`
    that is the only puller of that target is refused once a wait-online unit is enabled
    ("network.target is started at boot only through it"; in the containers nothing else starts
    `network.target`, on a real host the network service usually does, while the wait-online unit itself is
    protected by name). Harmless cases of this kind are refused; the rule is not loosened (owner decision).
    Real cloud images are measured by the KVM procedure (Phase A, survey). The check uses systemd's loaded graph; it is only
    valid because the change gate refuses while unit files changed on disk after the last load.
  - **Disable:** units started at boot by other means (`Requires=`, `Wants=`, activation) are unchanged by
    disable and are not reviewed; `Requisite=` from a unit that is protected only by an alias name is not
    matched by name (aliases of the requiring unit are not resolved).
  - Settings outside unit files (`/etc/systemd/system.conf` and its drop-ins, kernel command line, presets)
    are not reviewed.
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
  never shown (the response lists them as unreadable). Redacted before leaving the broker: every `Environment=` value (variable names are kept); the whole value of `SetCredential*=` and of any key containing `passw`, `passphrase`, `secret`, `token`, `apikey`, `api_key`, `privatekey`, `private_key` or `credential`; in `Exec*=` lines, the value of arguments whose name contains `pass`, `secret`, `token`, `apikey`, `auth` or `key` (`--password=…`, `--token …`, `api_key=…`); and the user-info part of any URL (`scheme://user:pass@`). `EnvironmentFile=` paths are shown, never the file. Redaction works on logical lines joined across `\` continuations (a redacted multi-line value is shown as one line). Bounded to 64 KiB and 2,000 lines, control characters
  stripped. Gated by `services.logs` (the existing non-default scope for sensitive content; a dedicated scope
  is an open owner item O-2) and audited fail-closed (who, which unit, never content). Redaction is
  heuristic and **not a secrecy guarantee**; this is stated in the UI. Known gaps (review, §7): positional secrets
  (`-u user:pass`, `mysql -pSECRET`, a bare token argument), quoted header values (`"Authorization: Bearer …"`),
  secrets in URL query strings, and keys whose names do not contain the listed words. See O-2.
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

## 7. Review of PR #36 (2026-11-04, after CI run 41793837318)
Requested by the owner as a review and corrective-development assignment (not acceptance). Findings on
product commit `5a14c727`, fixed in `82c24a74` unless marked otherwise:

| # | Finding | Severity | Evidence | Correction |
|---|---|---|---|---|
| R1 | Review used systemd's loaded view of the unit files; for a loaded unit a new `<id>.d` drop-in or `/etc` fragment written without a reload was honoured by `enable` but not reviewed (and not caught by apply-time revalidation) | High (unreviewed links, possibly to a protected target) | P14, `probe7b-loaded-*.out` | On-disk resolution, refusal on mismatch, digest over the on-disk files (§3.4); `TestEnablementRefusesStaleLoadedView`, `TestEnablementDropInAfterPlanIsRefusedAtApply`; container check "new drop-in not loaded by systemd" |
| R2 | `disable` removes links its `[Install]` does not describe (other targets, another unit's `.requires/`, manual aliases, any depth); the plan did not show them and read-back did not see them | High (affected links not reviewed) | P17 | Bounded scan, refusal, runtime links refused, link targets checked (§3.4); `TestDisableRefusesUndescribedLinks`; container check "disable with a link [Install] does not describe" |
| R3 | Read-back checked only that a link existed, not where it pointed, and did not look for other links | Medium | — | Target check and fresh scan, `unexpected_links` (§3.7); `TestEnablementReadBackScansLinks` |
| R4 | `[Install]` from prefix/type-level drop-ins was treated as honoured | Low (would produce a false `failed`) | P16 | `<id>.d` only; ambiguous shared `[Install]` refused; `TestEnablementSharedDropIns` |
| R5 | `Conflicts=`/`*Action=` edits on disk not yet loaded were not checked for the next boot | Medium | P14 | Read from disk for enable (§3.4); `TestEnablementDiskUnitHazards` |
| R6 | Unit-file contents: a secret on a backslash continuation line was shown unredacted | Medium (disclosure to `services.logs` holders) | `TestParseCatRedactsContinuations` | Logical-line redaction (§4). Other heuristic gaps remain (§4, O-2) |
| R7 | Plan and UI promised "takes effect at the next boot" | Medium (misleading consequence) | P19, P20 | Wording corrected in the broker, UI and docs |
| R8 | Interrupted-operation recovery described only the current `is-enabled` | Low | — | Describes before/requested/observed and the links named after the unit, "not verified"; `TestEnablementRecoveryDescribesObservedState`; real broker kill, `m2_3_interrupt.py` |
| R9 | Tests mirrored the implementation: the fake controller created exactly the links the review computed, so R1–R3 could not fail | Test quality | — | The fake now models measured `systemctl` behaviour independently (`systemctlModel`); every new check was removed once to confirm a test fails |
| R10 | `WantedBy=` early-boot targets suspected of creating ordering cycles | Not confirmed | P18 | None |
| R11 | Harness: `docker-systemd.sh` ran only the base smoke checks unless `HC_SMOKE_EXTRA_*` was passed | Harness | first rerun showed 11 checks | Defaults to the CI set |

Inspected in this round without finding a defect:
- `enablement.go` apply path: token consumed with op, action, target, actor, session and step-up; both locks
  taken before root revalidation; digest compared; intent before and outcome after the template; a panic
  writes an `uncertain` outcome.
- Module-disabled enforcement: `api/server.go` `handle()` answers a disabled module's routes with the same
  404 as unknown routes before authentication and CSRF (`TestDisabledModuleRoutes404` also sends the
  requests without a session; it compares the body only for authenticated requests);
  `broker/server/server.go` refuses with `module_disabled` before the scope check;
  `modules.Load` refuses symlinks, non-regular, group/world-writable and non-root files, and any error
  disables every module. Deleting the file would re-enable modules, but `/etc/homelab-control` is
  `root:homelab-control 0750` in the packages (`hc-api`'s account cannot delete it).
- Nothing outside the Services module calls `services.*` routes (the Audit page only filters by the
  `services.` prefix).
Not re-inspected in this round (unchanged since the first review and covered by its tests): the token
framework (`writes.go`) and the journal format.

## 8. Second review round (2026-11-04): unreloaded changes
The owner asked whether the two gaps reported after §7 (new shared drop-ins without a reload; boot checks
using systemd's loaded state) permit enabling a unit whose eventual activation affects protected services.
**They did** (§1.2, P21–P23; reproduced through the product with `82c24a74`). Fixed in product commit
`3bec1294`, corrected in `63979c1b` (R14):

| # | Finding | Correction | Tests |
|---|---|---|---|
| R12 | New/edited/removed unit files or links not loaded by systemd (own, shared or another unit's) escaped the enable review; edits between preview and apply too | Change gate against the last load (§3.4), fail closed; own journaled changes excused; new fixed root template `systemctl.show-manager` (no target) | `TestEnableRefusedAfterUnloadedChanges`, `TestEnableEditBetweenPreviewAndApply`, `TestEnableGateExcusesOwnJournaledChanges`, `TestEnableGateForeignChangeDuringOwnChange`, `TestEnableGateDoesNotExcuseInterruptedChanges`, `TestEnableGateFailsClosedWithoutLoadTime`, `TestParseManagerShow`, `TestLoadThreshold`; container checks 6d |
| R13 | Disable's `Requisite=` protection used only systemd's loaded `RequisiteOf` | Scan of unit files on disk (§3.4) | `TestDisableRefusesRequisiteOnDisk`; container check "disable refused: unloaded Requisite=" |

| R14 | The first gate rule (100 ms margin) counted files and links written just before their own reload, so a freshly installed host would have refused every enable until a reboot (found by measuring a fresh install without a harness reload, P26) | Exact threshold (generator directory, earliest of the stamps, no margin); runtime configuration refused instead of judged by change time; only planned links excused; Requisite= scanner fails closed on read errors. Product commit `63979c1b` (supersedes `3bec1294`) | `TestLoadThreshold`, `TestEnableRefusedWithRuntimeConfiguration`, `TestEnableGateDoesNotExcuseUnexpectedLinks`; container checks "package pattern" and "runtime unit configuration"; probe 9 |

No daemon-reload was added and the protected-unit policy is unchanged. Each new check was removed once to
confirm that a test fails (including excusing every change inside an own operation's window).

## 10. B-2: runtime unit configuration (2026-11-05)
Root cause: the rule "any runtime unit configuration refuses" (R14, `63979c1b`) assumed runtime configuration is
gone at the next boot and could not be evaluated; on netplan-based Ubuntu hosts netplan's generator always
writes some (§1.4), so every enable and disable was refused. The owner approved P-8 option 3 (narrowly scoped,
fail-closed evaluation; verbatim in `OWNER-DECISIONS.md`). Corrected in product commit `00fb7f2b` (§3.4):

| # | Finding | Correction | Tests |
|---|---|---|---|
| R19 | Blanket refusal on any `/run/systemd/system` entry (netplan hosts unusable) | Runtime configuration evaluated only for reached units; runtime-only relations never anchor; runtime masks/shadowing/aliases on reached units refused; safety settings re-derived from persistent files | `TestNetplanRuntimeEntriesDoNotBlockOrdinaryChanges` (netplan's exact entries), `TestEnableRuntimeConfigurationOfEvaluatedUnits`, `TestEnableRuntimeMaskedConflict`, `TestDisableRuntimeAnchorsDoNotCount` (runtime link, drop-in, alias, runtime unit, unreadable and uncertain anchors refused; persistent anchor allowed), `TestDisableRuntimeConfigurationOfReachedUnits`, `TestRuntimeChangeBetweenPreviewAndApply`, `TestRuntimeEvaluationBounds`; all B-1 tests rebuilt on persistent fixtures; container checks 6f on five images incl. `ubuntu-2404-netplan`; the stock CI runner |

Each new check was removed once and a test failed (R using the loaded graph; no boot-name check; no enable
runtime check; no runtime-mask check on reached units; no persistent safety-setting check; uncertain files
anchoring; no link-directory bound; no shadowing check; no runtime-mask conflict check).

**Can the relevance calculation miss relationships? (review; limits, not a boot simulation)**
- The enable start walk descends only into units that are not running; runtime configuration on the
  dependencies of currently running units is not examined (as for all of the walk, their state at boot is
  assumed to be like now).
- Runtime-only relations are assumed absent at boot, including generator output and netplan's entries, which
  in practice are recreated: this can only cause refusals (a protected unit anchored only through generated or
  runtime configuration is treated as not started), never permit one.
- Implicit dependencies that are not written in files (default dependencies, `Type=dbus` → `dbus.socket`,
  mount and slice dependencies) do not count as anchors: more refusals only.
- Runtime drop-ins: only the emergency actions and trigger job modes are re-derived; other overridden settings
  (`Exec*=`, conditions, `Restart=`, `StopWhenUnneeded=`) are not, as they do not start or stop other units at
  boot in this model.
- Runtime masks: only the masked unit's persistent primary file's `Conflicts=` is compared with the evaluated
  units; what a runtime-masked unit would itself start at boot is not attributed to the change (it happens at
  boot regardless of it).
- D-Bus activation, unit conditions, the kernel command line (`systemd.unit=`), and a runtime unit directory
  changed after the review between apply's revalidation and the template (milliseconds) are not modelled.

## 9. B-1: disable reaching a protected unit at boot (2026-11-05)

**B-2 (open, found by GitHub CI run 41977140563, job `systemd-ubuntu` — the first real, non-container host):**
on the GitHub Ubuntu 24.04 runner VM, netplan places unit configuration in `/run/systemd/system`
(`netplan-ovs-cleanup.service`, `systemd-networkd-wait-online.service.d`, `systemd-networkd.service.wants`).
The rule "any runtime unit configuration refuses" (R14, `63979c1b`) therefore refuses **every enable and
every disable** on that host (`m2-3-enablement: 72 passed, 54 failed`; evidence
`docs/evidence/m2/m2-3/review-d9946a4a/ci/`). The rule's premise — runtime configuration is gone at the next
boot — is false for generator-produced entries, which netplan recreates at every boot; the broker cannot tell
them from manual runtime configuration (`systemctl --runtime`, other tools) by any measured signal. The
containers and the containerised CI job (`systemd-ubuntu-2604`, 127/0) have no such entries, which is why they
pass. The rule is unchanged (fail closed) pending the owner's choice (proposal P-8 in `OWNER-DECISIONS.md`).
Netplan-based Ubuntu installations — most Ubuntu servers and cloud images — are affected.

The owner approved the safety objective of P-7 (verbatim in `OWNER-DECISIONS.md`); the implementation is for
review. Reproduced without Homelab Control (§1.3, probe 10) and through a unit test against the previous
build's logic. Corrected in product commits `ad725d43`, `ae937d8a` (R17) and `d9946a4a` (R18):

| # | Finding | Correction | Tests |
|---|---|---|---|
| R15 | Disable accepted for a unit that, directly, transitively or through an alias, is the only boot-time puller of a protected (for example remote-access) unit | Boot-pull check (§3.4), fail closed; `Triggers` added to the fixed read-only property list | `TestDisableRefusedWhenOnlyItStartsAProtectedUnitAtBoot` (Wants/Requires/BindsTo/Upholds, transitive, alias; links, boot and runtime states unchanged), `TestDisableAllowedWhenTheProtectedUnitIsStartedOtherwise` (anchored, socket-activated, unprotected, default dependencies), `TestDisableBootPullFailsClosed`, `TestDisableBootPullStalePlan` (graph and disk changes between preview and apply); container checks 6e |
| R17 | (found by the real-systemd scenario on `ad725d43`, all four versions) The loaded graph does not contain the broker's own `--no-reload` changes: after it disabled one of two pullers, the last puller's disable would have been allowed although nothing starts the protected unit at the next boot | Own-change link overlay (§3.4), product commit `ae937d8a` | `TestDisableBootPullSeesOwnUnloadedDisables` (fails without either half of the overlay); scenario 6e "last puller after an own unloaded disable"; the failing `ad725d43` round is kept in `docs/evidence/m2/m2-3/review-ad725d43/` |
| R18 | `hc-api`'s broker client waited 15 s for `services.enablement.apply`, whose broker timeout is 55 s (revalidation with the walk, the template, read-back): a slow apply could be reported as an error while it completed | Client deadlines above every broker op timeout (`enablement.apply` 75 s, `enablement.plan` 20 s, 15 s read-only ops 20 s); product commit `d9946a4a` | `TestClientDeadlinesExceedBrokerTimeouts` (guard, all ops); `TestDisableBootPullTimeoutAtApplyFailsClosed`; measured plan durations 85–231 ms in the containers |
| R16 | Disable's checks used the loaded graph while unit files could have changed on disk | The change gate applies to disable too; own journaled changes excused; after an interrupted change disable is refused until a reload or boot (recorded by `m2_3_interrupt.py`) | `TestEnableGateFailsClosedWithoutLoadTime`, `TestDisableBootPullStalePlan`; container checks |

Each of the six checks of R15/R16 was removed or weakened once (no boot-pull check; direct `Wants=` only;
protection by one alias name only; no anchoring; boot-only units outside the digest; gate on enable only) and a
test failed each time. Usability on a freshly installed host: the scenario's ordinary disables (default
dependencies, `WantedBy=multi-user.target`) succeed on all four versions; see the evidence for `ad725d43`.

