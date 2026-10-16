# ADR-0003: Legacy Webmin module bridge and migration

**Status:** Accepted (2026-10-16, owner) — architectural direction: coexistence plus audited per-operation adapters; no generic bridge or RPC.
> **Owner decision recorded 2026-10-16:** this acceptance is **not** authorization to implement privileged write operations. Any write-capable broker operation needs a separate, explicit owner approval (M2 design: confirmation, locking, audit, rollback).

**Date:** 2026-10-15
**Deciders:** Project owner

## Context
We must preserve useful Webmin functionality during migration and not rewrite the Perl backend first,
but we must never expose unrestricted legacy root access through new APIs. Webmin's native integration
points — CGIs running as root, `foreign_call`, `rpc.cgi`/`fastrpc.cgi`/`xmlrpc.cgi` — are generic
function-call mechanisms (audit §4, §6).

## Decision
1. **Coexistence, not proxying.** Upstream Webmin keeps running as its own service on its own port and
   login, linked from the new UI ("Legacy Webmin"). hc-api never reverse-proxies Webmin CGIs and never
   holds Webmin session cookies.
2. **No generic bridge.** There will be no endpoint that takes a module name, function name or CGI path.
   `rpc.cgi`, `fastrpc.cgi`, `xmlrpc.cgi` are never called by our code; packaging recommends
   `rpc=0` for all Webmin users.
3. **Per-operation adapters.** When a Webmin module's logic is worth reusing (e.g. parsers in
   `useradmin`, `mount`, `raid`, `software`), the broker gets a typed operation implemented either
   (a) natively in Go, using the Perl as a reference and parity-test oracle, or (b) as a small,
   reviewed Perl script under `compat/bridge/ops/<op>.pl` that `require`s specific Webmin libraries,
   takes JSON on stdin, validates it against the operation schema, calls one known function, and writes
   JSON. (b) runs only inside the broker, with fixed paths, no shell, and its own tests.
4. **Read-only first.** Bridged operations start read-only; writes need preflight, dry-run where
   possible, explicit confirmation and VM evidence (CLAUDE.md rules).
5. **Parity tests.** For each replaced module, fixture-driven tests compare new adapter output with the
   upstream parser on the same inputs.
6. **Retirement.** A legacy module is hidden from the default package only after the replacement passes
   parity and the release notes say so; the source stays in `compat/webmin`.

## Options Considered
| Option | Verdict |
|---|---|
| A. Coexist + typed per-op adapters (chosen) | Small root surface, incremental, testable |
| B. Reverse-proxy Webmin CGIs under the new UI | Inherits root CGI attack surface and cookie scope; mixed CSP; rejected |
| C. Generic JSON→`foreign_call` bridge | Arbitrary root code execution by construction; rejected |
| D. Rewrite all modules in Go first | Violates "do not rewrite the Perl backend first"; rejected |

## Consequences
- Easier: reasoning about what the new API can do (enumerable ops).
- Harder: some modules will stay legacy-only for a long time (see `MODULE-INVENTORY.md`).
- Revisit after M2: whether option (b) is needed at all or Go adapters suffice.

## Action Items
1. [x] M1: legacy link + detection of an installed upstream Webmin (version/port/TLS/bind/RPC posture via broker op `legacy.webmin.status`; Webmin files are read, never executed or modified).
2. [x] M1: hardware inventory implemented natively in Go (sysfs) instead of bridging `hardware-info` Perl; no Perl is executed by the new stack in M1.
3. [ ] M2: parity-test harness against `compat/webmin` parsers.
