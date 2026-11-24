# M4 APT settings and mirror writes — checkpoint evidence (2026-11-24)

**Partial checkpoint. Not M4 acceptance, not a merge, not production activation.** Owner authorization for
these two writes: `docs/m2/OWNER-DECISIONS.md` ("APT settings and mirrors"). Design: ADR 0008 Amendment 10
(Proposed). Package install/remove/upgrade, `apt update`, repositories and keys stay unauthorized.

## Tested source and artifact

- Source `856d0031` (branch `feature/m4-apt-config-mirrors`, stacked on PR #161 head `c8032cf3`).
- Package `homelab-control_0.0.0+git.856d0031_amd64.deb`, sha256
  `9bf56f6687eeee879406435191bc73298955e6a57d03435d8564a45cbf8fa2f1`, built from a clean `git archive`
  (no `compat/`): Go binaries and the frontend locally, `hc-audit-retention` for `x86_64-unknown-linux-gnu` in the
  local `mediamouse/hc-rust-dev:1.100.0` image (the host has no cargo), then `packaging/debian/build-deb.sh`.
  `scripts/package-inventory.sh` passes.

## Code checks on `856d0031`

- `go test ./...` (non-root), `gofmt`, `go vet`: pass, including `internal/guard` (routes, OpenAPI, helper
  confinement `TestAptApplyHelperConfined`, registry and allow-list snapshots).
- Frontend: typecheck, ESLint, Prettier clean; Vitest 32 files / 302 tests pass.
- Not run locally: Rust lint/tests (no cargo), `make security`, `make repro-check`. CI runs these.

## Graphify review

| Pass | Indexed SHA | Scope | Nodes / edges | Result |
|---|---|---|---|---|
| 1 | `36ec3be8` (feature commit) | `backend/`, `frontend/app/src/`, AST only, 468 code files | 5433 / 21316 | 1 confirmed finding, 2 test gaps, method-call false positives |
| 2 | `ac7e299f` (after fixes) | same | 5435 / 21329 | no new finding |
| 3 | `856d0031` (= tested commit) | same | 5437 / 21338 | no new finding; `ValidCurrentURI` callers and tests linked |

Graph health: about 2006 dangling-endpoint edges (stdlib and external symbols), 285 collapsed duplicates,
0 missing endpoints. "No callers" for `offer`/`wasOffered` is the extractor's method-call gap (checked in source).

1. **Confirmed, fixed (`ac7e299f`).** The probe offered every candidate, so a direct API call could plan a
   switch to a stale or unreachable mirror. Only mirrors with status `ok` are plannable now (regression test
   fails without the fix).
2. **Test gaps, closed (`ac7e299f`).** `dialAptApply` (real socket client, protocol-version check) and
   `defaultCountry` (time zone → country) had no tests.

Other findings while building (each with a regression test that fails without the fix):
- `safefile.Write` (extracted from `hc-mergerfs-apply`) let the umask decide the mode; under the mergerfs unit's
  `UMask=0077` that writes `/etc/fstab` as 0600. It now sets the exact mode.
- `/etc/os-release` is a symlink on Debian and Ubuntu; reading it with the no-follow reader would have refused
  every request (`7f4e584f` reads it through `aptapply.ReadOSRelease`).
- A third-party line in `sources.list` that reuses the codename (Docker) was classified as the archive; the
  distribution's components now decide.
- Debian cloud images use `mirror+file:///etc/apt/mirrors/debian.list`; that exact form may now be replaced.
- `OpenAPI` `POST /files/delete/apply` had a missing brace (also on the base branch) that made the spec unparsable.

## Disposable VM end-to-end

Guests on qcow2 overlays of preserved base images (pool `claude-vms-zfs`), cloud-init with a per-VM client key
and a pinned per-VM host key, autostart off, rebooted after install so `apparmor.service` loads the packaged
profiles, shut down by ACPI after the run (disks and definitions kept). hc-api uses direct TLS on loopback.
Two accounts: `admin` (`packages.read,packages.configure,audit.read`) and `reader` (`packages.read`).

| Guest | Version | Kernel | apt / dpkg / systemd | Sources | Result |
|---|---|---|---|---|---|
| `m4cfg-u2604-856d0031` | Ubuntu 26.04.1 LTS | 7.0.0-43-generic | 3.2.1ubuntu0.1 / 1.23.7ubuntu1 / 259.9 | deb822 `ubuntu.sources` | 31/31 |
| `m4cfg-deb13-856d0031` | Debian 13 (trixie), UEFI Secure Boot firmware | 6.12.118+deb13-amd64 | 3.0.4 / 1.22.22 / 257.13 | deb822, `mirror+file` archive | 31/31 |
| `m4cfg-u2204-856d0031` | Ubuntu 22.04.5 LTS | 5.15.0-203-generic | 2.4.15ubuntu0.1 / 1.21.1ubuntu2.6 / 249.11 | one-line `sources.list` (7 archive lines) | 31/31 |
| Ubuntu 24.04 | — | — | — | — | **NOT RUN** (see below) |
| Debian 12 | — | — | — | — | **NOT RUN** (no verified base image on hand) |

Each run compares the product with independent observations: `apt-config dump` after apply; mode/owner of the
managed file; backups (0600) present; the source file diff limited to the planned lines and, in each, the URI
token; and an observer `apt-get update` (harness, not product) that fetched from the new mirror without errors.
Negative paths: `packages.read` cannot plan (403); a replayed token is refused; a hand edit between plan and
apply gives 409 and leaves the file as edited; a later `99` configuration file is reported as overriding;
a mirror not offered by a probe is refused (409); with the opt-in marker removed the helper does not start and
nothing is written; security entries are never switchable. The operation journal lists the writes.

Observed mirrors (country US): Ubuntu 26.04 probed 40 candidates (34 ok, 2 stale, 1 mismatch, 3 unreachable)
and switched `archive.ubuntu.com` → `mirror.nodesdirect.com`; Ubuntu 22.04 probed 40 (38 ok) and switched all
7 archive lines to `pubmirrors.dal.corespace.com`; Debian 13 probed 3 official candidates and replaced the cloud
mirror list with `deb.debian.org`.

VM-driven fixes: `82b17329` (AppArmor: Go runtime reads `/proc/<pid>/cgroup`), `856d0031` (AppArmor: huge page
size read on Ubuntu 22.04). Earlier runs are kept as evidence: `7f4e584f` (Ubuntu 26.04, 30/31) and `82b17329`
(Ubuntu 26.04 31/31; Ubuntu 22.04 29/31 — one harness check bug, fixed, plus the AppArmor read; Debian 13 did not
get an address with plain OVMF and now uses the Secure Boot firmware the M11 Debian run used).

Evidence on the VM host: `/mnt/tank/claude-vms/artifacts/m4-aptcfg-856d0031/` (harness, check logs, API JSON,
source files before/after, observer `apt-get update` output, AppArmor denial scan, helper journal). Passwords were
generated in the guests and never copied out.

## Not tested and known limits

- **Ubuntu 24.04:** `qemu-img` cannot open the preserved baseline `images/baselines/ubuntu-24.04-base.img`
  (mode 0400, owner `libvirt-qemu`); changing it needs owner approval. **Debian 12:** no base image on hand.
- The probe does not verify signatures; APT does at its next update with the unchanged key. The Ubuntu mirror
  list is plain HTTP.
- Package lists are not refreshed after a mirror change (not authorized).
- Fedora, openSUSE and Arch: not applicable (APT only).
