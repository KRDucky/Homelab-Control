# M0–M3 production-readiness status

**Reconciled:** 2026-11-18 against `main` at `59170f772e907d21a41842401c292c610e7ff4b3` plus draft PR #118 on `dreggbot/m2-acceptance-integration`.
**Verdict:** not production-ready; M2 is not closed, so M3 acceptance remains gated. This is a readiness
reconciliation, not owner acceptance, a deployment authorization, or a substitute for the milestone
acceptance records.

## Milestone state

| Milestone | Current evidence | Status |
|---|---|---|
| M0 | Webmin provenance/license audit, reproducible baseline, CI and threat model are recorded in the project docs. | Complete per project status; verify release artifacts separately before a production release. |
| M1 | Authenticated UI/API, compatibility boundary, and read-only host information are documented and merged. | Complete per project status; distro/host evidence has the limits listed in `docs/STATUS.md`. |
| M2 | PR-1 and M2-2 are merged. Draft PR #118 contains the M2-4 read-only closeout evidence, M2-5 Stage A, M2-6 test repairs, and the File Manager UI/API. | **Open; not merged.** Exact-head CI and package provenance, authorized M2-3 KVM and distro evidence, M2-5/M2-6/File Manager owner acceptance and ADR dispositions, and final M2 close decision remain. |
| M3 | Theme Development Kit is browser-local; the read-only sample plugin checkpoint is on main. | **Not accepted.** M2 must close first; M3 theme/plugin acceptance and production readiness are outstanding. |

## M2 close gates

The mandatory sequence and exclusions are in `docs/m2/M2-ROADMAP.md` and `docs/m2/OWNER-DECISIONS.md`.

| Slice | Current state | Remaining readiness work |
|---|---|---|
| PR-1 durable state/audit/logs | Merged read-only foundation. | Reconcile current CI and acceptance evidence with the roadmap; resolve any remaining audit durability/retention policy for production. |
| M2-2 service operations | Merged; acceptance record exists. | Preserve its security conditions; production activation remains a separate owner decision. |
| M2-3 service configuration | KVM Phase A and B and opening PR #118 are authorized (2026-11-18). No M2-3 guest run occurred. The pinned 40801659 artifact is stale for this candidate. | Wait for exact-head CI packages and provenance, then run the authorized tests in Claude's disposable VMs and close remaining owner/design/distro/security/browser evidence. Do not use the older artifact. |
| M2-4 storage inventory | **Accepted 2026-11-18 for the scoped read-only inventory** on the isolated branch. Native LVM metadata is wired through helper, broker, API graph and UI; Fedora 44 SELinux and Debian 13 AppArmor enforcing guest probes passed. The final closeout candidate also registers storage with the module switch; its new disable-path tests remain unexecuted. Full evidence and explicit limits are in `M2-4-ACCEPTANCE.md`. Helper remains disabled by default. | Verify the new module-gate tests on the exact candidate. No host/production activation. Ubuntu LTS, systemd-nspawn, broader distro/CI coverage, real vendor hardware and additional AppArmor negative/device variants remain deployment coverage follow-ups. A narrower by-id design and separate authorization are required before host activation. |
| M2-5 drive layout Stage A | PR #118 includes manual assignment, import/export and versioned changes. Write responses redact identities without `storage.read`; export contains geometry and an `assigned` boolean only; import accepts that redacted shape and has an 8 MiB cap; chassis create/update have a 1 MiB cap; drops require the custom MIME type and an idle UI. Exact integrated CI is pending. | Owner acceptance remains open; hardware/controller discovery is outside Stage A. |
| M2-6 hostname/timezone | Plan/apply backend and UI skeletons exist. DeepSeek reports focused Go/UI tests, vet, typecheck and lint passing, including confirmation-cancel coverage; Change Reviewer found no actionable issue in that commit delta. | ADR 0008 Amendment 4 and ADR 0011 extension remain Proposed; D-Bus read integration, CI/browser-e2e, distro/KVM evidence, user documentation and owner acceptance remain. No production apply is authorized. |
| File manager | Confined read-only API has a navigation and metadata UI. UI escapes control/format characters and backslashes, isolates paths, keys collision-prone entries by index, and surfaces omitted names. Backend uses a labelled digest for deep or unsafe audit paths, reports omitted-name counts, and refuses non-searchable directories without misclassifying unlink races. Prior browser/axe evidence is from an isolated branch, not the integrated candidate. | Exact integrated CI and browser/axe verification, ADR 0012 and owner acceptance remain open. No write operations are in scope. |

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

1. Finish exact-head CI, package provenance and authorized Claude VM tests for the current PR.
2. Keep M2-5 within Stage A; do not infer hardware discovery or storage writes.
3. Close M2-3, M2-5, M2-6 and File Manager evidence/reviews, then obtain owner acceptance and the M2 close decision.
4. Only then finish and accept M3 Theme/Plugin SDK.
5. Run the production-readiness and deployment gates as a separate owner-approved release step.

## Source records

- `docs/STATUS.md` — implementation history and evidence (historical heading dated 2026-11-07).
- `docs/m2/M2-ROADMAP.md` — mandatory M2 sequence and acceptance matrix.
- `docs/m2/OWNER-DECISIONS.md` — approvals and unresolved owner decisions.
- `docs/m2/M2-3-ACCEPTANCE.md`, `M2-4-ACCEPTANCE.md`, `M2-5-STAGE-A-CONTRACT.md`,
  `M2-6-HOST-SETTINGS-SKELETON.md`, and `FILE-MANAGER-SKELETON.md` — slice evidence and limits.
- `docs/m3/PLUGIN-SDK-SKELETON.md` and `docs/project/04-ROADMAP.md` — current M3 scope and checkpoint.
