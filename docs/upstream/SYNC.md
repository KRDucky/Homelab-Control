# Upstream Webmin baseline and sync procedure

## Current pin
| Field | Value |
|---|---|
| Upstream | https://github.com/webmin/webmin |
| Tag | `2.670` (released 2026-09-21) |
| Commit | `3a2b2b8671c0669c129f5275b3dd1853106ae02b` |
| Tree | `a8482cc848f28a8e4c806861f237a2094514f533` (verified byte-identical by `scripts/verify-upstream.sh`) |
| Fork patches | none (`compat/PATCHES.md`) |

Why 2.670: latest released tag; above the 2.650 floor for CVE-2026-49243; includes the 2.670 security
fixes (module-name validation, File Manager ACL path bypass, Software Packages arbitrary file read, TLS
client-certificate verification, Custom Commands quoting). See `docs/audit/WEBMIN-2.670-AUDIT.md` §1 for
the post-2.670 security commits that motivate moving to **2.680** when it is tagged.

## Watching for security fixes
1. Watch releases and `SECURITY.md` on webmin/webmin and authentic-theme (GitHub "Releases only" +
   "Security alerts" watch).
2. Weekly (or on any advisory): `git fetch https://github.com/webmin/webmin master` into a scratch clone
   and run `git log --oneline <pinned-tag>..FETCH_HEAD | grep -iE 'secur|inject|travers|xss|escap|acl|bypass|cve|valid'`.
3. Record notable items in this file under "Known exposure" with the upstream commit IDs.

## Sync procedure (new upstream tag)
```bash
git switch -c upstream/webmin-2.680
scripts/sync-upstream.sh 2.680          # squashed subtree pull + updates compat/webmin.lock
git show --stat HEAD                    # review scope
git diff HEAD~1 -- compat/webmin/miniserv.pl compat/webmin/web-lib-funcs.pl compat/webmin/miniserv-lib.pl
scripts/verify-upstream.sh              # must report OK or list recorded patches
make check                              # lint, unit tests, security gate
# disposable baseline test (never on a production host):
dev/disposable/webmin-baseline.sh && tests/integration/webmin-baseline-login.sh
```
Checklist before merging:
- [ ] Upstream changelog and SECURITY.md reviewed; new advisories added to the audit doc.
- [ ] Every entry in `compat/PATCHES.md` re-applied or dropped (with reason) — upstream may have fixed it.
- [ ] `t/` suite (`prove -lr`) passes in the disposable environment; evidence saved under `docs/evidence/`.
- [ ] Module inventory regenerated if modules were added/removed.
- [ ] Release notes mention the new upstream version.

## Emergency security fix before an upstream release
Cherry-pick the upstream commit into `compat/webmin` as its own commit
(`compat/webmin: backport <sha> <subject>`), add a `P<n>` row to `compat/PATCHES.md` marked
"upstream backport", and drop it at the next sync.

## Known exposure at the current pin
Post-2.670 upstream security commits not yet in a release: `616547c26`, `c23b6b5b4`, `17ba87cbc`,
`c57711a3b`, `6c008506d`, `5e90e1438`, `f8f35b26c`, `8742ccd31`, `94436938d`, `cec6e6074`, `8f880ab36`,
`dda9b972b`, `623a5f0fa`. They affect the **legacy Webmin UI only**; the M0 API does not execute Webmin code.
