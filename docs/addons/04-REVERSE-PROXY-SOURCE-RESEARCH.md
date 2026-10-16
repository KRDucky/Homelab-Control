# Reverse Proxy Source Research and Integration

## Objective
Study upstream projects and official documentation to design native proxy adapters and an NPM-style UI. **Do not vendor entire server codebases into Homelab Control** or treat a Docker-first application as a native package without a feasibility study. Pin a source tag/commit for any research, record license, NOTICE, security advisories, architecture, public APIs and configuration validation.

## Official source and documentation
| Project | Source | Documentation | Integration intent |
|---|---|---|---|
| Traefik | https://github.com/traefik/traefik | https://doc.traefik.io/traefik/ | Native binary/service; file provider, API (secured), ACME, routing/middleware |
| Nginx Proxy Manager (NPM) | https://github.com/NginxProxyManager/nginx-proxy-manager | https://nginxproxymanager.com/ | Study UX and feature semantics; NPM is a separate Docker-oriented manager, **not** a proxy engine |
| Caddy | https://github.com/caddyserver/caddy | https://caddyserver.com/docs/ | Native binary/service, Caddyfile/JSON config, Admin API (local-only), automatic HTTPS |
| Nginx | https://github.com/nginx/nginx | https://nginx.org/en/docs/ | Native service, configuration templates and `nginx -t` |
| Certbot | https://github.com/certbot/certbot | https://eff-certbot.readthedocs.io/ | ACME for Nginx/Apache where appropriate |
| lego | https://github.com/go-acme/lego | https://go-acme.github.io/lego/ | ACME/DNS provider reference; evaluate dependency/security/licensing |
| acme.sh | https://github.com/acmesh-official/acme.sh | https://github.com/acmesh-official/acme.sh/wiki | Optional external ACME client, not default curl-pipe-shell installer |
| Cloudflare Tunnel | https://github.com/cloudflare/cloudflared | https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/ | Separate connector from reverse proxy and DDNS |
| Cloudflare DNS API | https://developers.cloudflare.com/api/resources/dns/ | https://developers.cloudflare.com/dns/ | DDNS and DNS-01 API integration |

## Research instructions for Claude Code
1. Fetch metadata and docs from upstream official GitHub repos, using read-only checkout into `research/upstream/<name>` (gitignored) or temporary directory. Do not copy code into `compat/webmin`.
2. Capture pinned commit/tag, release compatibility, license/SPDX, NOTICE, trademarks, runtime requirements, supported native packages and documented APIs in `docs/research/<name>.md`.
3. Identify native integration surfaces: CLI validation, config file, REST API, service reload, systemd unit, logs, ACME, security controls.
4. For NPM, map UI concepts (proxy hosts, redirects, streams, certificates, access lists, user roles) to *our own* domain model; do not claim its backend can run without its runtime stack.
5. Assess integration vs code reuse: prefer clean-room adapter to daemon's public interfaces. Copy snippets only with attribution/license compliance and explicit review.
6. Document exact repo availability by distro, install provenance, version pinning, signing and supported upgrade path. Never add a third-party repo without operator approval.
7. Write contract tests using sample configs and disposable containers/VMs, without exposing admin API or public ports on the developer host.
8. Track licensing: Caddy Apache-2.0, Traefik MIT, NPM MIT according to official repository metadata at research time; re-check license files before redistribution. Nginx and plugins may differ.

## Example read-only source checkout commands (run only in approved workspace)
```bash
mkdir -p research/upstream
for repo in traefik/traefik NginxProxyManager/nginx-proxy-manager caddyserver/caddy nginx/nginx certbot/certbot go-acme/lego cloudflare/cloudflared; do
  name="${repo##*/}"
  git clone --filter=blob:none --depth=1 "https://github.com/$repo.git" "research/upstream/$name"
done
```
This is research, not installation or code import. Use pinned tags/commits for repeatable analyses; verify origin and LICENSE before copying anything.
