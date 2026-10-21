# ADR-0008: Privileged broker design (M1)

**Status:** Accepted (2026-10-16, owner) **for the four existing read-only, argument-free M1 operations only** (`broker.info`, `legacy.webmin.status`, `hardware.dmi.identity`, `services.systemd.list`). Adding or changing an operation requires a new review.
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
