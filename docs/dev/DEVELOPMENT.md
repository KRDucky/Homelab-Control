# Development guide

## Requirements
| Tool | Version used in M0 | Notes |
|---|---|---|
| Go | 1.24.7 (local), 1.26.10 (CI and releases, exact pin in `packaging/release/toolchain.env`) | `go.mod` requires ≥ 1.24 (stdlib `crypto/pbkdf2`). No third-party modules. Release/CI builds must use a *supported* Go line so stdlib security fixes apply; govulncheck in CI fails otherwise. |
| Node.js / npm | 22.22.0 / 10.9.4 | jsdom (dev-only) warns it prefers Node ≥ 22.22.2; tests pass on 22.22.0. |
| GNU make, git, bash, coreutils | any recent | |
| Perl 5 | 5.38 | Only for the legacy Webmin baseline and upstream `prove` suite. |
| Python 3 + Playwright (optional) | 1.x with Chromium | Browser e2e test only. |
| shellcheck 0.9.0, govulncheck v1.9.0 (optional) | — | Run in CI; skipped locally if missing. govulncheck v1.9.0 needs Go ≥ 1.26.0: `go install golang.org/x/vuln/cmd/govulncheck@v1.9.0`. |

On CachyOS/Arch: `sudo pacman -S go nodejs npm make git perl` (all official repos).
On Debian/Ubuntu the distro Go is often too old — use the official tarball after verifying its checksum.

## Layout
```
CLAUDE.md                 agent instructions (paths point at docs/project/)
compat/webmin/            pristine upstream Webmin 2.670 (git subtree --squash) — do not edit casually
compat/webmin.lock        pinned upstream tag/commit/tree; compat/PATCHES.md lists fork patches
backend/                  Go module: cmd/hc-api (server), cmd/hc-broker (root broker), cmd/hlctl (CLI),
                          internal/{api,auth,audit,hostinfo,hwinfo,webui,guard,version,brokerclient},
                          internal/broker/{proto,types,ops,runner,server}
frontend/app/             React + TypeScript + Vite UI shell
frontend/design-system/   CSS design tokens (theme seed)
sdk/  plugins/  themes/   extension SDK, first-party plugins, theme packs (M3; placeholders now)
schemas/openapi/          versioned API contract (v1)
packaging/                common systemd unit, Debian dev package, Fedora/openSUSE/Arch drafts
tests/                    integration (shell), e2e (Playwright), systemd (real-systemd smoke), packaging (install test), security, vm-fixtures
dev/                      disposable container script and libvirt VM harness (dev/vm)
docs/                     project spec, ADRs, audit, security, evidence, plans
scripts/                  verify/sync upstream, security gate, reproducibility, license report
```

## Build and test
```bash
make build          # npm ci --ignore-scripts, vite build, embed UI, go build -> build/hc-api, build/hlctl
make test           # Go unit/integration tests + Vitest
make lint           # gofmt, go vet, tsc, eslint, prettier --check
make security       # upstream provenance, guard tests, go vet, dependency policy, npm audit, notices/SBOM drift, reuse lint, secret scan
scripts/release-bundle.sh <ver> <dir>  # release DRY RUN: source archive, rebuild-from-archive check, packages, SHA256SUMS (publishes nothing; see BUILDING.md)
make check          # lint + test + security
make repro-check    # two clean builds of HEAD, compares SHA-256
make deb            # dev .deb in build/ (unsigned)
make rpm            # dev .rpm (Fedora/openSUSE) in build/ (unsigned)
make systemd-smoke  # package under real systemd in a disposable Ubuntu container (root, docker, debootstrap)
```
Run Go tests as a **non-root** user: one test checks permission handling and is skipped as root.

## Run locally (dev mode)
```bash
make build
mkdir -p .dev && chmod 700 .dev
read -rs PW && printf '%s\n' "$PW" | build/hlctl user add --file .dev/users.json --username admin; unset PW
build/hc-api -dev-insecure-http -listen 127.0.0.1:9180 \
  -users-file .dev/users.json -audit-log .dev/audit.jsonl \
  -legacy-url https://127.0.0.1:10000/       # optional link to upstream Webmin
# open http://127.0.0.1:9180/
```
With the broker (development, as root on a disposable machine; it refuses to start as non-root unless
`-dev-non-root` is given, in which case root-only ops report unreadable data):
```bash
sudo build/hc-broker -socket /tmp/hc-broker.sock -service-user "$USER" -audit-log .dev/broker-audit.jsonl
build/hc-api ... -broker-socket /tmp/hc-broker.sock
build/hlctl broker info --socket /tmp/hc-broker.sock      # lists the registered operations
```
Without a broker, Services shows as unavailable and Hardware/Legacy as limited — by design.

Service logs (M2 PR-1b) need the `services.logs` scope, which is never granted by default:
`build/hlctl user add ... --scopes system.read,services.read,services.logs`. The broker host must
run systemd with journald and have a `systemd-journal` group. Verify an audit log, with an optional
journal checkpoint (see `docs/security/AUDIT-LOG.md`):
```bash
build/hlctl audit verify --file .dev/audit.jsonl [--checkpoint SEQ:HASH]
```

Frontend hot reload: run hc-api as above, then `cd frontend/app && npm run dev` and open
`http://127.0.0.1:5173/` (Vite proxies `/api` to 9180; the Origin check accepts the proxied host).

`-dev-insecure-http` is refused on non-loopback addresses. For LAN access use `-tls-cert/-tls-key`
or a local TLS reverse proxy with `-behind-tls-proxy -allowed-origins https://your-host`.

## Conventions
- Never add a route outside `Server.handle` (guard test enforces).
- Process execution only in `internal/broker/runner` (guard test enforces); `hc-api`/`hlctl` must never
  link `broker/runner`, `broker/ops` or `broker/server` (import-graph guard test).
- New broker operations require: a registry entry, a scope, typed args, tests, an update to
  `TestRegistryIsClosedAndReadOnly`, the OpenAPI file, and security review (ADR 0008). Operations
  that take arguments also follow ADR 0008 Amendment 1 (strict decode, validation in both tiers,
  `AuditArgs` summary, fuzz test).
- Every API route must be in `schemas/openapi/homelab-control.v1.yaml` with its scope (contract test).
- UI: no inline styles/scripts (CSP), no raw HTML rendering (lint enforces), no fabricated values —
  show "Unknown"/"Unavailable" with the data source.
- Commits touching `compat/webmin/` must be listed in `compat/PATCHES.md`.

## Continuous integration
`.github/workflows/ci.yml` runs on every push to `main` and every pull request, and can be started by hand:
**Actions** tab → workflow **ci** → **Run workflow** → choose the branch → **Run workflow**. A manual run
takes no inputs and runs exactly the same 20 jobs as push/PR CI: lint, tests, security gate, reproducible build,
packages and payload checks, install matrix, real-systemd tests, browser e2e, upstream Perl tests, release
dry run and reproducibility comparison. It needs a branch that contains this trigger (on the default branch
it appears once merged). The token is read-only and nothing is published.

Actions are official `actions/*` releases pinned by commit SHA (ADR 0006); update them by changing the SHA
and the tag comment together after reading the release notes. `release.yml` and `release-build.yml` must use
the same SHAs (`tests/release/test_workflow_policy.py` fails otherwise).

The release dry run (`release-dry-run`) calls the reusable `.github/workflows/release-build.yml`, the same
workflow the manual **release** workflow uses, with the exact toolchain in
`packaging/release/toolchain.env`. Changing a pin there is a reviewed change: the dry run on that PR shows
whether the new toolchain still reproduces. `make test-release` runs the release tooling unit tests, the
workflow policy tests (PyYAML needed; skipped locally without it) and the package version-ordering checks.
Releasing is documented in `docs/release/RELEASING.md`.
