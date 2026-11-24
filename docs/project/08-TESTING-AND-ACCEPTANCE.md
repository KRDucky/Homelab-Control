# Testing Strategy and Acceptance Criteria

## Test levels
- Unit: TypeScript components/hooks, Perl bridge, Go services, schema validation, parsers.
- Contract: OpenAPI schema, RBAC permissions, plugin/theme compatibility, distro package resolution.
- Integration: real Webmin fork sessions, systemd, package managers, Docker/libvirt adapters and storage tools on disposable VMs.
- Storage: loop devices, sparse files, virtual disks, simulated disk loss, ZFS/mdraid/Btrfs/mergerfs/SnapRAID fixtures; power-interruption and restart behavior.
- Hardware: SMART fixtures, SES mocks, unknown bay locations, missing GPU interfaces, fan watchdog/failsafe simulations.
- Browser: Playwright keyboard/accessibility, theme switching, CSRF/auth failure, error states, mobile and desktop layouts.
- Security: injection, path traversal, symlink races, privilege boundary, plugin supply chain, secrets redaction, hostile config imports.
- Packaging: install, upgrade, rollback, uninstall and coexistence/migration across supported distributions.

## Mandatory VM gate for full-function implementation (owner, 2026-11-23)

**Every milestone or module claiming full, functioning implementation requires end-to-end testing on real disposable VMs before acceptance.** This applies to read and write functionality, not only destructive operations, storage, packaging or M4. For each claimed supported distribution, install the actual build/package in a guest, run the feature through the supported API and WebUI as applicable, exercise the real system integration, and verify the observed outcome independently. VM guest tests supplement unit, contract, integration, browser and security tests; mocks, fixtures, containers and CI compilation alone cannot substitute for them.

For any feature with writes, test successful operation, refused/unauthorized access, error and lock paths, interrupted operation or restart recovery when relevant, and actual post-operation state. Read-only full functionality must likewise show truthful host data, missing capability behavior and permission failures on a real guest. Use only disposable guests and disposable virtual resources; never test mutating flows on the production MediaMouse/OpenBot host or real disks. Document guest isolation, reset/recreation method and any retained VM constraints before running.

A **design proposal, parser-only module, mock, read-only checkpoint or other expressly partial deliverable** may be reviewed and merged without this full-function gate, but must be labelled partial/unaccepted and must **not** claim full functionality, production readiness or milestone acceptance. This is a forward-looking project-wide standard; it does not retroactively change recorded owner acceptance or invent historical VM proof.

For every VM acceptance report record: source commit and built artifact identity; guest distribution, exact version and package-manager version; installation/upgrade method; test commands and UI/API steps; real outcomes, pass/fail counts, negative cases, sanitised logs/evidence and known limitations. A failed, unreachable or unrun VM is reported as unverified, never as passed. Where a module intentionally supports only Debian/Ubuntu, test those targets and mark other distributions unsupported, not pending as release blockers.

## Release gates
1. Read-only UI shows real host data or clearly labeled unsupported status.
2. No write API without server-side authorization and audit trail.
3. Destructive storage operations remain disabled until verified on disposable disks and reviewed.
4. Bay mapping provenance is explicit; unknown is never labeled verified.
5. Missing dependencies are explained and can be resolved through trusted, consented sources.
6. Network/proxy changes validate before reload and have recovery instructions.
7. Supported distro means tested, not merely theoretically compatible.
8. Theme/plugin install, disable, uninstall and compatibility failures are tested.
9. Any claimed full-function implementation passes the mandatory disposable-VM end-to-end gate above on each claimed supported distro; missing VM evidence prevents full-function or milestone acceptance.

## Evidence format
For every milestone provide commit hashes, test commands, passing/failing counts, environment, screenshots where appropriate, security considerations and known unsupported scenarios. Do not invent test results.
