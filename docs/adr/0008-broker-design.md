# ADR-0008: Privileged broker design (M1)

**Status:** Accepted (2026-10-16, owner) **for the four existing read-only, argument-free M1 operations only**; Amendment 1 (2026-10-21, `services.unit.logs`) and Amendment 2 (2026-10-23, `root-exec`, owner decision D-4) extend it (`broker.info`, `legacy.webmin.status`, `hardware.dmi.identity`, `services.systemd.list`). Amendments 3 (M2-3), 4 (M2-6), 5 (M4-1b), and 6 (P0 unused mkfs.ext4 template) are **Proposed**. Adding or changing an operation requires a new review.
> **Amended 2026-11-25 by [ADR 0016](0016-independent-project-webmin-removed.md):** the `legacy.webmin.status` op and its `/etc/webmin` reads were removed.
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
   ≤ 4 MiB and a 5 s read deadline. Admission is nonblocking: the eight global
   in-flight slots are partitioned as control-plan (1), control-apply (2),
   services-read (2), system-read (1), packages (1), and storage probe (1).
   `broker.info` has a separate one-slot limit and does not consume a global
   slot. A full class or global limit refuses the request with `busy`; work is
   not queued.
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

## Amendment 6 (2026-11-13, P0 storage, Proposed): `mkfs.ext4` root template

Proposed with the P0 shared-storage work. **Not accepted.** Independent security review of PR #74
covered the unused template. On 2026-11-23 the owner required the template to have a caller
(`ext4.format.plan` / `ext4.format.apply`) and allowed the proof to run in a disposable VM.
This amendment still does not accept ADR 0008 for storage writes, and it does not change the
main broker unit.

- **One closed template.** `mkfs.ext4` = `mkfs.ext4 -q -- <by-id>`; exactly one
  `/dev/disk/by-id` target (`wwn-0x` 16 or 32 hex, `nvme-eui.` 16 hex, `nvme-uuid.`, `ata-`,
  `scsi-`, `virtio-`; optional `-part` 1–3 digits). No `-F`. Kernel names, mapper/md/lvm/dm,
  by-path, usb, `..`, and `//` are refused.
- **One caller.** `ext4.format.apply` is the only broker op that lists or runs this template.
  Scope `ext4.format` is not a default and is not implied by `ext4.read` or `storage.read`.
  Risk is destructive (password re-entry). `TestUnusedStorageRootTemplateConfined` allows the
  template name only in `runner.go`, the registry, and `ext4format.go`.
- **Gates the caller enforces.** G1: the runner's stdin is `/dev/null` (no existing-FS prompt)
  and apply holds the per-identity lock plus a lock on the device number, so a WWN
  alias and a serial alias cannot format that disk at the same time. G2: the path must be a block device immediately
  before exec; a regular file is refused. G4: the plan token target is the persist identity
  (`wwn`, `nvme-uuid`, or `serial`) and the by-id basename must name that same identity.
  The by-id path and rdev are also in the state digest, so a path swap is a conflict and
  does not format. Partition device numbers include sysfs symlink entries, so a mounted
  partition is busy. G5: the child budget is 8 minutes on a context independent of the
  request; timeout, panic, or a non-zero exit is `uncertain` and is not retried.
- **G3 is not met.** The main broker unit stays `PrivateDevices=yes`. This caller does not
  add `DeviceAllow` and does not change `homelab-control-storage-helper.service`
  (`ExecStart=/bin/false`). A broker that cannot see the disk fails closed before exec.
  A live format belongs on a disposable VM disk, not the MediaMouse host.
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

## Amendment 8 (2026-11-17, M2-4 O-1, Proposed): read-only block-probe helper unit

Proposed; **not accepted.** Owner approval covers implementation and isolated
validation of the O-1 probe only (`docs/m2/OWNER-DECISIONS.md`); no host or
production activation. Details and residual privilege:
`docs/m2/M2-4-O1-DEVICE-POLICY.md`.

- **Unit.** `homelab-control-storage-probe@.service` (socket-activated, `Accept=yes`,
  `MaxConnections=1`) is separate from the unused Amendment 7 helper unit (left unchanged). It stays
  disabled by default: no `[Install]`, opt-in marker `ConditionPathExists`, `RefuseManualStart=yes`.
- **Device policy.** `PrivateDevices=no`, `DevicePolicy=closed`, `DeviceAllow=block-sd|blkext|virtblk|md r`
  only; no `w`/`m`, no `char-*`, no capabilities, `DynamicUser=yes` + group `disk`.
  `RootImage=`/`MountImages=`/`ExtensionImages=` are banned (implicit `rwm` grants).
  `TestStorageProbeServiceFailClosed` and `tests/packaging/storage-probe-policy-check.sh` enforce this.
- **Residual.** The static unit permits raw reads of all disks in those classes, including
  USB (`sd`); it does not satisfy Amendment 7's "named by-id nodes, no wildcards". Narrower
  designs (per-request transient unit; fd passing) need their own review. The class-level policy is accepted only for isolated validation (owner O-1 decision); a narrower design and separate owner authorization are required before any host activation.
- **MAC.** `SELinuxContext=hc_storage_probe_t` / `AppArmorProfile=hc-storage-probe` fail closed when
  the LSM is active and the policy is missing. No unconfined, `lib_t` or `bin_t` shortcut.
- **Startup.** The `/proc/devices` group self-check is the helper's duty (Exec lines inside the
  MAC domain cannot run `grep`).

## Amendment 9 (2026-11-24, M11, Proposed): read-only networking reads

Proposed for the M11 read-only networking checkpoint (`docs/project/04-ROADMAP.md` M11; owner slot
2026-11-07 in `docs/m2/OWNER-DECISIONS.md`). **Not accepted.** It authorizes no firewall, bond, or
port-binding change, and it does not change the broker unit.

- **Two root-read ops, no arguments.** `network.ports.list` (scope `networking.ports.read`) and
  `network.firewall.config` (scope `networking.firewall.read`), both `root-read`, admission
  `system-read`, 10 s. They are tied to the `ports` and `firewall` modules by op prefix, so a disabled
  module refuses its op.
- **Ports: fixed `/proc` reads in-process.** The broker unit has `PrivateNetwork=yes`, so the op reads
  `/proc/1/net/{tcp,tcp6,udp,udp6}` (the host namespace, as seen by PID 1). Owners come from three
  sources, each labelled: `process` (a `/proc/<pid>/fd` link names the socket inode; observed),
  `systemd-socket` (fixed unprivileged `systemctl list-sockets --all --output=json --no-pager`, whose JSON
  has no type, then `systemctl show --no-pager --property=Id,Listen -- <listed .socket units>` for the
  Stream/Datagram type; observed) and `account` (every process of the socket's non-root
  account runs in one unit, from `/proc/<pid>/status` and `cgroup`, which need no ptrace access;
  inferred). The kernel's ptrace check on `/proc/<pid>/fd` needs the reader's capabilities to cover the
  target's, so with only `CAP_SETUID`/`CAP_SETGID` the broker reads almost no fd table, including those
  of full-capability root daemons (observed on the Ubuntu 26.04 guest, 2026-11-24). `CAP_SYS_PTRACE` is
  **not** added: it would let the broker read any process's memory and needs its own review. Bounded:
  4096 sockets, 32768 processes, 65536 fds per process.
- **Firewall: saved configuration only.** One fixed unprivileged `systemctl show --no-pager
  --property=Id,LoadState,ActiveState,UnitFileState --` over the closed unit list
  (`firewalld`, `ufw`, `nftables`, `netfilter-persistent`, `iptables`, `ip6tables`) decides the active
  backend (firewalld, then ufw with `ENABLED=yes`, then nftables, then iptables). Only that backend's
  fixed configuration paths are read as root (`O_NOFOLLOW`, regular file, 1 MiB cap). The live kernel
  ruleset is **not** read: that would need `CAP_NET_ADMIN` and the host network namespace in the broker,
  which needs its own review.
- **Bonds stay in `hc-api`.** Kernel bonding state is world-readable sysfs plus runtime state files of
  systemd-networkd, NetworkManager and ifupdown; no broker op. NetworkManager keyfiles and netplan YAML
  (which can hold secrets) are never read.
- **No secrets, counts-only audit.** Results hold addresses, ports, process names, units, short
  container IDs and firewall rule text. API audit records socket/bond counts or the backend ID only.
- **Scopes.** `networking.ports.read`, `networking.bonds.read`, `networking.firewall.read`: not
  defaults, not implied by any scope (including `networking.proxy.read`) or by each other.
- **Proposed status.** Subject to review and owner acceptance. Firewall writes need their own owner
  authorization and an ADR 0011 review.

## Amendment 10 (2026-11-24, M6 partition apply, Proposed): `sfdisk` GPT template

Proposed. **Not accepted.** This amendment does not accept ADR 0008 for storage
writes, and it does not change the main broker unit or the unused storage helper.
Amendment 9 is the M11 read-only networking checkpoint and is unchanged.

- **One closed template.** `sfdisk.gpt` = `sfdisk --wipe=always --wipe-partitions=always -- <by-id>`.
  Exactly one whole `/dev/disk/by-id` path (the same families as `mkfs.ext4`, without `-partN`).
  No `--force`. The script is stdin, generated from the reviewed layout, and is not caller argv.
- **One caller.** `storage.partition.apply` is the only broker op that lists or runs this template.
  Scope `storage.partition` is not a default and is not implied by `storage.read` or `ext4.format`.
  Risk is destructive (password re-entry). The apply also takes the ext4 format device-number lock.
- **What it does not do.** It does not format a filesystem, mount, or resize. `unknown` is refused.
  A slice must start at or after 1 MiB and end at least 1 MiB before the disk.
- **G3 is not met.** The main broker unit stays `PrivateDevices=yes`. A broker that cannot see
  the disk fails closed before exec. A live partition belongs on a disposable VM disk.

## Amendment 11 (2026-11-24, M4, owner-authorized writes): APT settings file and archive mirror

Numbered 11 because Amendment 9 (M11 networking) and Amendment 10 (M6 `sfdisk`) came first. The
owner authorized these two writes on 2026-11-24 (`docs/m2/OWNER-DECISIONS.md`, "APT settings and mirrors"). They follow ADR 0011
(plan → confirm → apply, journal, lock domain `apt-files`, reversible risk, no step-up). They do not
authorize package install/remove/upgrade, `apt update`, adding repositories or importing keys.

- **Ops.** `packages.aptconfig.read` (unprivileged `apt-config dump`, new allow-listed binary with one
  fixed argv, kept off the generic `Run` path like `apt-get`), `packages.aptconfig.plan|apply`,
  `packages.mirrors.inventory`, `packages.mirror.plan|apply`. Plans read files only; applies send the
  reviewed request to the helper. Scope `packages.configure` (not default, implies nothing else).
- **Helper.** `hc-apt-apply`, socket-activated (`Accept=yes`, `MaxConnections=1`), opt-in marker
  `/etc/homelab-control/apt-apply.enabled`, socket not enabled by the package. `User=root` with an empty
  capability set (root owns every file it writes), `ProtectSystem=strict`, `ReadWritePaths=/etc/apt`,
  `StateDirectory=homelab-control-apt-apply` for backups, `PrivateNetwork=yes`, AF_UNIX only, AppArmor
  `hc-apt-apply` on Debian/Ubuntu. It executes no programs (guard test `TestAptApplyHelperConfined`).
- **What it writes.** Only `/etc/apt/apt.conf.d/60homelab-control`, rendered from a closed typed set
  (no free text in APT syntax; the proxy is a credential-free http(s) URL), and the archive URI of
  `/etc/apt/sources.list` or `/etc/apt/sources.list.d/{debian,ubuntu}.sources`. A mirror change edits only
  the URI bytes of entries classified as this release's archive (suites, the distribution's components,
  vendor or no `Signed-By`, one URI); security, ports and other entries are never changed. The helper
  re-reads the file, refuses a symlink, a non-root owner or a digest that differs from the plan, writes a
  0600 backup (newest 10 kept), replaces the file through a temporary file and rename, and reads it back.
- **Trust.** The signing key of a changed entry is unchanged, so APT verifies the new mirror with the same
  key at its next update. Candidates come from the official lists (Ubuntu `mirrors.ubuntu.com/<CC>.txt`,
  which is plain HTTP; Debian `deb.debian.org` and `ftp.<cc>.debian.org`) and are probed by hc-api over
  HTTP(S) without credentials; list candidates must resolve to public addresses. The plan accepts only a
  mirror offered by a probe in the last 30 minutes. A frozen or stale mirror is shown as stale (index more
  than 12 hours older than the newest seen); the probe does not verify signatures.
- **Proposed status.** Implementation and VM validation are pending review; this amendment does not
  accept ADR 0014 or ADR 0015.

## Amendment 12 (2026-11-25, M5, owner-authorized): storage helper runs the standard tools

The owner activated the read-only storage helper (`docs/m2/OWNER-DECISIONS.md`, "Use the standard storage
tools"). `storage.probe` (scope `storage.probe`, not a default) is unchanged on the broker side: no
arguments, broker rate limit, root-only socket. What changes is the helper behind it:

- **Protocol 2.** The request carries only the version; the helper names devices from its own `lsblk`
  output (kernel-name grammar, SMART classes `sd*` and `nvme*n*`). Version 1 (caller-named devices,
  fd-pinned blkid, the native `lvmmeta` reader) is removed.
- **Fixed templates** (`internal/storagetools`): `lsblk --json --list --bytes --output …`;
  `smartctl --json=c --nocheck=standby --health --info --attributes /dev/<kname>` (never wakes a disk in
  standby); `pvs`/`lvs --readonly --reportformat json --units b --nosuffix --segments -o …`
  (metadata-only; no locks, no device-mapper).
- **Unit.** Read-only `DeviceAllow=` classes as before, plus `CapabilityBoundingSet=CAP_SYS_RAWIO` and the
  same ambient capability for SMART pass-through. No `CAP_SYS_ADMIN`. `@raw-io` stays denied. The
  package ships the opt-in marker; an administrator enables the socket.
- **Optional tools.** smartmontools and lvm2 are not dependencies. When missing, the result says so and is
  partial; lsblk (util-linux) is required.

**Amendment 12 update (owner, 2026-11-25):** the helper also holds `CAP_SYS_ADMIN`, for NVMe admin commands
(SMART/Health log, Identify), which the kernel allows only with it. The system-call filter, namespace and
kernel protections, read-only device policy and AppArmor `deny mount` stay; the profile allows exactly
`sys_rawio` and `sys_admin`.

## Amendment 13 (2026-11-25, M7, owner-authorized): OpenZFS through the storage helper

Part of the owner's activation of the read-only helper ("Activate read-only helper", which named
`/dev/zfs`). The helper request gains `scope`: empty is the storage probe of Amendment 12; `zfs` runs
only three fixed templates (`internal/storagetools/zfs.go`): `zpool list -H -p -o
name,size,alloc,free,frag,cap,health`, `zpool status -p`, and `zfs list -H -p -o
name,used,avail,refer,mountpoint,compression,type -t filesystem,volume`. Any other scope is refused.

- **Broker op** `zfs.status`: scope `zfs.read`, no arguments, `root-read`, admission `system-read`, a
  3-second answer cache so page refreshes cannot drive the helper. The `zfs` module gates it by the
  `zfs.` prefix.
- **Unit.** One explicit node, `DeviceAllow=/dev/zfs rw`: libzfs opens the control device read-write for
  every ioctl, including the ones `zpool list`/`status` and `zfs list` use. Block classes stay `r`. The
  policy guard accepts exactly this line and rejects `rwm`, another node, or its removal.
- **AppArmor.** `ix` for `zpool`/`zfs`, `/dev/zfs rw`, `/etc/zfs/** r`, `/etc/hostid r` and
  `@{PROC}/sys/kernel/spl/hostid r` (VM finding: zpool reads the SPL hostid).
- **Parsing.** Script mode output and the `zpool status` text report, bounded (512 KiB per command, 64
  pools, 512 vdevs per pool, 2048 datasets, 1 KiB per text field). `zpool status -j` is not used because
  Ubuntu 22.04/24.04 ship OpenZFS 2.1/2.2 without it.
- **No writes.** No scrub, import, export, online/offline, clear or property change exists.

## Amendment 14 (2026-11-25, M7, owner-authorized): Btrfs through the storage helper

Same activation as Amendment 13. Request scope `btrfs` runs four fixed templates
(`internal/storagetools/btrfs.go`) per mounted Btrfs filesystem: `btrfs filesystem usage -b <mnt>`,
`btrfs device stats <mnt>`, `btrfs scrub status <mnt>` and `btrfs subvolume list <mnt>`. None starts,
cancels, resets or changes anything.

- **Mountpoints are the helper's own.** It reads its `/proc/self/mountinfo`, groups `btrfs` mounts by device
  number (one filesystem, any number of subvolume mounts) and uses the first mountpoint it can open. No
  caller names a path. Limits: 16 filesystems, 1024 subvolumes each (1 MiB of `subvolume list` output),
  256 KiB per other command.
- **ProtectHome stays.** systemd removes `/home`, `/root` and `/run/user` sub-mounts from the helper's
  namespace, so a filesystem mounted only there is not inspected; the page names such mounts from its own
  unprivileged mount list.
- **Broker op** `btrfs.status`: scope `btrfs.read`, no arguments, `root-read`, admission `system-read`. ZFS
  and Btrfs share one per-scope 3-second answer cache.
- **AppArmor.** `ix` for `btrfs`; `/**/ r` (directories only, to open a mountpoint for the ioctls);
  `/var/lib/btrfs/ r` and `/var/lib/btrfs/scrub.status.* rwk`. `scrub status` takes an exclusive lock on
  its saved state, which AppArmor counts as write; `ProtectSystem=strict` keeps `/var/lib` read-only in the
  helper. The policy guard allows write only for `/dev/zfs` and this file, and rejects a file-wide read.

## Amendment 15 (2026-11-25, M7, owner-authorized): Software RAID through the storage helper

Same activation as Amendments 13 and 14. Request scope `md` reads `/proc/mdstat` (opened directly, bounded,
the `mdadminfo` parser) and runs one fixed template per listed array: `mdadm --detail /dev/<kernel name>`
(`internal/storagetools/md.go`). Names are the kernel's own (`md0`, `md127`, `md_<name>`); no caller names a
device. Limits: 64 arrays, 256 members each, 128 KiB per report; budget 30 s.

- `mdadm --detail` opens the md block device (`block-md r`, already granted), so this scope stays behind the
  block-class self-check; the ZFS and Btrfs scopes, which open no block device, run before it.
- **Broker op** `mdadm.status`: scope `mdadm.read`, no arguments, `root-read`, admission `system-read`,
  deadlines from the shared helper budget.
- **AppArmor.** `ix` for `mdadm`; `/etc/mdadm/**`, `/etc/mdadm.conf`, `/run/mdadm/**` and `@{PROC}/mdstat`
  read-only. No new device or write rule.
- **No writes.** No create, assemble, add, remove, fail, grow or stop exists.

## Amendment 16 (2026-11-25, M7, owner-authorized): LVM page through the storage helper

Request scope `lvm` runs the existing `pvs` and `lvs` templates of Amendment 12 plus one new fixed template,
`vgs --readonly --nolocking --reportformat json --units b --nosuffix -o
vg_uuid,vg_name,vg_size,vg_free,vg_attr,pv_count,lv_count,vg_missing_pv_count`, through the same function as
the full probe. It scans block devices for PV labels, so it runs behind the block-class self-check. Budget
30 s.

- **Broker op** `lvm.status`: scope `lvm.read`, no arguments, `root-read`, admission `system-read`; it
  returns the three raw reports only when all three are present.
- **hc-api** decodes them with `storageinfo.ParseLVM`/`ParsePVS` (as for the Storage page) and the new
  `ParseVGS`.
- **Removed:** `lvminfo`'s sysfs `dm-*` name walk, which reconstructed LVs from device-mapper names. The page's
  `lvm` field (device list) is gone from `GET /lvm`; `thin_check` is offered when `lvs` reports a thin pool.
- **AppArmor:** `vgs` joins `{lvm,pvs,lvs}` under `ix`. No new device or write rule.


## Amendment 17 (2026-11-26, M7, owner-authorized): ext4 and XFS through the storage helper

Two request scopes, each one fixed template per mounted filesystem, names taken from the helper's own mount
table (no caller argument):

- `ext4`: `tune2fs -l /dev/block/<major>:<minor>`. The device number comes from mountinfo and
  `/dev/block` is udev's link to the node, so no mount source string is opened. Devices whose class the
  unit does not grant (loop and others, by `/proc/devices`) are skipped with a reason. It opens block
  devices, so it runs behind the block-class self-check. Budget 30 s.
- `xfs`: `xfs_spaceman -p xfs_info -c info <mountpoint>`, which is what the `xfs_info` shell script runs for a
  mounted filesystem; the helper runs it without a shell. Mountpoint ioctls only, so it runs before the
  self-check, as Btrfs does. Budget 30 s.

- **Broker ops** `ext4.status` (scope `ext4.read`) and `xfs.status` (scope `xfs.read`): no arguments,
  `root-read`, admission `system-read`, timeouts derived from `HelperBudget`. Module `xfs` gains op prefix
  `xfs.`.
- **Unit:** `DeviceAllow=block-device-mapper r` joins the reviewed read-only classes. ext4 on LVM is the
  Ubuntu default install. Reading an opened LUKS volume reads plaintext, the same privilege the helper already
  has on every unencrypted disk in the other classes. Still no `w` or `m` on any block device.
- **AppArmor:** `tune2fs` and `xfs_spaceman` `ix`; `/dev/dm-[0-9]* r`; `/run/blkid/blkid.tab r` (libblkid
  cache); `/etc/nsswitch.conf`, `/etc/passwd`, `/etc/group` `r` (tune2fs names the reserved-blocks uid and
  gid). No new write rule.
- **f2fs** needs no helper. f2fs-tools reads the device and has no report for a mounted filesystem, so hc-api
  reads the kernel ABI files `/sys/fs/f2fs/<disk>/` (sysfs-fs-f2fs) unprivileged.
- **Known limit:** ProtectHome=yes removes /home, /root and /run/user sub-mounts from the helper's namespace.
  An ext4 or XFS mounted only there is not inspected, and the page names it.

## Amendment 18 (2026-11-26, M8): SnapRAID status through the MergerFS helper

`mergerfs.snapraid.status` ran `snapraid status` in the broker as the service user. On a real array that
never works: SnapRAID takes its array lock (`<first content file>.lock`) for status too (cmdline/snapraid.c:
only `devices`, `smart` and `probe` skip it), the broker's `ProtectSystem=strict` makes that a read-only
file system, and content files are root `0600`. A disposable Ubuntu 26.04 guest (snapraid 13.0) showed
"Failed to create the lock file ... Read-only file system".

- **Change:** the op sends the closed request `{"action":"status"}` to the opt-in root helper
  `hc-mergerfs-apply`, which runs exactly `snapraid status` and returns its text (48 KiB cap). The broker
  keeps the per-disk table and SnapRAID's summary sentences and redacts paths. Privilege `root-read`,
  timeout 60 s (client 75 s). Without the helper the op answers "unavailable".
- **Allow-list:** `snapraid` leaves the broker runner's allow-list; only the helper runs it.
- **Helper:** `status` takes no field. The plan/apply ops refuse it. The lock is SnapRAID's own, so a
  status read during a sync, scrub or fix reports `locked` instead of waiting.
- **AppArmor:** `/var/snapraid.content* rwlk` for SnapRAID's example content path.
- **Persist:** the array definition is no longer saved over a `snapraid.conf` that the helper did not
  write (first line is the managed header); nothing, including fstab, is written then.
