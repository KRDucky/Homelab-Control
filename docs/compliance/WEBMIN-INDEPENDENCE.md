# Webmin independence — dependency and migration assessment

**Date:** 2026-10-16 · **Directive:** §2 (Webmin is a historical reference, not the application framework).
**Nothing in this document has been removed or changed in packaging.** Removal steps need owner approval.

## 1. Conclusion
- **Release packages already contain zero Webmin code or assets.** The DEB, RPM and Arch packages install
  three Go binaries, systemd units, a sysusers file, a default env file and four documents — nothing from
  `compat/webmin/` (verified per build by `scripts/package-inventory.sh`; `RELEASE-COMPLIANCE.md` §2).
- **Homelab Control does not need Webmin installed, and never runs Webmin code.** No package depends on
  Webmin; no binary links, embeds or executes anything from the subtree.
- What remains is a **development and test dependency on the `compat/webmin/` subtree**, used to test
  the optional "Legacy Webmin" detection against a real Webmin. That is where the migration work lies.

## 2. Dependency inventory
### Runtime (shipped code) — optional detection only
| Where | What it does | Uses Webmin code? | If Webmin is absent |
|---|---|---|---|
| `backend/internal/broker/ops/webmin.go` — broker op `legacy.webmin.status` (ADR 0008) | Reads `/etc/webmin/miniserv.conf`, usernames from `miniserv.users` (never hashes), per-user `.acl` RPC flags, `version`, and checks the pidfile's `cmdline` for `miniserv.pl` | No — reads files, executes nothing | Reports "not installed"; everything else works |
| `backend/internal/hostinfo/hostinfo.go` (legacy section) | Unprivileged read of `/etc/webmin/version` / `miniserv.conf` when readable | No | Overview says Webmin isn't installed |
| API `GET /api/v1/legacy/webmin`, UI "Legacy Webmin" page | Shows the detection result and security findings; optional link to an existing Webmin via `-legacy-url` | No | Page explains Webmin is not installed |
| `PinnedWebmin = "2.670"` constant | Compares an installed Webmin's version with the audited baseline | No (a version string) | Unused |

### Build time — none
`make build`, `make deb`, the RPM and Arch builders do not read `compat/`. The only build-adjacent use is
`make check` → `scripts/security-check.sh` → `scripts/verify-upstream.sh`, which checks that the subtree
still matches the pinned upstream tree (a maintenance check, not a build input).

### Packaging — none
No files from `compat/`, no `Depends`/`Requires` on Webmin. Package descriptions say the product coexists
with Webmin and does not modify `/etc/webmin` (true: the M1 rollback test checks Webmin keeps answering).

### Test and development time — the actual dependency
| Consumer | Use of `compat/webmin/` |
|---|---|
| CI `upstream-perl-tests` | Runs Webmin's own `prove -lr t` on the pinned baseline |
| CI `systemd-ubuntu`, `systemd-ubuntu-2604`; `tests/systemd/m1-checks.sh` (via `HC_WEBMIN_SRC`) | Installs the pinned Webmin into the disposable host to test real legacy detection |
| `dev/vm/run-m1-validation.sh`, `dev/vm/guest-validate.sh` | Same, in the workstation VM; also the coexistence/rollback checks |
| `tests/e2e/run-m1-e2e.sh`, `dev/disposable/systemd-container.sh` | Same, for browser tests in containers |
| `dev/disposable/webmin-baseline.sh`, `tests/integration/webmin-baseline-login.sh` | M0 baseline: does upstream Webmin itself install and log in |
| Unit tests (`broker/ops`, `hostinfo`) | **No** — synthetic `/etc/webmin` fixtures |

### Maintenance
`compat/webmin.lock`, `compat/PATCHES.md` (no patches), `scripts/sync-upstream.sh`,
`scripts/verify-upstream.sh`, `docs/upstream/`, ADR 0007.

## 3. Removal impact on accepted M1 functionality
| If… | Effect on M1 |
|---|---|
| `compat/webmin/` were deleted today | Product unaffected. CI `upstream-perl-tests` and every real-Webmin legacy-detection test would break (they would need another Webmin source). |
| The Legacy Webmin feature were removed | Loses accepted M1 functionality (legacy page, broker op, coexistence checks). **Not proposed.** It is detection of an operator-installed Webmin, not a dependency on Webmin. |

## 4. Migration (each phase needs owner approval)
**Owner decision 2026-10-16:** phase 1 is **approved for a separate implementation PR** (not this one).
The reference subtree stays where it is, and the accepted M1 Legacy Webmin functionality does not change.
Phases 2 and 3 are not approved.

| Phase | Change | Keeps M1 working? | Prerequisites |
|---|---|---|---|
| **0 — done** | Release packages free of Webmin; enforced in CI | Yes | — |
| **1 — approved, separate PR** | Tests obtain the pinned Webmin release from upstream (release tarball, SHA-256 pinned in `compat/webmin.lock`, upstream signature checked) into a cache instead of reading the subtree; unit fixtures unchanged | Yes | Confirm upstream signing key/provenance; network access in CI |
| **2** | Move `compat/webmin/` out of the main branch (archived tag or separate repository), keeping `webmin.lock`, ADR 0007 and the audit; `upstream-perl-tests` follows it or is dropped | Yes, after phase 1 | Owner decision that the reference subtree is no longer needed in-tree |
| **3** | Re-evaluate the Legacy Webmin feature as an optional plugin (M3 plugin SDK) rather than core | Product decision | Plugin SDK exists |

Recommended order: phase 1 when CI work is next scheduled; phase 2 only after phase 1 has run green for
a while; phase 3 belongs to product planning, not compliance.

## 5. Rules that stay in force
- No Webmin themes, icons, CSS, JavaScript, templates or branding in the new interface (LA-09: none today).
- No Webmin CGI, Perl modules, authentication or RPC as architecture (ADR 0003; no such path exists).
- `compat/webmin/` is not modified to simplify licensing; upstream notices stay intact (LA-08).
