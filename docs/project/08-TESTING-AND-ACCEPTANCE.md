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

## Release gates
1. Read-only UI shows real host data or clearly labeled unsupported status.
2. No write API without server-side authorization and audit trail.
3. Destructive storage operations remain disabled until verified on disposable disks and reviewed.
4. Bay mapping provenance is explicit; unknown is never labeled verified.
5. Missing dependencies are explained and can be resolved through trusted, consented sources.
6. Network/proxy changes validate before reload and have recovery instructions.
7. Supported distro means tested, not merely theoretically compatible.
8. Theme/plugin install, disable, uninstall and compatibility failures are tested.

## Evidence format
For every milestone provide commit hashes, test commands, passing/failing counts, environment, screenshots where appropriate, security considerations and known unsupported scenarios. Do not invent test results.
