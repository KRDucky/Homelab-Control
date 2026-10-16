# License exceptions register

Items that deviate from the default "original code under the project license, permissive third-party
dependencies with notices" and are knowingly accepted, each with its condition. An entry with
**Owner approval: pending** is proposed, not accepted.

| ID | Item | License | Why it is acceptable | Condition that keeps it acceptable | Review trigger | Owner approval |
|---|---|---|---|---|---|---|
| EX-01 | `compat/webmin/` reference subtree | BSD-3-Clause with GPL-2.0-or-later, GPL, LGPL and MIT files (LICENSE-AUDIT LA-08) | Development reference only; never redistributed in packages; upstream notices kept intact | Excluded from every package (`scripts/package-inventory.sh` fails otherwise); not modified to simplify licensing; `scripts/verify-upstream.sh` passes | Any proposal to ship Webmin files, or `WEBMIN-INDEPENDENCE.md` phase 2 | Accepted as status quo (directive §2.1) |
| EX-02 | Dev-only MPL-2.0 packages (axe-core, lightningcss + platform binaries) | MPL-2.0 | File-level copyleft applies only on redistribution; used only at build/test time | Never bundled or packaged (source-map check, payload check); `axe.min.js` copied only into disposable test VMs | Any change that would ship them; lockfile changes (SBOM drift check) | Pending (recommend accept) |
| EX-03 | caniuse-lite data | CC-BY-4.0 | Build-time browser data; not shipped | Not bundled | As EX-02 | Pending (recommend accept) |
| EX-04 | Archivo variable font in the UI | OFL-1.1 | Permits bundling with software; no Reserved Font Name; full license shipped in `THIRD-PARTY-NOTICES.md` | Notices shipped; fonts never sold standalone | Directive §5.2 preference for system fonts (LA-06) | Pending (keep, or replace with system fonts) |
| EX-05 | `pci.ids` 3-line excerpt in a hardware test fixture | GPL-2.0-or-later OR BSD-3-Clause (PCI ID Repository) | Factual identifiers in a test fixture; not shipped | Test-only | Before release: add attribution or replace with invented names (LA-07) | Pending |
| EX-06 | Screenshot of Webmin's login page in M0 evidence (`docs/evidence/screenshots/06-legacy-webmin-login.png`) | Depicts upstream Webmin UI | Evidence that the upstream baseline runs; documentation only | Not shipped; not used as a design asset | If evidence is ever packaged | Pending (recommend accept) |
| EX-07 | GPL-3.0 tools in CI (shellcheck) and GPL package builders | GPL | Tools only; their output is not a derivative | Not redistributed | — | Not required |
