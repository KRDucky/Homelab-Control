# Specification review: contradictions and ambiguities

Reviewed before writing code, against `CLAUDE.md` and `docs/project/00`–`10`, the project
`UI-CONCEPT.png`, the existing repository, and the upstream audit. Each item lists how M0 handled it;
items marked **Decision needed** are listed again in the M0 report.

| # | Conflict | Sources | M0 handling |
|---|---|---|---|
| 1 | The repository already contains a GPL-3.0 `LICENSE.md`, while `CLAUDE.md` says the final license is chosen *after* the dependency audit. | repo initial commit; CLAUDE.md rule 1 | Kept `LICENSE.md` untouched; ADR 0005 records the audit and recommends keeping GPL-3.0-or-later for the combined distribution. **Decision needed.** |
| 2 | Upstream Webmin is described as BSD, but the tree contains GPL-2.0+, GPL and LGPL files. | audit §2 | Recorded in `NOTICE.md` and ADR 0005; affects any non-GPL distribution choice. |
| 3 | Spec docs are expected at the repo root (`CLAUDE.md` reading order) but `00-PROJECT-SETUP.md` suggests `docs/project/`. | CLAUDE.md, 00 | Placed under `docs/project/`; root `CLAUDE.md` paths updated accordingly. |
| 4 | Layout in `02-ARCHITECTURE.md` has no SDK workspace and names `compat/webmin-perl/`; the M0 prompt asks for a separate SDK workspace. | 02, 10 | Added `sdk/`; upstream lives at `compat/webmin/` (pristine subtree) so a separate future `compat/bridge/` can hold adapter code without mixing it into upstream files. |
| 5 | "Webmin Perl bridge" is a host adapter, but Webmin's own bridge mechanisms (`rpc.cgi`, `fastrpc.cgi`, `xmlrpc.cgi`, `foreign_call`) are arbitrary root execution. | 02, 03, audit §6 | ADR 0003: no generic bridge; per-operation adapters only, executed by the broker. |
| 6 | Webmin themes are executable root Perl; the spec requires declarative, non-executable themes. | 06, audit §5 | New theme system does not reuse Webmin's theme interface; legacy UI keeps in-tree `gray-theme`. |
| 7 | Server-side RBAC scopes (03) vs. Webmin's per-module ACL files enforced inside CGIs. | 03, audit §6 | New RBAC is independent; no mapping from Webmin ACLs grants new-API scopes (ADR 0002). Migration tooling is M2. |
| 8 | `UI-CONCEPT.png` shows "Homelab Suite" with a **Cockpit fork/revamp** track and "HTMX/Alpine.js or React"; the specs say Webmin fork only (not a Cockpit plugin) and React. Its roadmap phases also differ from M0–M13. | UI-CONCEPT.png, 01, 02, 04 | Treated the image as a labeled mockup for visual direction only; specs win. Cockpit track is out of scope. **Confirm** the Cockpit track is dropped. |
| 9 | "Configure a disposable VM" (M0) vs. the environment actually available. | 10 | No KVM/hypervisor and no container registry access in the build environment. Baseline was tested in a **disposable cloud container** (destroyed after the session); a Vagrant VM definition is provided but **not executed** (`dev/vm/`). |
| 10 | 04 says packaging must be exercised from M0; 07 lists five targets and signing. | 04, 07 | Built and install/remove-tested an unsigned **dev `.deb` only**. Fedora/openSUSE spec and Arch PKGBUILD are drafts, untested. |
| 11 | SQLite is the preferred state store, but Go SQLite needs cgo or a large third-party module, and Go module downloads were blocked in this environment. | conventions, 02 | M0 has no persistent state beyond the users file and audit log; DB choice deferred to M2 (ADR 0004). |
| 12 | 03 lists `secrets.manage` etc. but no scope for reading legacy Webmin state or for the overview. | 03 | M0 uses `system.read` only. Full scope catalogue to be versioned in M2. |
| 13 | 08 release gate 1 ("read-only UI shows real host data") vs. 01 "No fabricated data" vs. the concept mockup's sample numbers. | 08, CLAUDE.md, UI-CONCEPT | UI shows only collected data; unknown values render as "Unknown"/"Unavailable" with sources; test fixtures are labeled. |
| 14 | `09-PROJECT-MEMORY.md` "Fork repository name and hosting organization" is listed as undecided, but the repo `KRDucky/Homelab-Control---Webmin-Fork` exists. | 09 | Used the existing repo; Go module path is the neutral `homelab-control/backend` until the final name is approved. |
| 15 | M1 in 04 already includes "authenticated modern UI shell", which M0 step 5 also asks for (conditionally). | 04, 10 | M0 delivers a minimal slice (login + overview). M1 extends it with the broker and legacy fallback. |
