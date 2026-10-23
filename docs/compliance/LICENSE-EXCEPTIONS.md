# License exceptions register

Items that deviate from the default "original code under the project license, permissive third-party
dependencies with notices" and are knowingly accepted, each with its condition. An entry with
**Owner approval: pending** is proposed, not accepted. Owner decision 2026-10-16: EX-02…EX-06 accepted
subject to their conditions; the PCI fixture was replaced rather than kept (EX-05 closed).

| ID | Item | License | Why it is acceptable | Condition that keeps it acceptable | Review trigger | Owner approval |
|---|---|---|---|---|---|---|
| EX-01 | `compat/webmin/` reference subtree | Mixed per file: see `WEBMIN-LICENSE-INVENTORY.md` (incl. 835 third-party files with no stated licence) | Development reference only; never redistributed in packages or release source archives; upstream notices kept intact | Excluded from every package and from the release source archive (owner decision 2026-10-16); not modified to simplify licensing; `scripts/verify-upstream.sh` passes; inventory `--check` passes; `LicenseRef-Webmin-unverified` files never copied into Homelab Control | Any proposal to ship Webmin files, or `WEBMIN-INDEPENDENCE.md` phase 2 | Accepted as status quo (directive §2.1) |
| EX-02 | Dev-only MPL-2.0 packages (axe-core, lightningcss + platform binaries) | MPL-2.0 | File-level copyleft applies only on redistribution; used only at build/test time | Never bundled or packaged (source-map check, payload check); `axe.min.js` copied only into disposable test VMs | Any change that would ship them; lockfile changes (SBOM drift check) | **Accepted** 2026-10-16, subject to the condition |
| EX-03 | caniuse-lite data | CC-BY-4.0 | Build-time browser data; not shipped | Not bundled | As EX-02 | **Accepted** 2026-10-16, subject to the condition |
| EX-04 | UI fonts: Inter and JetBrains Mono variable fonts (Archivo until 2026-10-23) | OFL-1.1 | Permit bundling with software; no Reserved Font Name; full license shipped in `THIRD-PARTY-NOTICES.md` | Notices shipped; fonts never sold standalone; any added font follows the HCIS font licensing policy | A font whose licence or rights can't be verified (use the system stacks instead) | **Accepted** 2026-10-16 (Archivo); 2026-10-23 for Inter and JetBrains Mono (owner, HCIS step 2) |
| EX-05 | `pci.ids` 3-line excerpt in a hardware test fixture | GPL-2.0-or-later OR BSD-3-Clause (PCI ID Repository) | Factual identifiers in a test fixture; not shipped | Test-only | — | **Closed** 2026-10-16: replaced with synthetic data; parser test coverage increased (LA-07) |
| EX-06 | Screenshot of Webmin's login page in M0 evidence (`docs/evidence/screenshots/06-legacy-webmin-login.png`) | Depicts upstream Webmin UI | Evidence that the upstream baseline runs; documentation only | Not shipped; not used as a design asset | If evidence is ever packaged | **Accepted** 2026-10-16, subject to the condition |
| EX-07 | GPL tools in CI (shellcheck, reuse, lintian) and GPL package builders | GPL | Tools only; their output is not a derivative | Not redistributed | — | Not required |
