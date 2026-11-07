# ADR-0012: Confined-root file listing in hc-api

**Status:** **Proposed** 2026-11-07 (file-manager first checkpoint). Not accepted. Not authorization for
file writes, content download, or a broker file operation.
**Date:** 2026-11-07
**Related:** ADR 0001 (hc-api does not spawn), ADR 0008 (no generic path-taking file-read), owner
2026-11-07 choice of the file manager after PR #42, `docs/m2/FILE-MANAGER-SKELETON.md`.

## Context

The M2 roadmap lists a separately authorized file manager as mandatory before M3. Webmin `filemin` is
never bridged (arbitrary command execution / root file access). A broker op that takes a caller path
and reads as root would be that design.

## Decision

1. **Unprivileged listing only**, in `hc-api`, of metadata (name, dir, size, owner, mode, mtime) under
   administrator allow-listed directories. No content, no editor, no archive extract, no mkdir / rename /
   chmod / upload / delete in this checkpoint.
2. **Confined roots** come from root-owned, not group/world-writable `/etc/homelab-control/files.json`
   (same trust class as `modules.json`). Missing, empty, unreadable or unsafe mode fails closed. `/`,
   `/etc`, `/root`, `/proc`, `/sys` and other users' homes are refused.
3. **`os.Root` + `O_NOFOLLOW`.** Canonicalize the jail; relative paths must stay inside it. Symlinks are
   not followed. `hc-api` still does not spawn. There is **no** new broker operation and **no** ADR 0008
   amendment.
4. **Scope `files.read`**, in `KnownScopes`, not `DefaultScopes`, implied by no other scope including
   `services.files`. Core routes: not registered in `modules.json`.
5. **CSRF + Origin** on identifier-bearing GETs. Audit records root id and relative path only.

## Consequences

- A later write slice needs its own owner authorization, ADR 0011 plan/confirm/apply, a separate
  `files.operate` / `files.configure` / `files.destroy` scope, and reviewed jailed verbs — not a generic
  path-taking root file op.
- Frontend FileBrowser is a later stacked slice.
