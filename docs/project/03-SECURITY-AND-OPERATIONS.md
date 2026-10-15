# Security, Privileges and Operational Safety

## Threat model
Web UI receives untrusted input and runs near sensitive host resources. Threats: CSRF, XSS, plugin compromise, arbitrary command injection, path traversal, symlink attacks, privilege escalation, package supply-chain compromise, secret leakage, mistaken disk identity, interrupted destructive operations, network lockout.

## Controls
- Authenticated sessions; secure cookies, CSRF protection, origin checks, XSS-safe rendering and CSP where practical.
- Explicit RBAC scopes: `system.read`, `packages.manage`, `storage.read`, `storage.plan`, `storage.execute`, `proxy.manage`, `secrets.manage`, `identity.manage`, etc. Enforce server-side on every endpoint.
- Narrow privileged broker with typed operations and exact target validation; no generic root shell RPC.
- Read-only by default; require a plan/preview, affected resource graph, stable-ID confirmation and confirmation for risky operations.
- Exclusive locks for conflicting storage, package, firewall and network changes. Persistent operation state and audit history.
- For remote networking changes, provide timed rollback/reconnect safeguard where feasible; otherwise clearly warn of lockout.
- Atomic config file replacement when applicable, validation before reload, backup of previous config, and recovery instructions.
- Explicitly distinguish dry-run from execution and reversible from irreversible steps.
- Plugins: verified provenance, scoped permissions, dependency review, install/disable/uninstall hooks; privileged adapters separately reviewed.
- Secrets: scoped Cloudflare tokens, ACME account keys, Tailscale auth keys, TLS private keys; never print secrets in UI logs or exports.
- Fan control: hardware allowlist, safe automatic fallback/watchdog, min RPM limits, thermal failsafe; unsupported devices monitoring-only.
- SnapRAID: do not auto-sync when disks are missing or recovery is unresolved; clarify parity is point-in-time.

## Release gates
Security tests, dependency/license scan, packaging provenance, update/rollback documentation, backup/restore of app configuration, test matrix sign-off. Destructive operations require VM test evidence and human review before enablement.
