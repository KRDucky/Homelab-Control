# Native Reverse Proxy, ACME and DDNS Architecture

## Adapter contract
`detect()`, `capabilities()`, `inventory()`, `planCreateHost()`, `planEditHost()`, `planDeleteHost()`, `validate()`, `apply()`, `reload()`, `health()`, `logs()`; `apply` through typed broker only. Common resources: ProxyHost, Upstream, Route, Middleware/AccessPolicy, TLSCertificate, DNSProvider, ProviderInstance. Support native Caddy, Nginx, Traefik; do not require Docker.

## Backends
**Caddy:** Caddyfile or JSON admin API, choose one managed strategy; avoid conflicting with user-managed configuration. Keep Admin API bound to loopback/Unix where possible, never public. Validate before reload. DNS-01 may need custom DNS plugin build; don't assume distro package includes Cloudflare module.

**Nginx:** managed include directory, avoid rewriting main nginx.conf; validate `nginx -t`; ACME via Certbot/lego or central certificate service; safe graceful reload.

**Traefik:** static configuration ownership carefully separated from dynamic file provider; file provider for user-created routes, Docker provider optional and read-only by default; protect dashboard/API; built-in ACME; never expose Docker socket broadly.

**NPM:** use as inspiration or optional external integration, not a fourth reverse-proxy daemon. NPM is a manager around Nginx and typically ships as a Docker image. Native NPM-like UX is implemented in Homelab Control itself.

## Features
Hosts, paths, redirects, upstream pools, WebSocket/gRPC, HTTP/2/3 capability labels, TCP/UDP routes where supported, access lists, authentication middleware, IP allow/deny, headers, rate limiting, compression, logs, health, port-conflict detection, automatic rollback of *managed config* if validation fails. UI marks backend-specific features unavailable rather than silently dropping settings.

## Certificates and DNS
Central inventory: cert/key references, domain/SAN, issuer, expiration, renewal owner, deployment hooks. ACME HTTP-01 and DNS-01, staging by default in test, production after approval, wildcard only DNS-01. Prevent two managers renewing the same cert concurrently. Cloudflare DDNS plugin: zones, A/AAAA, proxied flag, IP detection strategy, scoped API token, history, rate limits, DNS propagation. Cloudflare Tunnel is separate integration.

## Threats
Admin API exposure, public dashboard, SSRF through upstream target and health checks, DNS rebinding, credentials in generated configs, certificate private key permissions, ACME rate limits, reload failures, misrouted traffic, wildcard DNS mistakes, port 80/443 conflicts, unintended firewall changes. No auto-public exposure or unverified external targets.
