# Security tests
M0 security tests live next to the code they protect so they run in `make test`:
- `backend/internal/api/server_test.go` — authn, RBAC, CSRF/Origin, fixation, headers, traversal
- `backend/internal/guard/guard_test.go` — no process execution, every route wrapped
- `backend/internal/auth/auth_test.go` — hashing, file permissions, symlinks, throttling, session expiry
- `backend/internal/audit/audit_test.go` — log injection, file mode
- `tests/integration/run-hc-e2e.sh` — the same checks against a live server
Repository-wide gate: `scripts/security-check.sh` (`make security`).
