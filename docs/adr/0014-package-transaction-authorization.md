<!-- SPDX-FileCopyrightText: 2026 The Homelab Control Authors -->
<!-- SPDX-License-Identifier: GPL-3.0-or-later -->

# ADR-0014: Package transaction authorization (M4 proposal)

**Status: Proposed — NOT accepted; no package writes authorized.**
**Date:** 2026-11-23
**Related:** ADR 0001, 0006, 0008, 0011, proposed package-source trust ADR 0015, `docs/project/04-ROADMAP.md`, issue #91.

## Context and boundary

The M4-1a parsers and M4-1b Software Center provide cache-oriented reads. They do not resolve, approve or execute package transactions. The read-only source flags are heuristics, not cryptographic verification. This ADR proposes the narrowly reviewed write path necessary for M4; it is a design checkpoint, not permission to implement or run it on any host.

The core broker exclusively owns process execution and mutation; Extensions may supply pure parsing and presentation but may not choose commands, privileges, paths, sources or grants. Unknown distro/tool versions fail closed.

## Proposed decision — separately authorize before implementation

* Add explicit `packages.manage` (known, never default, never implied by `packages.read` or admin-facing UI). Gate API and broker independently. Leave existing GET semantics unchanged.
* A package transaction has **two distinct phases**, `packages.transaction.plan` (non-mutating) and `packages.transaction.apply` (privileged). No generic argv, shell, install script, arbitrary repo URL, arbitrary package file path or arbitrary action.
* First approved action set, if accepted: install a named package from an already configured, trusted source; remove a named package; upgrade explicitly selected packages. Full-upgrade and repository/key mutations are separate decisions. Package identity uses verified distro/family plus strict package-name grammar and a canonical package identity returned by the resolver; reject option-looking names, path separators, control characters, unexpected aliases and ambiguous matches.
* Plan derives proposed installs, upgrades, removals, downgrades, versions, architectures, selected source, dependency additions, reverse-dependency removals, disk impact and scripts/hooks risk from the actual distro resolver. Never present a guessed dependency graph. If the resolver cannot reliably supply a complete preview, refuse.
* For the initial implementation prefer one explicitly approved distro/version pair; do not advertise four-family write support on the strength of M4 read parsers. Each distro needs its own exact command templates and fixture/VM evidence.
* Treat maintainer scripts, triggers and package-manager hooks as **arbitrary privileged code from the trusted package publisher**. A closed argv template does not remove that risk. Repositories not passing ADR 0015 cannot be selected.
* Plan must neither refresh metadata nor silently download packages; any refresh/download behavior requires separate, conspicuous approval and a documented networking boundary. A plan records metadata snapshot, candidate versions and source identity; do not silently substitute a newer candidate.
* Use existing ADR 0011 broker-minted single-use plan tokens (120-second expiry, actor+session+op binding), mandatory password step-up (all package actions classified disruptive), host-wide `packages` lock, durable fsync'ed intent before execution, durable outcome after, and explicit uncertain/interrupted statuses. No automatic retry or blanket rollback claim.
* At apply, re-read installed state, resolver output, repository trust/config and cache metadata under the broker's trusted execution context; compare a canonical digest of the complete plan, and reject any difference as conflict. Recheck scopes and token binding. Prevent concurrent metadata or source mutation from bypassing this check; where package-manager locking cannot guarantee it, fail closed.
* Exact executable path, argv, environment, cwd, stdin, network policy, timeouts, output limits, exit-status handling and package-manager locks belong in reviewed runner templates. No user-supplied flags. Never let an API/client-provided package name become a command option.
* Audit plan requests/refusals and apply intents/outcomes without package-list secrets or tokens; expose only reviewed summaries. The broker journal is authoritative when the client disconnects. Surface failures and reboot requirements honestly.
* No write controls in the current Software UI until this ADR and ADR 0015 are owner accepted and the VM acceptance gate has passed. Keep host installations (including MediaMouse/OpenBot) out of development verification.

## Acceptance and test plan (not executed)

1. Unit/negative tests: unsupported distro or version, no trusted source, unsigned/ambiguous candidate, unexpected dependencies, option injection, path/Unicode spoofing, stale cache, changed repository keys/config, tampered resolver output, excess dependency graph, insufficient disk, script/hook disclosure, wrong actor/session/scope, token replay/expiry, missing step-up and failed journal.
2. Contract tests: OpenAPI, scopes, audit-before-release and broker op/runner allowlist snapshots; no new executable imports in `hc-api` or Extensions; tamper tests for the compiled Extensions import boundary.
3. Disposable isolated VM matrix for each enabled distro/version: dependency installs, safe removals, upgrades, cancellation/interruption, reboot, lock contention, stale plan, repository/signing failures, audit and restart recovery. Inspect installed packages before and after; capture package-manager logs and broker journal. Tests must not touch host disks or production services.
4. Packaging and security checks on the built artifact, including installation and upgrades on supported release images. CI success alone is not owner acceptance.
5. Owner reviews command templates, observed resolver semantics, source-trust evidence, deviations and remaining risks **before** the write code is enabled; publish exact supported distro/version list and explicitly unsupported cases.

## Alternatives and unresolved choices

* Keep Software Center read-only indefinitely: safest, but does not satisfy M4.
* Delegate to arbitrary package-manager CLI via API: rejected; violates broker boundary.
* PackageKit or distro-native transaction APIs: revisit per distro only after reviewing privilege, preview fidelity and supply chain; do not substitute silently.

**Open for owner decision:** first write-enabled distro/version, whether removal and upgrade belong in the first slice, metadata refresh UX, script/hook risk presentation, resource limits and VM acceptance matrix. This ADR intentionally makes no production authorization.
