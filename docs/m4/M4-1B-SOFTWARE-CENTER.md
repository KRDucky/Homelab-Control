# M4-1b Software Center checkpoint

**Status:** Proposed. This note records scope only. It does not authorize implementation, accept an ADR, or accept M4.

## Checkpoint

The first Software Center checkpoint is read-only. It covers package search, installed packages, pending-update previews, and configured-source views. Package-manager commands use closed argv templates with stdlib parsing, as recorded for D-11 in [the owner decision register](../m2/OWNER-DECISIONS.md).

The checkpoint does not install, remove, or update packages. It does not add or change repositories, import keys, or refresh metadata. Those writes need separate owner approval.

## Authorization boundary

- `packages.read` is the scope for read operations.
- `packages.manage` remains unused until a separate write ADR.
- The Software Center is a `modules.json` module, not a core route.
- ADR 0008 Amendment 5 is Proposed. It does not authorize package writes or constitute M4 acceptance. ADR 0014 is reserved for the later package-transaction write ADR and is not written here.

## Non-goals

This checkpoint does not provide a package transaction, dependency resolver, repository mutation, or live package operation. It does not establish M4 acceptance or authorize changes on a production host or retained VM.
