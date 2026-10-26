<!--
SPDX-FileCopyrightText: 2026 The Homelab Control Authors

SPDX-License-Identifier: GPL-3.0-or-later
-->

# Typed broker operations, including writes

How to add or change an `hc-broker` operation. Read ADR 0008 (broker), ADR 0011 (write
authorization) and `docs/m2/M2-SECURITY-ACCEPTANCE.md` first. **Every new operation needs owner
authorization and security review; write operations additionally need their own ADR 0011 review.**

## Where things live

| Package | Role |
|---|---|
| `internal/broker/ops` | The closed registry (`Registry()`), handlers, strict argument structs |
| `internal/broker/runner` | The only package that executes programs: allow-listed binaries, fixed argv, unprivileged children, and the closed **root template** table (`RootTemplate`) |
| `internal/broker/writes` | Plan tokens (mint/consume), per-target locks, the operation journal (intent, outcome, interrupted) |
| `internal/servicepolicy` | Pure policy code for service units: names, protected categories, dangerous targets |
| `internal/broker/server` | Framing, peer uid check, scope re-check, per-call `Env` (actor; root handle only for `root-exec` ops), broker audit (fail closed) |
| `internal/api` | HTTP routes (`Server.handle`: session, scope, Origin, CSRF), first-tier validation, API audit |
| `schemas/openapi/homelab-control.v1.yaml` | Every route with its `x-scope`; `internal/guard` checks the contract |

## Read-only operation checklist
1. Typed args struct, decoded with `proto.DecodeArgs` (unknown fields, trailing data rejected).
2. Validate every value against a conservative grammar in the API **and** in the handler.
3. Fixed file paths (`O_NOFOLLOW`, size caps) or a fixed argv through `env.Runner`.
4. `AuditArgs` returns a summary of *validated* arguments only, never secrets or raw input.
5. Register with name, scope (`<module>.<level>`), privilege (`root-read` or `unprivileged-exec`),
   timeout and description; update `TestRegistryIsClosed`.

## Write operation checklist (ADR 0011)
A write is two ops: `<name>.plan` (unprivileged, read-only) and `<name>.apply` (`root-exec`).

**Plan handler**
- Require `env.Actor.User` and `env.Actor.Session`.
- Resolve the target to its **canonical, stable identity** (unit ID; WWN/serial for disks, never
  `/dev/sdX`) and apply the policy to every name it is known by.
- Compute a state digest covering everything whose change should invalidate the review, including
  the set of other resources the operation affects.
- Classify the risk (`reversible`, `disruptive`, `destructive`). Anything other than reversible
  requires step-up; the broker enforces it in `writes.Consume`.
- Mint the token with `env.Writes.Mint(writes.Binding{…})` and return a preview: effect,
  consequences, affected resources, observed state, risk, expiry. Never promise rollback.

**Apply handler**, in this order (see `unitApply` in `ops/services_write.go`):
1. `env.Writes.Consume(token, writes.Request{…})` — verifies the MAC, binding, expiry and step-up and
   removes the token atomically. Map `ErrStepUp` → `step_up_required`, anything else → `invalid_token`.
2. `env.Writes.Lock(domain)` and `defer unlock()`; `ErrBusy` → `busy`.
3. Re-read the target, recompute the digest and the policy; mismatch → `conflict`, nothing done.
4. `env.Writes.Intent(record)` — if it fails, return `unavailable` and do **not** execute.
5. Execute through `env.RootExec.Run(ctx, template, target)` with a context independent of the
   request, so a slow client cannot cut the operation off half-way.
6. Read the state back and classify the **observed** outcome; never report success without it.
7. `env.Writes.Outcome(record)`; return the outcome with recovery guidance on failure.

**Root templates** (`runner/runner.go`): add an entry to `rootTemplates` with the allow-listed binary,
the fixed argv ending in `--`, and a target validator written independently of the op's own
validation. Update `TestRootTemplatesAreClosed`, `TestRegistryIsClosed` and, if a new file needs the
handle, the reviewed list in `guard.TestRootExecConfined`. Never accept a binary, flag or free-form
argument from the caller; never use a shell.

**API**: route through `Server.handle` with the op's scope; strict JSON (`decodeStrict`); validate
before calling the broker; write the API audit record **before** the apply call and refuse if it
cannot be written; add the route to OpenAPI with `x-scope`. Passwords stay in `hc-api`; the broker
only receives `step_up_at`.

**Tests** (each is required by §4 of the acceptance checklist): argument-injection table and a fuzz
target (add it to the CI fuzz step); authorization in the API and the broker; replay, expiry and every
binding field; lock contention and a panicking handler; revalidation; journal fail-closed and
interrupted recovery; outcome classification on failure; and a real-environment scenario on two
distribution families (see `tests/systemd/m2_service_ops.py`).

## Testing against a fake systemd
`ops.Env.Units` accepts any `UnitController`. The tests in `ops/services_write_test.go` use an
in-memory fake (`fakeUnits`) that resolves aliases, records actions and can inject failures, so the
whole plan/apply path runs without systemd and without root. Real-systemd coverage is in
`tests/systemd/m2-service-ops.sh`, run by `tests/systemd/systemd-smoke.sh` inside a disposable host
(`HC_SMOKE_EXTRA_CHECKS`).
