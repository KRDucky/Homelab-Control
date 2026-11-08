# M2 planning (2026-10-21)

Readiness review, security plan and roadmap for M2, with the owner's product-scope decisions of
2026-10-21. **Authorization status (2026-11-07):** M2 PR-1 (authorized 2026-10-21) and M2-2, service start/stop/restart
(authorized 2026-10-23, merged 2026-10-26), are authorized and merged. **M2-3 development and isolated testing were
authorized on 2026-11-04** (scope and owner items: [`OWNER-DECISIONS.md`](OWNER-DECISIONS.md#m2-3-development-authorization-2026-11-04));
its working skeleton is on `main` as PR #36. M2-4 through M2-6 development was authorized on 2026-11-05;
those working skeletons, plus OIDC and the read-only file manager, are on `main`. Owner acceptance,
production activation, and M2 close remain pending. ADR 0012, ADR 0008 Amendment 4, and the ADR 0011 M2-6
extension stay **Proposed**. The documents below are dated planning snapshots; current status is in
[`../STATUS.md`](../STATUS.md).

| Document | Content |
|---|---|
| [M2-READINESS.md](M2-READINESS.md) | Verified baseline, approved architecture, documented M2 scope, blockers B1–B5, risks, verdict |
| [M2-ROADMAP.md](M2-ROADMAP.md) | PR sequence (PR-1, M2-2…M2-6), test requirements, post-M2 module milestones |
| [M2-SECURITY-ACCEPTANCE.md](M2-SECURITY-ACCEPTANCE.md) | Testable criteria before and for privileged writes |
| [OWNER-DECISIONS.md](OWNER-DECISIONS.md) | Decisions D-1…D-12; D-1, D-6, D-7 approved 2026-10-21 |
| [M2-PR1-PROPOSAL.md](M2-PR1-PROPOSAL.md) | Exact scope and tests of the first implementation PR |
| [MODULE-ARCHITECTURE.md](MODULE-ARCHITECTURE.md) | Independent modules, ownership and conflict prevention, coordinated workflows, Physical Drive Layout Designer model and stages |
| [M2-3-DESIGN-REVIEW.md](M2-3-DESIGN-REVIEW.md) | M2-3 design, systemd probes and ADR 0011 review of boot-time enable/disable |
| [M2-3-ACCEPTANCE.md](M2-3-ACCEPTANCE.md) | M2-3 results and evidence, separated from owner acceptance |
| [CAPABILITY-MATRIX.md](CAPABILITY-MATRIX.md) | Per-technology discovery, lifecycle, privileges, backend, risks, dependencies, distro notes, milestone |
| [FILE-MANAGER-SKELETON.md](FILE-MANAGER-SKELETON.md) | Read-only file-manager first checkpoint (not accepted) |

**Summary:**
- **PR-1 authorized 2026-10-21** (D-1, D-6, D-7 approved). It is read-only: durable state, audit, sessions and service logs.
- **The first privileged write (PR M2-2)** was authorized on 2026-10-23 (D-2…D-5 approved, ADR 0011 accepted) and merged on
  2026-10-26 (PR #28). Results and open items, including the outstanding workstation-VM validation (E2, deferred by the
  owner): [`M2-2-ACCEPTANCE.md`](M2-2-ACCEPTANCE.md). Every later write op needs its own authorization and the checklist in
  `M2-SECURITY-ACCEPTANCE.md`.
