# Fork-specific patches to `compat/webmin`

`compat/webmin` is a `git subtree --squash` import of upstream Webmin (see
`compat/webmin.lock`). Every change made inside it must be listed here so that
upstream syncs can be reviewed and so fork divergence stays visible.

| ID | Date | Files | Reason | Upstreamable? | Upstream PR |
|---|---|---|---|---|---|
| — | — | — | No fork patches yet (M0). Tree is byte-identical to upstream 2.670. | — | — |

Rules
- Prefer fixing things outside `compat/webmin` (adapters, config, packaging).
- Security fixes go upstream first (security@webmin.com) when they affect upstream.
- Each patch is its own commit with subject prefix `compat/webmin:` and a `Patch-ID:` trailer.
