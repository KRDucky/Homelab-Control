<!-- SPDX-FileCopyrightText: 2026 The Homelab Control Authors -->
<!-- SPDX-License-Identifier: GPL-3.0-or-later -->

# ADR-0015: Package source and signing trust (M4 proposal)

**Status: Proposed — NOT accepted.**
**Date:** 2026-11-23
**Related:** ADR 0006 and proposed ADR 0014; M4 acceptance gate.

## Context

The read-only Software Center lists configured package sources with `signed` and `third_party` heuristic fields. Those fields are **not** proof that metadata or package contents have valid signatures, nor permission to install. M4 calls for trusted source selection without silently adding repositories or importing keys.

## Proposed policy

1. **Default deny:** no source eligible for a transaction until its package-manager-specific trust chain can be verified against actual locally configured trust anchors. Unsupported formats, ambiguous signatures, key rollovers, missing keys or mixed-trust repositories fail closed.
2. Enumerate the selected repository's canonical ID, configured endpoints (sanitized), enabled state, vendor/third-party classification, effective signing policy, trust anchor fingerprints and verified metadata identity. Do not display credentials embedded in source URLs.
3. Prefer the distro's vendor-signed repositories using installed OS trust anchors. Third-party sources need an explicit owner-approved source and key fingerprint, reviewed via a separate source-enrollment flow; accepting one transaction must not silently approve a repository forever.
4. Refuse `trusted=yes`, disabled signature checks, unauthenticated or downgrade-prone metadata, arbitrary local package paths, AUR/foreign scripts, and unreviewed custom package-manager hooks for the initial write slice. A heuristic `signed=true` from M4-1b cannot satisfy this rule.
5. Bind a transaction plan to the exact repository configuration, key fingerprint(s), metadata snapshot, candidate package checksums/signatures and candidate-to-source mapping; revalidate before apply. Any change requires a new plan and confirmation. Package manager verification remains enabled through installation.
6. Surface third-party publisher risk and package maintainer scripts clearly. Signature verification confirms publisher provenance, **not** freedom from malicious or unsafe privileged scripts.
7. No built-in 'add source', import key, disable verification or metadata-refresh mutation is authorized by this ADR. Those need a later, separately reviewed operation and explicit operator consent.

## Verification gates — not executed

* Per-distro tests using signed vendor fixtures and adversarial sources: wrong/expired/revoked or changed keys, unsigned metadata, substituted package, missing checksums, cross-repo candidate selection, mirror changes, stale snapshots, credential-bearing source URL, and injected source IDs.
* Disposable VMs with actual configured repository and package-manager versions; capture command outputs and proofs of the effective signature policy.
* Verify read-only source labels remain descriptive heuristics until a distinct verified trust result is implemented. `packages.read` must never imply `packages.manage`.
* Refuse write enablement for a distro until source identity/signature evidence is reproducible and reviewed.

## Open decisions

Owner must decide which vendor trust anchors and distro/version pairs are acceptable for the first safe transaction, whether any third-party repository can be eligible, how offline cache freshness is defined, and how key rotations are handled. No automatic enrollment is permitted by this proposal.
