# ADR-0001: Privilege boundary — unprivileged API, typed root broker

**Status:** Proposed (M0 implements the unprivileged half; broker is M1/M2)
**Date:** 2026-10-15
**Deciders:** Project owner (Ian); security review before any broker operation is enabled

## Context
Upstream Webmin runs `miniserv.pl` as root and executes every CGI as root; per-user restrictions are
enforced inside each module's Perl code (audit §6). Its advisory history is dominated by
"less-privileged user → root" bugs, and its RPC endpoints allow arbitrary module-function calls by
design. Our rules forbid arbitrary shell execution and unaudited root plugins, and require per-operation
RBAC, target revalidation, locking, timeouts and audit.

## Decision
Split the system into three processes with a hard boundary:

1. **`hc-api` (unprivileged, `homelab-control` system user).** Terminates HTTP, authenticates, enforces
   RBAC/CSRF, serves the UI, reads world-readable state (procfs/sysfs/os-release). It **cannot spawn
   processes** — enforced by `backend/internal/guard` tests (no `os/exec`, `syscall.Exec`,
   `os.StartProcess`, `net/rpc`, `plugin`, `unsafe`) — and refuses to start as root.
   The systemd unit drops all capabilities and applies `ProtectSystem=strict`, `NoNewPrivileges`,
   syscall filtering.
2. **`hc-broker` (root, M1/M2, not built yet).** Listens on a root-owned Unix socket
   (`/run/homelab-control/broker.sock`, mode 0660, group `homelab-control`), authenticates the peer
   with `SO_PEERCRED`, and exposes a **closed list of typed operations** (e.g. `storage.smart.read
   {disk_id}`, `services.unit.status {unit}`). Each operation has a JSON Schema for arguments, a
   required scope, target revalidation (resolve stable IDs at execution time), fixed argv
   (`exec` without shell, absolute paths, scrubbed environment), timeout, output size cap, lock domain
   and an audit record. The API forwards the authenticated user and scope; the broker re-checks.
   There is no "run command" or "call Perl function" operation, ever.
3. **Legacy Webmin (`miniserv`, root, unchanged).** Remains a separate, optional UI with its own login
   on its own port. It is never proxied through `hc-api` (see ADR 0003).

## Options Considered

### Option A: Unprivileged API + typed broker (chosen)
| Dimension | Assessment |
|---|---|
| Complexity | Medium — two processes and an IPC schema |
| Security | Strong: root surface is a small, enumerable, testable set of operations |
| Upstream reuse | Indirect: Webmin parsers/logic are references; adapters are re-implemented or called via broker ops |
| Team familiarity | Go stdlib, systemd hardening; well understood |

**Pros:** small root TCB; easy to audit (`broker ops list`); compromise of the web tier yields only what
the operation list allows. **Cons:** every capability needs explicit adapter work; slower to reach parity.

### Option B: Run new API as root (Webmin model)
**Pros:** fastest parity. **Cons:** any web-tier bug is root; repeats upstream's bug class. Rejected.

### Option C: polkit/sudo rules per command
**Pros:** reuses OS mechanisms. **Cons:** sudo rules on raw commands are argument-injection prone,
distro-divergent, and hard to audit as typed operations; polkit is fine for D-Bus services (systemd,
UDisks2) and may be used *by the broker*, not instead of it. Rejected as the primary boundary.

### Option D: Bridge to Webmin RPC as root
Arbitrary function calls by design. Rejected (ADR 0003).

## Trade-off Analysis
We trade speed-to-parity for a root surface that can be listed, reviewed and tested. For a tool that
manages disks and networks on home servers exposed on LANs and tailnets, that is the right trade.

## Consequences
- Easier: security review, threat modeling, plugin permission review (plugins request operations, not
  root).
- Harder: every write feature needs a broker op, schema, tests and VM evidence.
- Revisit: whether some read-only data (e.g. SMART) can use group-readable device access instead of the
  broker.

## Action Items
1. [x] Guard tests that block process execution in all current Go code (M0).
2. [x] Hardened systemd unit for hc-api (M0, `packaging/common/homelab-control.service`).
3. [ ] Broker IPC schema + `SO_PEERCRED` auth + first read-only op (M1).
4. [ ] Locking/journaled operation state (M2).
