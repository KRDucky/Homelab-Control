# Upstream Webmin baseline 2.670 — unreleased security fixes and mitigations

**Checked:** 2026-10-16 (05:30 UTC) · **Decision:** keep the verified 2.670 baseline; no backports into `compat/webmin`.

## Is 2.680 released?
No. Verified against upstream directly:
- `git ls-remote --tags https://github.com/webmin/webmin.git` — newest tag **2.670**; no 2.680 tag.
- GitHub releases page — newest release **2.670 (21 Sep)**; its notes include module-name validation,
  File Manager ACL, Software Packages arbitrary-read, TLS client-certificate and Authentic-theme ACL fixes,
  all of which our pin already contains.
- The `version` file on upstream `master` (HEAD `19cd8a10`, 2026-10-15) still reads `2.670`, so a 2.680
  release is not imminent from the tree itself.

## Unreleased security-relevant fixes on upstream master since 2.670
Shallow fetch of `master` since 2026-09-20; commits whose subjects indicate a security fix (merges and
reverts omitted). None is in our pin. "Exposure" is for a host running Homelab Control M1 **plus**
an operator-installed upstream Webmin.

| Commit | Date | Fix | Exposure with M1 |
|---|---|---|---|
| `0678fbfa` | 10-15 | Fast RPC transfer authentication | **High** if any Webmin user has RPC enabled |
| `045c7abf` | 10-13 | Privileged RPC username conversion checks | **High** if RPC enabled |
| `5e90e143` | 10-08 | Module name validation (again; follows the 2.670 fix) | Medium — any authenticated Webmin user |
| `94436938` | 10-02 | Newline injection in Webmin user records | Medium — users who can edit Webmin users |
| `8742ccd3` | 10-02 | Custom Commands editor path injection | Medium — module-specific |
| `f8f35b26`, `0843de05` | 10-02, 10-16 | MySQL manual editor ACL bypass; MySQL backup privilege boundary | Module-specific |
| `5e4b9374` | 10-13 | SQL and LDAP backend permission checks | Module-specific |
| `2d014758`, `c23b6b5b`, `17ba87cb` | 10-10..12 | Cron job input validation; environment-variable ACLs | Module-specific |
| `cec6e607`, `ae8feb18` | 10-02, 10-16 | Mailbox attachment option injection; mail queue attachment XSS | Module-specific |
| `8f880ab3` | 10-02 | Squid ownership command escaping | Module-specific |
| `c57711a3`, `6c008506`, `3d0a9aee`, `a68dacad` | 09-27..10-11 | Missing ACL / permission checks | Unclear from subjects — review before 2.680 adoption |
| `0055cccf`/`8f79fbd0`, `623a5f0f` | 09-23..27 | Log-field escaping, username sanitising for logs | Low |

## Does Homelab Control itself expose these?
No new path. Per ADR 0003 (accepted 2026-10-16) Homelab Control never calls Webmin CGIs, `rpc.cgi`,
`fastrpc.cgi`, `xmlrpc.cgi` or `foreign_call`, and does not proxy Webmin. The only interaction is the
read-only broker op `legacy.webmin.status`, which reads `miniserv.conf`/ACL files (no execution,
no arguments). The risk is that of **upstream Webmin itself, when the operator runs it** alongside us.
Our `.deb`/`.rpm`/Arch packages do **not** install or start Webmin.

## Mitigations (documented, and partly surfaced by M1)
1. **Disable RPC for every Webmin user** (`rpc=0` in each `/etc/webmin/<user>.acl`). The M1 Legacy page
   already flags RPC-capable accounts (`rpc: warn`); treat that warning as blocking until 2.680.
2. **Bind Webmin to loopback** (`bind=127.0.0.1` in `miniserv.conf`) and reach it over SSH or a VPN. The
   Legacy page warns when it is bound to all interfaces.
3. **Give Webmin accounts to trusted administrators only**; do not hand out partial-module accounts
   (most fixes above are ACL bypasses between Webmin users).
4. Keep TLS and referer checks on (already reported by the Legacy page).
5. On 2.680 release: `scripts/sync-upstream.sh 2.680`, re-run `scripts/verify-upstream.sh`, the upstream
   `prove` suite and the M1 legacy checks, then move the pin (ADR 0007). Do not cherry-pick these fixes
   into `compat/webmin` beforehand: the subtree must stay byte-identical to an upstream release, and
   partial backports of unreleased commits would ship an untested combination.

## Blocks M2?
Not by itself, provided M2 keeps ADR 0003 (no Webmin RPC or CGI bridge). It **does** block any future
work that would call Webmin programmatically until the pin is moved past these fixes.
