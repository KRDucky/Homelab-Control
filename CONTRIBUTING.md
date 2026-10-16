# Contributing to Homelab Control

> **Status: proposed policy (LA-14), awaiting owner approval.** Until approved, the sign-off requirement
> below is not enforced by CI, and existing commits are not required to carry it.

Thank you for helping. Please read `CLAUDE.md` (project rules), `docs/STATUS.md` (what is in scope now)
and `docs/dev/DEVELOPMENT.md` (build and test) first. Milestones are gated: work beyond the current
milestone needs the owner's approval before a pull request.

## Licence of contributions
- Contributions to the core are licensed **GPL-3.0-or-later**; contributions to `sdk/`, `schemas/`,
  `themes/`, `frontend/design-system/` and the theme schema/examples in `docs/ui/` are licensed **MIT**.
  `REUSE.toml` is the authoritative map. You keep your copyright; add yourself to `AUTHORS.md`.
- Put an SPDX header on every new source file, matching the area it is in:
  ```text
  // SPDX-FileCopyrightText: 2026 The Homelab Control Authors
  // SPDX-License-Identifier: GPL-3.0-or-later
  ```
  `reuse lint` (in `make security`) fails on files without licence information.
- `compat/webmin/` is upstream Webmin. Do not edit it except through the documented patch process
  (`compat/PATCHES.md`); changes there are contributed under Webmin's BSD-3-Clause licence.
- Do not copy code, icons, fonts, screenshots or text from other projects (including Webmin's Authentic
  theme, Unraid, TrueNAS or Cockpit) unless its licence is compatible **and** you record its origin in
  the pull request; new runtime dependencies need an ADR 0006 review and regenerated notices/SBOM.

## Developer Certificate of Origin (DCO)
Every commit must be signed off, certifying the Developer Certificate of Origin 1.1
(<https://developercertificate.org/>): in short, that you wrote the change or otherwise have the right
to submit it under the project's licence, and that you understand the contribution and your sign-off
are public and kept permanently.

```bash
git commit -s     # adds: Signed-off-by: Your Name <you@example.org>
```
Use your real name (or a consistent, identifiable name you are known by). The sign-off is a statement by
the human submitting the commit; nobody else, and no tool, may add it on your behalf.

## AI-assisted contributions
AI assistance is allowed. The human who signs off is the author responsible for the change:
- you have reviewed and understood every line and can explain it;
- you have checked it does not reproduce third-party code or text you cannot license (assistants can
  emit memorised code);
- you mark the commit with a trailer naming the tool, for example
  `Co-Authored-By: Claude <noreply@anthropic.com>` or `Assisted-by: <tool>`.

## Pull requests
- One focused change per PR, with tests and documentation; describe what you verified and how.
- `make check` must pass locally; CI must be green (lint, tests, security gate, packages, payload and
  licence checks, release dry run).
- Security-sensitive changes (broker operations, authentication, anything run as root) need an ADR or
  an explicit review; never add generic command execution (`CLAUDE.md`).
- No destructive tests on real disks; use the fixtures, loop devices and disposable VMs in `dev/`.

## Reporting security issues
Do not open a public issue. Private vulnerability reporting is **not yet enabled** on the repository
(checked 2026-10-16); enabling it (Settings → Code security) and adding a `SECURITY.md` is an owner action
proposed with this policy. Until then, contact the owner through their GitHub profile.

## Enforcement (after approval)
A CI check will require a `Signed-off-by:` line matching the commit author on every commit in a pull
request, for commits created after the approval date. Earlier history is grandfathered: all commits up to
the approval date were made by the project owner.
