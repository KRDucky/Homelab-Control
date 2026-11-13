# ADR-0008: Privileged broker design (M1)

**Status:** Accepted (2026-10-16, owner) **for the four existing read-only, argument-free M1 operations only**; Amendment 1 (2026-10-21, `services.unit.logs`) and Amendment 2 (2026-10-23, `root-exec`, owner decision D-4) extend it (`broker.info`, `legacy.webmin.status`, `hardware.dmi.identity`, `services.systemd.list`). Amendments 3 (M2-3), 4 (M2-6), 5 (M4-1b), and 6 (P0 unused mkfs.ext4 template) are **Proposed**. Adding or changing an operation requires a new review.
> **Owner decision recorded 2026-10-16:** this acceptance is **not** authorization to implement privileged write operations. Any write-capable broker operation needs a separate, explicit owner approval (M2 design: confirmation, locking, audit, rollback).

**Date:** 2026-10-15
**Deciders:** Project owner; security review of every new operation

## Context
ADR 0001 split the system into an unprivileged `hc-api` and a root `hc-broker` exposing a closed list
of typed operations. M1 needs the first read-only operations: systemd unit listing, root-only hardware
identity (DMI serials/UUID) and legacy Webmin configuration (root-only `miniserv.conf`). This ADR fixes
the concrete mechanism, the trust assumptions and what remains out of scope.

Measured privilege needs (Ubuntu 22.04/24.04, systemd 249/255):

| Data | Readable unprivileged? | Where it is collected |
|---|---|---|
| procfs/sysfs overview, PCI/USB/block/hwmon sysfs, `/dev/disk/by-id` | yes | `hc-api` (no broker) |
| DMI `product_serial`, `board_serial`, `chassis_serial`, `product_uuid` | no (root 0400) | broker op, file read |
| `/etc/webmin/miniserv.conf`, `*.acl`, `config` | no (root 0600) | broker op, file read |
| systemd unit list | yes via D-Bus/`systemctl`, but `hc-api` may not execute programs | broker op, **child runs unprivileged** |

## Decision
1. **Transport.** systemd socket activation: `homelab-control-broker.socket` listens on
   `/run/homelab-control/broker.sock`, mode `0660`, `root:homelab-control`. The broker accepts the
   inherited fd (`LISTEN_FDS`), so it never needs `CAP_CHOWN`. A dev/test mode creates the socket itself.
2. **Peer authentication.** Every connection's `SO_PEERCRED` uid must be the `homelab-control` uid or 0
   (root, for `hlctl broker` diagnostics). Anything else is rejected and audited.
3. **Framing.** One request per connection: 4-byte big-endian length + JSON. Request ≤ 16 KiB, response
   ≤ 4 MiB, 5 s read deadline, at most 8 operations in flight.
4. **Request.** `{"v":1,"op":"<name>","args":{…},"actor":{"user":"…","scopes":[…],"request_id":"…"}}`.
   Unknown fields anywhere are rejected. Unknown ops return `unknown_op`.
5. **Operation registry.** Compiled-in table; each entry declares name, required scope, privilege
   (`root-read` = in-process file read as root; `unprivileged-exec` = fixed command run as
   `homelab-control`), timeout, a typed args struct (strict JSON decode) and a handler. **M1 ops take no
   caller-supplied arguments.** There is no generic exec, file-read, path, or Perl-call operation, and
   adding one is prohibited.
6. **Execution runner** (`internal/broker/runner`, the only package allowed to import `os/exec` — guard
   test enforced): binaries come from a fixed allow-list resolved at start-up to absolute paths that
   must be root-owned and not group/world-writable; argv is fixed in the op's source code; environment is
   replaced (`PATH`, `LC_ALL=C.UTF-8`, `SYSTEMD_COLORS=0`, `SYSTEMD_PAGER=`); stdin is `/dev/null`;
   the child runs **as the `homelab-control` uid/gid** (no root child processes in M1), in its own
   process group, killed on timeout; stdout/stderr are size-capped.
7. **File reads as root** use fixed paths, `O_NOFOLLOW`, regular-file checks and size caps, and return
   only allow-listed fields (e.g. Webmin `port`, `ssl`, `bind`; never key files, password hashes or
   session data).
8. **Authorization.** `hc-api` authenticates the user and checks the scope; it forwards the actor. The
   broker **re-checks** the op's scope against the forwarded scopes (defence against API routing bugs).
9. **Audit.** The broker writes its own JSON Lines log (`/var/log/homelab-control/broker-audit.jsonl`,
   root `0600`): time, peer uid/pid, actor user, op, outcome, duration. No results or secrets.
10. **Hardening.** Broker unit: `CapabilityBoundingSet=CAP_SETUID CAP_SETGID` only (to drop children to
    the service uid). Root-only files are read through ordinary owner permissions (uid 0 owns them), so no
    DAC-override capability is granted — verified under systemd 249 and 255 (effective set `0xc0`).
    Plus `NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome=yes`, `PrivateNetwork=yes`,
    `RestrictAddressFamilies=AF_UNIX`, syscall filtering; `systemd-analyze security` exposure 1.5.
    `hc-api` stays unchanged: no capabilities.

## Trust assumptions (explicit)
- **A compromised `hc-api` can call any registered op with any claimed scope.** The broker cannot verify
  end-user sessions; scope re-checks only catch bugs. Safety therefore rests on the op list: in M1 every
  op is read-only, argument-free and returns bounded, non-secret data. This is the property reviewers must
  preserve when adding ops; write ops (M2+) will additionally require broker-side confirmation tokens and
  operation locks (to be designed in M2).
- Local root and anyone in group `homelab-control` are trusted (they can already read the users file or
  reach the socket).
- systemd, `systemctl` and the kernel are trusted; their output is still parsed defensively.

## Options Considered
| Option | Verdict |
|---|---|
| Socket activation + SO_PEERCRED + fixed op table (chosen) | No chown capability, kernel-verified peer, enumerable surface |
| Broker creates socket itself | Needs `CAP_CHOWN`; kept only for dev/tests |
| D-Bus service with polkit | Strong OS integration, but needs a D-Bus library (stdlib-only policy) and polkit rules per distro; revisit for write ops |
| Run `systemctl` as root | Unnecessary privilege; children run unprivileged instead |
| HTTP over loopback TCP | No peer credentials; rejected |
| gRPC/protobuf | Third-party deps and codegen; JSON + strict structs suffice |

## Consequences
- Easier: auditing the root surface (`hlctl broker ops` lists it); testing ops with fixture roots.
- Harder: each new capability needs an op, a scope, tests and review.
- Revisit in M2: write-op confirmation tokens, operation locks, persistent job state, and whether D-Bus
  replaces `systemctl` parsing.

## Amendment 1 (2026-10-21, M2 PR-1b): first argument-taking operation

Authorized by the owner as part of M2 PR-1 ("read-only `services.unit.logs` broker operation").
`services.unit.logs` is the first operation that accepts arguments, so this amendment narrows the
M1 rule "no args accepted" and replaces it with the following rules. Every future argument-taking
operation must meet them.

- **Strict typed arguments.** Arguments are strictly decoded into a typed struct (`{unit, lines}`).
  Unknown fields, wrong types, trailing data and non-objects are rejected.
- **Validated twice.** Values are checked against a conservative grammar: unit names match
  `^[A-Za-z0-9][A-Za-z0-9:_.@-]{0,99}\.(service|socket|timer|mount|automount|path|target|swap)$`,
  and `lines` is 1–500. `hc-api` checks first and returns 400; the broker checks again because it
  does not trust `hc-api`.
- **Re-checked against the system.** A unit must also appear in `systemctl list-units` at the time
  of the call. Free-form names never reach `journalctl`.
- **Fixed argv.** Only validated values are passed, embedded in `--unit=NAME` and `--lines=N`, never
  as standalone arguments. The runner's argument checks still apply.
- **Least privilege for the child.** `journalctl` is the only new allow-listed binary. It runs as
  `homelab-control` with one supplementary group, `systemd-journal`. `RunWithGroups` refuses gid 0,
  and the broker's capabilities are unchanged (`CAP_SETUID`, `CAP_SETGID`). As a result, the
  web-tier user gains read access to the system journal **only through this operation, for listed
  units, and only for callers with the `services.logs` scope**. `hc-api` itself is not added to the
  group.
- **Bounded output.** Messages are capped at 4 KiB each and 1 MiB in total (oldest dropped first).
  Control characters are stripped, and non-UTF-8 data is replaced.
- **Audited without content.** The broker audit record carries the validated `unit=` and `lines=`,
  or `invalid args`; it never contains raw input or log content. A result is released only after
  its audit record has been written (fail closed). See `docs/security/AUDIT-LOG.md`.
- **Scope.** The scope is `services.logs`, which follows the D-7 convention `<module>.<level>`. It
  is not in `DefaultScopes`, so existing accounts do not gain it on upgrade.

The trust assumption above still holds. A compromised `hc-api` can read journal entries of any
listed unit, up to 500 lines per call. Journals can contain sensitive application output, which is
why this is a separate scope. The operation remains read-only and does not touch the write-safety
design that M2 still has to deliver.

## Amendment 2 (2026-10-23, M2-2): the `root-exec` runner class

Authorized by the owner on 2026-10-23 (decision D-4, option A) together with ADR 0011. It narrows
the M1 rule "no root child processes" to the following, and only for operations that went through
the ADR 0011 plan/apply review.

- **Closed template table.** `internal/broker/runner` holds a fixed table of root argv templates.
  M2-2 has exactly four:
  - `systemctl.start`, `systemctl.stop`, `systemctl.restart`: each
    `systemctl --no-ask-password --job-mode=fail <verb> -- <unit>`, exactly one `.service` unit.
    `--job-mode=fail` makes systemd refuse instead of replacing a job queued on the unit by someone
    else.
  - `systemctl.show` (read-only): `systemctl show --no-pager --property=<fixed list> -- <units>`, 1–32
    units of any type. Used by `services.unit.apply` to revalidate and to verify the outcome, and at
    broker start to describe interrupted operations. Running it as root keeps those facts out of
    reach of a compromised `hc-api`: unprivileged children run as the same uid as `hc-api`, which
    could signal them or open their output pipe through `/proc`.
  The caller passes a template ID and targets; it cannot pass a binary, flags or other arguments.
- **Target validated in the runner too.** The runner's own validator (independent of
  `servicepolicy`) accepts only `.service` names without a leading `-`, escapes, separators,
  whitespace or template bases, and the target always follows `--`.
- **Same execution rules as other children.** Allow-listed absolute binary, root-owned and not
  group/world-writable, re-checked at use; environment replaced (`PATH`, `LC_ALL=C.UTF-8`,
  `SYSTEMD_COLORS=0`, `SYSTEMD_PAGER=`); stdin `/dev/null`; own process group, killed on timeout
  (30 s); stdout and stderr capped. No shell.
- **Handle confined to root-exec ops.** The server keeps the root handle outside the shared
  environment and passes it, in a per-call copy, only to operations registered with privilege
  `root-exec`. In M2-2 that is `services.unit.apply` alone. A guard test fails if any other
  non-test file references the handle or the templates; a runtime test proves that a read-only op
  never receives it even if one is put into the shared environment.
- **No new capabilities.** The broker unit keeps `CapabilityBoundingSet=CAP_SETUID CAP_SETGID`.
  The root child talks to systemd over its private socket as uid 0; it gains nothing beyond what the
  broker already had. Verified under real systemd (see `docs/m2/M2-2-ACCEPTANCE.md`).
- **Snapshot tests.** `TestRootTemplatesAreClosed` (exact template set and argv) and
  `TestRegistryIsClosed` (exact op set, privileges, scopes, risk classes, lock domains, templates)
  must be changed, and reviewed, for any addition.

The trust assumption above is restated for writes in ADR 0011: a compromised `hc-api` can request
any operation the broker's policy allows. The narrow template table, the protected-unit and
dependency policy, plan tokens, locks and the journal bound what that means.

## Amendment 3 (2026-11-04, M2-3, proposed for owner acceptance): enablement templates, read views, module refusal

Proposed with the owner's M2-3 development authorization of 2026-11-04 (scope in
`docs/m2/M2-3-DESIGN-REVIEW.md` §0); **not accepted until the owner accepts M2-3**. Review:
`docs/m2/M2-3-DESIGN-REVIEW.md` §2–§3.

- **Four new root templates**, each exactly one `.service` target after `--`, validated again by the runner
  (`validEnablementUnit` refuses template instances for enable/disable):
  `systemctl.enable` / `systemctl.disable` = `systemctl --no-ask-password --no-reload enable|disable -- <unit>`;
  `systemctl.is-enabled` = `systemctl is-enabled -- <unit>` (read-only); `systemctl.show-install` = `systemctl show
  --no-pager --property=<Id,Names,LoadState,FragmentPath,SourcePath,DropInPaths,Transient,NeedDaemonReload,UnitFilePreset> -- <unit>`
  (read-only). `--no-reload` prevents the implicit global daemon-reload that `enable`/`disable` otherwise
  perform (measured, §1 P1). No `--now`, `--runtime`, `--force`, `mask`, `link`, `preset`, `edit` or
  `daemon-reload` exists; `TestRootTemplatesAreClosed` fails on any of them.
- The handle still reaches only root-exec ops; `services.enablement.apply` is the second one.
  `internal/broker/ops/enablement.go` joins the reviewed root-exec files (`TestRootExecConfined`).
- **Root reads in-process:** `services.enablement.plan` (registered `root-read`) and apply read unit files
  as root only to extract `[Install]` unit names, under the path rules of the review (§3.5).
- **Read-only ops** `services.timers.list`, `services.sockets.list`, `services.unit.detail` and
  `services.unit.contents` run fixed argv as `homelab-control`; detail and contents take `{unit}` and first
  check inventory membership (`list-units`/`list-unit-files -- <name>`), per Amendment 1.
- **Module refusal:** ops of a module disabled in the root-owned `/etc/homelab-control/modules.json` are refused
  with `module_disabled` before the scope check and omitted from `broker.info`; an invalid file disables every
  module. Journal recovery is unaffected.
- **No new capabilities:** the broker unit is unchanged (`CapabilityBoundingSet=CAP_SETUID CAP_SETGID`); PID 1
  writes the symlinks over D-Bus (§1 P2).

## Amendment 4 (2026-11-06, M2-6, proposed for owner acceptance): hostnamectl/timedatectl templates

Proposed with the owner's 2026-11-06 choice to finish M2-6 apply after the read-only skeleton (PR #40)
landed on main; **not accepted until the owner accepts M2-6 apply**. Scope:
`/containers/workspaces/hc-team/shared/m2-6-apply-lead-scope.md` and `docs/m2/M2-6-HOST-SETTINGS-SKELETON.md`.
This amendment does not authorize NTP, network, users, D-Bus live view, kernel UTS writes,
or other hostnamectl/timedatectl verbs.

- **Two new allow-listed binaries:** `hostnamectl` and `timedatectl`, resolved like `systemctl` (`/usr/bin`
  then `/bin`), root-owned and not group/world-writable. `TestAllowListedBinariesAreClosed` is the allow-list snapshot.
- **Six new root templates**, values after `--`, validated again by the runner using
  `hostinfo.ValidStaticHostname` / `ValidPrettyHostname` / `ValidTimezoneName`:
  - `hostnamectl.set-static` = `hostnamectl --no-ask-password set-hostname --static -- <name>`
  - `hostnamectl.set-pretty` = `hostnamectl --no-ask-password set-hostname --pretty -- <name>` (empty name is removal)
  - `timedatectl.set-timezone` = `timedatectl --no-ask-password set-timezone -- <Area/City>`
  - `hostnamectl.status` = `hostnamectl --no-ask-password status` (read-only, no target)
  - `timedatectl.status` = `timedatectl --no-ask-password status` (read-only, no target)
  - `timedatectl.list-timezones` = `timedatectl --no-ask-password list-timezones` (read-only, no target)
  No `set-chassis`, `set-icon`, `set-deployment`, `set-ntp`, `set-time` or `set-local-rtc` exists;
  `TestRootTemplatesAreClosed` fails on any of them.
- The handle still reaches only root-exec ops; `host.settings.apply` is the third one.
  `internal/broker/ops/hostsettings_write.go` joins the reviewed root-exec files (`TestRootExecConfined`).
  `host.settings.plan` is `unprivileged-exec` (status and list-timezones as `homelab-control`).
- **No new capabilities:** the broker unit is unchanged (`CapabilityBoundingSet=CAP_SETUID CAP_SETGID`).
  hostnamectl/timedatectl talk to hostnamed/timedated as uid 0 on apply; they gain nothing beyond what
  the broker already had. `hc-api` still does not spawn them.

## Amendment 5 (2026-11-07, M4-1b, Proposed): read-only package queries

Proposed for the M4-1b read-only Software Center checkpoint, following owner words recorded in
`docs/m2/OWNER-DECISIONS.md` and scope in `docs/m4/M4-1B-SOFTWARE-CENTER.md`. This amendment does not
authorize install, remove, upgrade, repository changes, key import, or metadata refresh.

- **Closed CLI templates only.** Package-manager commands use fixed argv templates and stdlib parsing.
  Do not add PackageKit, libdnf, libalpm, or Go package-manager libraries.
- **Read-only and unprivileged.** Query children run as `homelab-control`. Operations use local cached
  data only, do not access the network, and do not refresh repository metadata. Output is bounded.
- **Validated query input.** Bound the accepted character set and length. Reject values that begin with
  `-`. Pass a validated query only in a fixed argv slot or after `--`.
- **Fixed execution environment.** Replace the child environment, including setting `LC_ALL=C`, and use
  `/dev/null` for stdin.
- **Unsupported families fail closed.** If a family's tool cannot run unprivileged and cache-only, report
  `unsupported: <reason>`. Do not add a root template as a workaround.
- **API audit before release.** Write the API audit record before releasing the result. Record counts only,
  not package lists or query text.
- **Scope.** Add `packages.read` for read operations. Do not add or use `packages.manage`; reserve it
  for a separately reviewed write ADR. This scope is not implied by another scope or added to defaults.
- **No write path.** No root template, plan/apply operation, package transaction, or repository mutation
  is authorized by this amendment. The later write ADR is reserved as 0014 and is not part of this change.
- **Proposed status.** This amendment remains subject to review and owner acceptance. It does not accept
  ADR 0008 or authorize M4 acceptance.

## Amendment 6 (2026-11-13, P0 storage, Proposed): unused `mkfs.ext4` root template

Proposed with the P0 shared-storage work. **Not accepted.** Independent security review of PR #74
(approve-with-nits) covers the unused template only. This amendment does not authorize execute
wiring, guest or host write tests, or a broker unit change.

- **One unused closed template.** `mkfs.ext4` = `mkfs.ext4 -q -- <by-id>`; exactly one
  `/dev/disk/by-id` target (`wwn-0x` 16 or 32 hex, `nvme-eui.` 16 hex, `nvme-uuid.`, `ata-`,
  `scsi-`, `virtio-`; optional `-part` 1–3 digits). No `-F`. Kernel names, mapper/md/lvm/dm,
  by-path, usb, `..`, and `//` are refused.
- **No caller.** No broker op lists or runs this template. `Advance` stays `BlockNoExecute`.
  `TestUnusedStorageRootTemplateConfined` fails if `RootMkfsExt4` or `mkfs.ext4` appears outside
  `runner.go` (tests excepted). A wiring PR must change that test on purpose.
- **Wiring gates (not met here).** G1: non-TTY stdin/stdout skip mke2fs's existing-FS prompt;
  the op must prove identity, unmounted, not a member, under lock. G2: require a block device
  immediately before exec (mke2fs may create a regular file). G3: do not widen the main broker
  unit (`PrivateDevices=yes`); use a narrow helper plus SELinux/AppArmor review. G4: bind
  confirmation to the plan's recorded identity, not the by-id string. G5: duration policy and
  recovery for a killed mkfs.
- **Proposed status.** This amendment remains subject to review and owner acceptance. It does
  not accept ADR 0008 for storage writes.

## Amendment 7 (2026-11-13, P0 storage, Proposed): unused closed storage helper unit

Proposed with the P0 shared-storage work. **Not accepted.** Independent security review of
the packaging PR covers the unused unit only. This amendment does not authorize owner O-1
(`PrivateDevices=no` / `DeviceAllow`), execute wiring, guest or host write tests, or any
change to `homelab-control-broker.service`.

- **One unused unit.** `packaging/common/homelab-control-storage-helper.service` ships in
  debian/fedora/arch development packages. No `[Install]`. `PrivateDevices=yes`. No
  `DeviceAllow`. Empty `CapabilityBoundingSet`. `ExecStart=/bin/false`.
  `RefuseManualStart=yes`. `ConditionPathExists=/nonexistent-unused`. No `Restart`.
  Install must not enable it. Uninstall must not `stop`/`disable` it on the same
  command as the API or broker (H2).
- **Broker unchanged.** The main broker keeps `PrivateDevices=yes` and
  `CapabilityBoundingSet=CAP_SETUID CAP_SETGID`. Do not add storage device access there.
- **No caller.** No broker op, `Advance` execute path, or non-test Go names the unit.
  `TestUnusedStorageHelperUnitConfined` fails if those constraints drift (duplicate
  keys, sha256 snapshot). `Advance` stays `BlockNoExecute`.
- **Later PRs (not this one).** O-1 read-only probe must, in the same PR: closed
  `DeviceAllow` of named by-id nodes resolved at plan (no wildcards); decide
  per-op transient unit vs persistent helper; choose `User=` deliberately (not
  `homelab-control`); SELinux/AppArmor dedicated domain with a labelled helper
  binary that transitions (do not repeat P2 `lib_t`/`init_t`; do not use
  unconfined); limited read-only block-node access; AVC validation; identity
  audit; timeouts and recovery (G5). Execute wiring is a separate PR (ADR 0011
  apply, T1 update, G1–G5). Each needs its own independent security review.
- **Proposed status.** This amendment remains subject to review and owner acceptance. It
  does not accept ADR 0008 for storage writes.
