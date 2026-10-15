# Upstream Webmin audit — baseline 2.670

**Audited:** 2026-10-15, from the actual upstream source (`git clone https://github.com/webmin/webmin`,
master at `dda9b972b`, 2026-10-14) and tag `2.670` (`3a2b2b8671c0669c129f5275b3dd1853106ae02b`,
2026-09-21). Authentic theme audited from `https://github.com/webmin/authentic-theme` at
`1677c4962` (theme version 26.70). Statements below cite files in `compat/webmin/` unless noted.

## 1. Release state and baseline recommendation

| Item | Finding |
|---|---|
| Latest release tag | `2.670` (2026-09-21). Previous: `2.660` (2026-08-21), `2.653` (2026-07-27). |
| Unreleased work | 135 commits on master after 2.670; `CHANGELOG.md` lists **2.680 (October, 2026)** as in progress. |
| Security-relevant commits after 2.670 | `616547c26` root-capable RPC user creation, `c23b6b5b4`/`17ba87cbc` Cron env-var ACLs, `c57711a3b`/`6c008506d` missing ACL checks, `5e90e1438` module-name validation, `f8f35b26c` MySQL editor ACL bypass, `8742ccd31` Custom Commands path injection, `94436938d` newline injection in Webmin user records, `cec6e6074` mailbox attachment option injection, `8f880ab36` Squid escaping, `dda9b972b` SVG attachments in mail queue, `623a5f0fa` log field escaping. |
| Minimum safe floor | ≥ 2.650 (CVE-2026-49243, critical reflected XSS → command execution in Webmin Configuration, fixed in 2.650). Also CVE-2026-42210 (2FA bypass via Basic auth, fixed 2.640), CVE-2026-56020 (client-cert DN spoofing, see SECURITY.md entry for 2.202/2.301 and vendor notes for 2.641), CVE-2026-22678 (stored XSS, fixed 2.641). |

**Recommendation:** pin **2.670** (latest *released*, signed-tag-equivalent, matches what distro users
install from upstream repos). Do **not** pin an arbitrary master commit: unreleased code has had no
release QA. Treat the post-2.670 security commits above as a *known exposure list*; move the pin to
**2.680 as soon as it is tagged** (procedure: `docs/upstream/SYNC.md`). If 2.680 slips, cherry-pick
the listed commits as recorded fork patches rather than tracking master.

M0 does not expose any upstream Webmin code path through the new API, so the exposure applies only to
users who also run the legacy UI.

## 2. License

- Top-level `LICENCE`: **BSD 3-Clause**, Copyright (c) Jamie Cameron. Retained unmodified.
- **Not everything in the tree is BSD.** Files carrying other licenses:

| Path | License stated in file |
|---|---|
| `dhcpd/{edit,save}_{zones,text,keys}.cgi` (6 files) | GPL-2.0-or-later |
| `openslp/*` (10 files) | GPL (version per file header) |
| `samba/smbhash.pl` | GPL-2.0-or-later |
| `filemin/images/icons/` (Faenza icon set) | GPL (see `filemin/images/icons/COPYING`, GPL-3 text) |
| `vendor_perl/QRCode/Encoder/{QRSpec,Matrix}.pm` | LGPL |
| `filemin/unauthenticated/dropdown/fg.menu.js` | MIT/GPL dual |
| `webmin/acme_tiny.py` | MIT (Daniel Roesler) |
| Bundled JS: jQuery/jQuery UI, jstree (+jQuery 3.6.0 in bacula-backup), Quill, highlight.js 11.7.0, bootstrap-hover-dropdown | MIT / BSD-3 (per upstream projects) |

  Consequence: a binary distribution of the full Webmin tree is a combination that must honour the GPL
  for those files. This matters for ADR 0005.
- Authentic theme (separate repo): **MIT**, Copyright (c) Ilia Rostovtsev; ships minified Bootstrap 3,
  Font Awesome and other bundled assets that would need their own audit. Not vendored in M0.

## 3. Build system and packaging

- No compile step: Perl CGI + `miniserv.pl` (Perl HTTP server). Install via `setup.sh` (interactive or
  env-driven), which writes `/etc/webmin/miniserv.conf`, `config`, `miniserv.users`, `webmin.acl`,
  start/stop scripts, then `thirdparty.pl`/`oschooser.pl` adapt modules to the detected OS.
- Release tooling: `makedist.pl` (tarball from an explicit file list), `makedebian.pl`, `makerpm.pl`,
  `makemoduledeb.pl`, `makemodulerpm.pl`. Upstream CI builds/uploads packages through the external
  workflow `webmin/webmin-ci-cd` (`.github/workflows/webmin.dev+webmin.yml`) with signing secrets.
- Upstream tests: `t/*.t` run with `prove -lr` (`.github/workflows/tests.yml`; installs Perl::Critic).
  **Verified here:** 26 files, 4161 tests, all pass at 2.670 (`docs/evidence/webmin-2.670-prove.txt`).
- Shebangs are `#!/usr/local/bin/perl`; `setup.sh`/`perlpath.pl` rewrite them. Skipping that step
  (`noperlpath=1`) leaves miniserv unable to start on distros without `/usr/local/bin/perl`
  (observed during the baseline test).
- Repo setup scripts (`webmin-setup-repo.sh`, `setup-repos.sh`) add the upstream apt/yum repos and
  keys — these are exactly the "third-party repository" actions our rules require consent for; the fork
  must never run them implicitly.

## 4. Module interface

- A module is a directory with `module.info` (`name`, `desc`, `category`, `os_support`, `depends`,
  `readonly`, `library`, …), per-OS `config-<os>` defaults, `defaultacl`, optional
  `acl_security.pl` (`acl_security_form`/`acl_security_save`), `lang/`, CGIs, and `*-lib.pl`.
- Cross-module calls: `foreign_require(module)` / `foreign_call(module, func, args)`
  (`web-lib-funcs.pl` l.4688/4759). Remote calls: `remote_foreign_require/call` via `rpc.cgi`,
  `fastrpc.cgi`, `xmlrpc.cgi`.
- 126 modules with `module.info` at 2.670; inventory and fork disposition in
  `docs/audit/MODULE-INVENTORY.md`.

## 5. Theme interface

- A theme is a directory with `theme.info` (`desc`, `version`, `depends`, `webmin=1`, `usermin=1`,
  optional `spa=1`, `websockets=1`, `config_link`) plus Perl (`theme.pl`, CGIs) that overrides UI
  functions and page chrome. In-tree themes: `gray-theme` ("Framed Theme"), `blue-theme`, plus
  `defaulttheme`. Authentic (`spa=1`) is the upstream default in release packages but lives in a
  separate repo.
- **Themes are executable Perl running as root.** SECURITY.md records two Authentic-theme root
  command-execution bugs (≤1.800) and the 2.670 changelog adds "theme authorization to use explicit
  Webmin module ACLs". This is incompatible with our rule that themes are declarative and
  non-executable; the new theme system (M3) is CSS-token based and does not reuse this interface.

## 6. Authentication, sessions and authorization

- `miniserv.pl` runs **as root** and executes every CGI as root; module code then applies Webmin ACLs
  itself (`get_module_acl`, `foreign_available`, `can_*` helpers). **The security boundary for
  less-privileged Webmin users is per-CGI Perl code**, which is why SECURITY.md has a long history of
  "less-privileged user → root" bugs (CVE-2022-30708, CVE-2024-12828, File Manager CVE-2022-0824/0829).
- Users: `miniserv.users` (`user:crypt-hash:…`, yescrypt/SHA-512 crypt), optional PAM (`Authen::PAM`)
  or Unix auth; optional TOTP 2FA.
- Sessions: `session=1`; random `sid` cookie; session DB is a DBM keyed by a hash of the session ID
  (`hash_session_id`); cookie flags `httpOnly` and `SameSite=Lax` by default, `secure` with SSL
  (`miniserv-lib.pl` l.3238–3254).
- CSRF/XSS defence: **Referer-header checking** (`referer_check` in `web-lib-funcs.pl` ~l.6056) plus
  SameSite=Lax. No per-request CSRF tokens. `referers_none=1` is written by `setup.sh` (it was missing
  for `setup.pl` installs before 1.973, CVE-2021-31760..2).
- RPC: `rpc.cgi`/`fastrpc.cgi` let a user with `rpc=1` (or, as a fallback, any user named
  `root`/`admin`/`sysadm` — `webmin_user_can_rpc`, l.14336) call arbitrary module functions,
  i.e. **arbitrary root execution by design**. `setup.sh` (l.673–675) grants `rpc=1` to the initial
  login when it is not `root`/`admin`; upstream commit `616547c26` (post-2.670) tightens this.

**Implication for the fork:** none of these interfaces can be exposed through the new API. The new API
authenticates and authorizes itself and reaches privileged operations only through the typed broker
(ADR 0001, 0002, 0003).

## 7. Package/system integration

- systemd unit (`webmin-systemd`), init/OpenRC scripts, PAM service files (`webmin-pam`,
  `webmin-debian-pam`). Default port 10000, TLS via `miniserv.pem` self-signed key.
- Default bind is all interfaces. The disposable test bound it to `127.0.0.1`.
- Module OS support through `os_list.txt` + `config-<os>` files; Arch/CachyOS map to generic configs.

## 8. Security advisory history (summary)

From `SECURITY.md` plus 2026 CVE records: recurring classes are **XSS in admin pages**, **ACL bypass by
less-privileged users leading to root**, **authentication bypasses in miniserv** (PAM conversation,
proxy-trusted client certificates, Basic auth skipping 2FA), and **supply-chain compromise**
(1.890/1.900 backdoor inserted on the build server, CVE-2019-15231). Fork controls mapped to each
class are in `docs/security/THREAT-MODEL.md`.

## 9. Safe integration path — conclusion

A safe M0 path exists **only** if the new stack does not depend on Webmin for authentication or
privilege: the read-only overview is served by an unprivileged Go service reading procfs/sysfs, with
its own sessions. That is what M0 implements. Bridging Webmin module *functionality* requires the
privileged broker and per-operation adapters (M1/M2); proxying Webmin CGIs or RPC is rejected.
