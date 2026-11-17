# M0–M3 production-readiness status

**Reconciled:** 2026-11-17 against public `origin/main` at `59170f772e907d21a41842401c292c610e7ff4b3`.
**Verdict:** not production-ready; M2 is not closed, so M3 acceptance remains gated. This is a readiness
reconciliation, not owner acceptance, a deployment authorization, or a substitute for the milestone
acceptance records.

## Milestone state

| Milestone | Current evidence | Status |
|---|---|---|
| M0 | Webmin provenance/license audit, reproducible baseline, CI and threat model are recorded in the project docs. | Complete per project status; verify release artifacts separately before a production release. |
| M1 | Authenticated UI/API, compatibility boundary, and read-only host information are documented and merged. | Complete per project status; distro/host evidence has the limits listed in `docs/STATUS.md`. |
| M2 | PR-1 and M2-2 are merged. M2-3, M2-4, M2-5, M2-6 and the read-only file manager have working skeletons on main. | **Open; not accepted.** The required slices, security reviews, decisions, and acceptance evidence are incomplete. |
| M3 | Theme Development Kit is browser-local; the read-only sample plugin checkpoint is on main. | **Not accepted.** M2 must close first; M3 theme/plugin acceptance and production readiness are outstanding. |

## M2 close gates

The mandatory sequence and exclusions are in `docs/m2/M2-ROADMAP.md` and `docs/m2/OWNER-DECISIONS.md`.

| Slice | Current state | Remaining readiness work |
|---|---|---|
| PR-1 durable state/audit/logs | Merged read-only foundation. | Reconcile current CI and acceptance evidence with the roadmap; resolve any remaining audit durability/retention policy for production. |
| M2-2 service operations | Merged; acceptance record exists. | Preserve its security conditions; production activation remains a separate owner decision. |
| M2-3 service configuration | Skeleton on main; implementation and container evidence are documented, but not accepted. | Close owner review decisions and design review findings; run the prepared KVM acceptance procedure only with its required authorization; complete distro/security/browser evidence. |
| M2-4 storage inventory | Read-only metadata collector and fixture parsers exist. O-1 development and isolated validation are owner-approved; the separate helper remains inert, and the current `PrivateDevices=yes` unit cannot read devices. Child tools do not consume the opened fd, so it is not ready for activation. | Implement the approved read-only device policy and identity handling, then complete SELinux/AppArmor, KVM/systemd-container and real LVM-tool validation; live LVM/RAID/cache and hardware evidence are missing; ZFS/Btrfs/mergerfs scope and distro acceptance remain open. No storage write path or host/production activation is authorized by this slice. |
| M2-5 drive layout Stage A | Manual, explicitly unverified layout implementation; keyboard and drag/drop assignment, redacted export, additive import, and per-chassis optimistic concurrency are implemented in this isolated review branch. Existing-layout mutations carry versions and reject stale writes with 409; create/import remain additive. | D-12 is open pending HBA Viewer source/reuse terms. Browser/axe and backend acceptance are outstanding; no owner acceptance or merge is implied. |
| M2-6 hostname/timezone | Plan/apply backend and UI skeletons exist. A reviewed change now reports conflict when host settings converge between plan and apply. | Complete design/security review, user documentation, CI/browser and distro/KVM acceptance, and obtain owner acceptance. Go verification could not be repeated in this environment. No production apply is authorized. |
| File manager | Confined read-only roots and listing skeleton. | ADR 0012 and acceptance are open; complete security review and required isolated/browser/distribution evidence. No write operations are in scope. |

**M3 entry gate:** all six M2 roadmap slices above plus the file manager, their security checklists, and required
evidence must be accepted. Current main does not satisfy that gate.

## M3 acceptance work after M2

- Theme SDK: complete install, preview, switch, and remove behavior with persistence and supported-browser,
  security, accessibility, packaging, and upgrade evidence. Existing browser-local Theme Studio is not by
  itself M3 acceptance.
- Plugin SDK: expand beyond the compiled-in read-only sample only within an owner-reviewed trust model;
  complete load/unload behavior, persistence, isolation/security review, accessibility, and compatibility
  evidence. The current checkpoint deliberately has no executable plugin payload or server-wide installation.
- Reconcile ADR 0010 and the proposed plugin ADR with the implemented behavior and the release threat model.

## Production gates that remain separate

- Do not infer production activation from development authorization or acceptance. Owner approval is still
  required for deployment/activation and any production host changes.
- Complete security review and supported-distro/upgrade/rollback evidence for every accepted write path.
- Keep destructive storage write testing in disposable VMs/containers only after the execute path and security
  gates are ready. Never test writes on the MediaMouse/OpenBot host or production pool.
- Refresh `docs/STATUS.md`, release documentation, dependency/CVE review, and support/rollback instructions as
  slices are accepted. The existing status file is historical (its heading says 2026-11-07); it should not be
  treated as a current acceptance record without reconciliation.

## Immediate next sequence

1. Resolve D-12 (source/reuse terms for M2-5); complete the newly approved O-1 implementation and isolated
   evidence without enabling the socket on host/production or probing real host disks.
2. Complete M2-5 and M2-4 within their approved boundaries; do not infer hardware discovery or storage writes.
3. Close M2-3, M2-6 and file-manager reviews and acceptance evidence, then obtain the M2 close decision.
4. Only then finish and accept M3 Theme/Plugin SDK.
5. Run the production-readiness and deployment gates as a separate owner-approved release step.

## Source records

- `docs/STATUS.md` — implementation history and evidence (historical heading dated 2026-11-07).
- `docs/m2/M2-ROADMAP.md` — mandatory M2 sequence and acceptance matrix.
- `docs/m2/OWNER-DECISIONS.md` — approvals and unresolved owner decisions.
- `docs/m2/M2-3-ACCEPTANCE.md`, `M2-4-ACCEPTANCE.md`, `M2-5-STAGE-A-CONTRACT.md`,
  `M2-6-HOST-SETTINGS-SKELETON.md`, and `FILE-MANAGER-SKELETON.md` — slice evidence and limits.
- `docs/m3/PLUGIN-SDK-SKELETON.md` and `docs/project/04-ROADMAP.md` — current M3 scope and checkpoint.
