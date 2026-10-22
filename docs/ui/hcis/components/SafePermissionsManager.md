# SafePermissionsManager

Docker Permissions Manager: choose bind-mounted directories, owner by name/UID, group by name/GID, recursion, separate directory and file modes, then a dry-run with counts and warnings.

**Status: Proposed (HCIS).** New in the HCIS specification; a reference design, not in the app.

**Implementation: awaiting authorization.** Anything here that writes to the host belongs to M2+ and needs a separately approved PR. Previews never execute operations.

Source: none in the repository.

## When to use

Warns about protected paths, mount boundaries (not crossed), symlinks (not followed) and ACLs. Built to be reused by Storage and File Sharing.

## Rules

- A mode bar always says whether you are editing, previewing or executing.
- Not every image honours PUID/PGID; the UI says so.

Props: `SafePermissionsManagerProps` in `reference/index.d.ts`. Preview data is fictional and labelled “Example data”.
