# Project Memory Addendum — File Services & Proxy

**Date:** 2026-10-15. **Status:** requirements agreed for planning, not implemented.

## User requirements
Add native administration for Samba/SMB, NFS, WebDAV, iSCSI target and initiator, FTP, FTPS, SFTP, share users/groups/permissions, remote connections and persistent local mounts. Add native reverse-proxy adapters for Caddy, Nginx, Traefik, Let's Encrypt, Cloudflare DDNS and optional Tunnel. Study Nginx Proxy Manager source for UX/functionality; no Docker requirement for core proxy management. Missing tools must be detected and offered through approved distro package/repository workflows. Supported OS families: Ubuntu, Debian, Fedora, openSUSE, Arch.

## Decisions
Native daemon integration, no custom protocol reimplementation; typed broker operations; read-only first; no auto-public exposure; no silent repo addition; secrets protected; upstream source used for research rather than vendoring; legacy Webmin subtree pristine.

## Open decisions
WebDAV default Apache vs alternative, preferred FTPS backend, SFTP FUSE availability per distro, iSCSI multipath scope, LDAP/AD phase, config ownership/import semantics, ACME ownership across proxy providers, shared identity policy. Resolve via ADR before implementation.

## Verified repo baseline at planning time
Webmin fork repository `KRDucky/Homelab-Control---Webmin-Fork` had M0 merged; root `CLAUDE.md` and `docs/M1-PLAN.md` required owner approval before M1. These addenda do not assert new features have been implemented.
