# M8 review: mergerfs live configuration from its control file (2026-11-26)

Part of the M5–M9 review ("use the standard tools"). Not M8 acceptance.

## Finding

The page described a pool from the kernel mount table and fstab. On a real mount the kernel shows only the
generic FUSE options (`rw,user_id=0,group_id=0,default_permissions,allow_other` on all three guests), so the
pool's create policy and minfreespace were never shown, and the code that kept them from mountinfo only
ever matched test fixtures. With `fsname=` (common; mergerfs 2.40's own default source is `mergerfs`) the
branches came from fstab, which names only what the mount started with: a branch added or removed at
runtime was not seen.

mergerfs publishes its live configuration as extended attributes of `<mountpoint>/.mergerfs` (mergerfs
manual, "Runtime Config"). hc-api now reads `user.mergerfs.branches` (with RW/RO/NC mode and per-branch
minfreespace), `version`, `category.create`/`action`/`search`, `minfreespace` and `moveonenospc` with
`getxattr` and sets nothing. Each pool has 500 ms, the whole read 1.5 s, and at most 4 goroutines may park
on a hung mount. mountinfo and fstab remain fallbacks and the page says which source it used and why the
control file was not read. `parseOptions` still keeps those keys for fstab and systemd mounts, where they
are real.

Tests: `TestCollectUsesMergerfsRuntimeConfig` (fstab lists one branch, mergerfs two; fails without the
change), `TestReadRuntimesBoundedWhenMergerfsHangs`, `TestReadRuntimesUnreadableKeepsMountinfoBranches`,
`TestParseRuntimeBranches`, `TestRuntimeDropsUnknownVersion`, and two panel tests.

## VM validation

Installed `.deb` of `c8759d6`, `harness/mergerfs-check.py` (and `mergerfs-setup.sh` on guests without a
pool: one pool from an fstab line with `fsname=mergerfs,category.create=mfs,minfreespace=1G`), run as root.

| Guest | Kernel | mergerfs | Result |
|---|---|---|---|
| Ubuntu 26.04.1 LTS | 7.0.0-43 | 2.40.2-5build1 | 9/9 |
| Debian 13 (trixie) | 6.12.118+deb13 | 2.40.2-5 | 9/9 |
| Ubuntu 22.04.5 LTS | 5.15.0-203 | 2.33.3-1 | 9/9 |

Checks: branches from `runtime` equal `getfattr`; create policy, minfreespace, version equal `getfattr`;
a branch added with `setfattr -n user.mergerfs.branches -v '+>/mnt/disk3'` is shown (fstab does not name
it) and disappears when removed; a root pool under a 0700 directory gives no runtime, the reason, and no
claim of live branches; mergerfs stopped with SIGSTOP: the page answers in under 5 s (0.85 s) with "did
not answer in time", and reads it again after SIGCONT; no `mergerfs.read` → 403; no AppArmor denial.

VM findings fixed on the way: Ubuntu's build reports version `unknown` (no longer shown as a version), and
the unreadable reason now names both causes. Harness corrections: mergerfs turns `allow_other` on by
itself when root mounts it, and an ordinary user could not mount here, so the negative case uses a 0700
parent directory. Evidence: `/mnt/tank/claude-vms/artifacts/m8-mergerfs-runtime-f54464a/`.

Not covered: Ubuntu 24.04 and Debian 12; the page in a browser (Vitest covers the view).

## Graphify

Indexed `c8759d6`: 7212 nodes, no definition-only symbols, 7 self-loops. `readRuntimes` is called only
from `Collect`; `Pool.Branches` feeds the honesty warnings and capacity, which now see the live list. The
new `hasDotDot` duplicated `mountinfo.HasDotDot` and was replaced by it.
