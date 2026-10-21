# M2 planning (2026-10-21)

Readiness review, security plan and roadmap for M2, with the owner's product-scope decisions of
2026-10-21. **M2 PR-1 is authorized (2026-10-21); everything else, including privileged writes, is not.**

| Document | Content |
|---|---|
| [M2-READINESS.md](M2-READINESS.md) | Verified baseline, approved architecture, documented M2 scope, blockers B1–B5, risks, verdict |
| [M2-ROADMAP.md](M2-ROADMAP.md) | PR sequence (PR-1, M2-2…M2-6), test requirements, post-M2 module milestones |
| [M2-SECURITY-ACCEPTANCE.md](M2-SECURITY-ACCEPTANCE.md) | Testable criteria before and for privileged writes |
| [OWNER-DECISIONS.md](OWNER-DECISIONS.md) | Decisions D-1…D-12; D-1, D-6, D-7 approved 2026-10-21 |
| [M2-PR1-PROPOSAL.md](M2-PR1-PROPOSAL.md) | Exact scope and tests of the first implementation PR |
| [MODULE-ARCHITECTURE.md](MODULE-ARCHITECTURE.md) | Independent modules, ownership and conflict prevention, coordinated workflows, Physical Drive Layout Designer model and stages |
| [CAPABILITY-MATRIX.md](CAPABILITY-MATRIX.md) | Per-technology discovery, lifecycle, privileges, backend, risks, dependencies, distro notes, milestone |

**Summary:**
- **PR-1 authorized 2026-10-21** (D-1, D-6, D-7 approved). It is read-only: durable state, audit, sessions and service logs.
- **The first privileged write (PR M2-2)** needs D-2…D-5, ADR 0011 and the checklist in `M2-SECURITY-ACCEPTANCE.md`.
