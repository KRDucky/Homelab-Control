# Development guide

## Requirements
| Tool | Version used in M0 | Notes |
|---|---|---|
| Go | 1.24.7 | `go.mod` requires ≥ 1.24 (stdlib `crypto/pbkdf2`). No third-party modules. |
| Node.js / npm | 22.22.0 / 10.9.4 | jsdom (dev-only) warns it prefers Node ≥ 22.22.2; tests pass on 22.22.0. |
| GNU make, git, bash, coreutils | any recent | |
| Perl 5 | 5.38 | Only for the legacy Webmin baseline and upstream `prove` suite. |
| Python 3 + Playwright (optional) | 1.x with Chromium | Browser e2e test only. |
| shellcheck, govulncheck (optional) | — | Run in CI; skipped locally if missing. |

On CachyOS/Arch: `sudo pacman -S go nodejs npm make git perl` (all official repos).
On Debian/Ubuntu the distro Go is often too old — use the official tarball after verifying its checksum.

## Layout
```
CLAUDE.md                 agent instructions (paths point at docs/project/)
compat/webmin/            pristine upstream Webmin 2.670 (git subtree --squash) — do not edit casually
compat/webmin.lock        pinned upstream tag/commit/tree; compat/PATCHES.md lists fork patches
backend/                  Go module: cmd/hc-api (server), cmd/hlctl (CLI), internal/{api,auth,audit,hostinfo,webui,guard}
frontend/app/             React + TypeScript + Vite UI shell
frontend/design-system/   CSS design tokens (theme seed)
sdk/  plugins/  themes/   extension SDK, first-party plugins, theme packs (M3; placeholders now)
schemas/openapi/          versioned API contract (v1)
packaging/                common systemd unit, Debian dev package, Fedora/openSUSE/Arch drafts
tests/                    integration (shell), e2e (Playwright), security, vm-fixtures
dev/                      disposable container script and Vagrant VM
docs/                     project spec, ADRs, audit, security, evidence, plans
scripts/                  verify/sync upstream, security gate, reproducibility, license report
```

## Build and test
```bash
make build          # npm ci --ignore-scripts, vite build, embed UI, go build -> build/hc-api, build/hlctl
make test           # Go unit/integration tests + Vitest
make lint           # gofmt, go vet, tsc, eslint, prettier --check
make security       # upstream provenance, guard tests, go vet, dependency policy, npm audit, secret scan
make check          # lint + test + security
make repro-check    # two clean builds of HEAD, compares SHA-256
make deb            # dev .deb in build/ (unsigned)
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
Frontend hot reload: run hc-api as above, then `cd frontend/app && npm run dev` and open
`http://127.0.0.1:5173/` (Vite proxies `/api` to 9180; the Origin check accepts the proxied host).

`-dev-insecure-http` is refused on non-loopback addresses. For LAN access use `-tls-cert/-tls-key`
or a local TLS reverse proxy with `-behind-tls-proxy -allowed-origins https://your-host`.

## Conventions
- Never add a route outside `Server.handle` (guard test enforces).
- No `os/exec` or process spawning anywhere in `backend/` until the broker exists (guard test enforces).
- UI: no inline styles/scripts (CSP), no raw HTML rendering (lint enforces), no fabricated values —
  show "Unknown"/"Unavailable" with the data source.
- Commits touching `compat/webmin/` must be listed in `compat/PATCHES.md`.
