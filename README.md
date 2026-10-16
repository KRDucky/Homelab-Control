# Homelab Control — Webmin Fork

Homelab-first Linux server administration, forked from [Webmin](https://github.com/webmin/webmin):
a modern React/TypeScript UI and a versioned, least-privilege management API on top of the proven
Webmin codebase.

> **Status: Milestone M1.** What works today, all **read-only**: an unprivileged API and web UI with
> authenticated sessions; a host overview; a systemd services list; a hardware inventory with stable
> drive identity and confidence labels; and a legacy-Webmin status page with a security review. Root-level
> data comes from a small privileged broker that exposes four fixed, read-only operations and never runs
> commands as root. Nothing in this build changes your system. See [`docs/STATUS.md`](docs/STATUS.md).

## Quick start (development)
```bash
make build                       # needs Go >= 1.24, Node >= 22, make, git
mkdir -p .dev && chmod 700 .dev
read -rs PW && printf '%s\n' "$PW" | build/hlctl user add --file .dev/users.json --username admin; unset PW
build/hc-api -dev-insecure-http -users-file .dev/users.json -audit-log .dev/audit.jsonl
# open http://127.0.0.1:9180/
```
More: [`docs/dev/DEVELOPMENT.md`](docs/dev/DEVELOPMENT.md) · tests: `make check` · packaging: `make deb`.

## Repository map
| Path | What it is |
|---|---|
| `compat/webmin/` | Upstream Webmin 2.670, unmodified (BSD-3 + per-file licenses), pinned in `compat/webmin.lock` |
| `backend/` | Go API server `hc-api`, privileged broker `hc-broker`, admin CLI `hlctl` |
| `frontend/` | React UI shell and design tokens |
| `sdk/`, `plugins/`, `themes/`, `schemas/` | Extension SDK, plugins, themes, API/manifest schemas |
| `packaging/` | systemd units; dev packages for Debian/Ubuntu (.deb), Fedora/openSUSE (.rpm), Arch (.pkg.tar.zst) |
| `tests/`, `dev/` | Integration/e2e/security tests; disposable container and VM setup |
| `docs/` | Project spec, ADRs, upstream audit, threat model, plans, test evidence |

## Security model in one paragraph
The web-facing service runs as an unprivileged user, cannot spawn processes (enforced by tests), and
checks session, scope, Origin and CSRF token on every request. Root-level work will only ever happen in
a separate broker exposing a closed list of typed operations — never a shell or generic Webmin RPC
([ADR 0001](docs/adr/0001-privilege-boundary.md)). Upstream Webmin can keep running alongside as the
legacy UI.

## License
New code: GPL-3.0 (`LICENSE.md`), final split pending ([ADR 0005](docs/adr/0005-release-and-license-strategy.md)).
Upstream Webmin: BSD-3-Clause with some GPL/LGPL files, see [`NOTICE.md`](NOTICE.md). Homelab Control is
not affiliated with or endorsed by the Webmin project.
