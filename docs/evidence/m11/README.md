# M11 read-only networking — first checkpoint evidence (2026-11-24)

**Partial checkpoint. Not M11 acceptance, not a merge, not production activation.** ADR 0008
Amendment 9 is Proposed. Firewall, bond and port-binding writes do not exist and are not authorized.

## Tested source and artifact

- Source: `0bc2a917` (branch `feat/m11-network-readonly`, from `origin/main` `0f17d108`). The commits after
  it change documentation only.
- Package: `homelab-control_0.0.0+git.0bc2a917_amd64.deb`, sha256
  `51688822d36e2f86e4c5d599176133cae85038cb4ed813f48a3523e7144724a0`, built from a clean `git archive`
  (no `compat/`) with `make frontend backend` and `packaging/debian/build-deb.sh`. Deviation:
  `hc-audit-retention` was built for `x86_64-unknown-linux-gnu` in the local `mediamouse/hc-rust-dev:1.100.0`
  image, because the musl target is not installed there and the host has no cargo. M11 does not touch it.

## Code checks on `0bc2a917`

- `go test ./...` (non-root): all packages pass; `gofmt`, `go vet` clean; `internal/guard` passes.
- Frontend: typecheck, ESLint, Prettier clean; Vitest 32 files / 304 tests pass.
- `make security`: every section that ran passed. Third-party notices failed only because cargo is absent on
  the host (`adler2` source not fetched); govulncheck, reuse, shellcheck, RustSec were skipped (not
  installed). CI runs these.
- OpenAPI parses as YAML. A pre-existing missing `}` in `POST /files/delete/apply` (on `origin/main`) was
  fixed.

## Graphify review

| Pass | Indexed SHA | Scope | Nodes / edges | Result |
|---|---|---|---|---|
| 1 | `1240cbd0` (= tested commit) | `backend/`, `frontend/app/src/`, AST only, 464 code files | 5383 / 20874 | 2 confirmed findings, 1 graph false positive |
| 2 | `0bc2a917` (= tested commit) | same, 465 code files | 5406 / 20968 | no new confirmed finding |

Graph health on both passes: about 1970 dangling-endpoint edges (references to stdlib and external
symbols) and about 279 collapsed duplicate edges. The extractor does not record `Handle:` references in
the broker `Registry` composite literal or some method calls, so "no callers" results were checked
against the source. Three Markdown files in scope were not extracted semantically (code review only).

Findings and disposition:

1. **Confirmed, fixed (`494faa10`).** `network.ports.list` and `network.firewall.config` had no direct
   tests. Added broker op tests (argument refusal, PID 1 table reads, fixed argv, systemd failure,
   cancellation) and an `Env.FirewallShow` seam.
2. **Confirmed, fixed (`494faa10`).** A `systemctl show` answer missing any firewall unit produced a
   partial state map that read as "no firewall service is active". It is now `unknown`; regression test
   `TestParseUnitStatesIncompleteIsUnknown`.
3. **False positive.** `fwRuleXML.text()` "no callers": it is called in `ParseFirewalldZone`.

## Disposable VM end-to-end

Guests on qcow2 overlays of preserved base images (pool `claude-vms-zfs`), cloud-init with a per-VM client
key and a pinned per-VM host key, autostart off, shut down by ACPI after the run. The installed package
was activated with the documented direct-TLS option (self-signed certificate in the guest). Fixtures were
created only inside the guests: a firewall backend, an 802.3ad bond managed by systemd-networkd on two
dummy links, a hand-made active-backup bond on two dummy links, a root listener and a `nobody` listener.

| Guest | Version | Kernel | dpkg / apt / systemd | API | UI (Playwright 1.63.0, Chromium) |
|---|---|---|---|---|---|
| `m11-ubuntu-0bc2a917-a` | Ubuntu 26.04.1 LTS | 7.0.0-43-generic | 1.23.7 / 3.2.1 / 259 | 47/47 | 15/15 |
| `m11-debian-0bc2a917-a` | Debian 13 (trixie), UEFI | 6.12.118+deb13-amd64 | 1.22.22 / 3.0.4 / 257 | 47/47 | 15/15 |

API checks compare the API with independent guest observations. TCP and UDP listener sets equal `ss`;
bonds agree with `/proc/net/bonding`. ufw rules and defaults agree with `ufw status verbose`. nftables
chains and policies agree with `nft -j list ruleset`. The negative paths cover:

- 401 without a session, 403 for the wrong scope (a ports-only user cannot read bonds or the firewall), and
  403 `csrf_rejected` without the CSRF token;
- `none` with an explanation when the backend is stopped;
- 404 when the module is disabled in `modules.json`, with its capability omitted;
- 503 `broker_unavailable` for ports when the broker is stopped, while bonds (unprivileged) still answer;
- audit records with counts or the backend only (no addresses, rule text or process names).

The UI checks open each page from the navigation and look for guest data. They also check that no write
control exists and that the 390 px layout has no page-level horizontal overflow.

Observed facts (Ubuntu / Debian):

- **Firewall:** Ubuntu showed ufw active, with nftables and iptables installed but not active. Debian showed
  nftables active, with ufw and iptables installed but not active.
- **Bonds:** `m11bond0` was 802.3ad with fast LACP rate and `layer3+4` hashing. Its renderer was
  `systemd-networkd`, observed, and no LACP partner was present on the dummy links. `m11ab0` was
  active-backup with renderer `unknown`.
- **Ports, ssh:22:** on Ubuntu it was owned by `systemd socket ssh.socket → ssh.service` (observed). On Debian
  `ssh.service` is not socket-activated, so the owner was not identified, because root is never inferred.
- **Ports, other listeners:** hc-api `:9443` showed `homelab-control.service`, and the `nobody` listener showed
  `m11-nobody-listener.service`, both inferred from the account. The full-capability root listener on
  `127.0.0.1:18081` was not identified.

VM-driven fixes (each reproduced by a regression test before the rerun):

- `00aecd93`: the broker read no fd table at all. The kernel ptrace check on `/proc/<pid>/fd` needs the
  reader's capabilities to cover the target's, and the broker has only `CAP_SETUID`/`CAP_SETGID`.
  `CAP_SYS_PTRACE` was **not** added. Owners now come from systemd's socket list (observed) and the
  account (inferred), labelled per owner.
- `0bc2a917`: `systemctl list-sockets --output=json` has no `type` field on Ubuntu 26.04, so the op now
  reads `Listen=` types with `systemctl show --property=Id,Listen`.

Earlier attempts are kept as evidence: `494faa10` (harness bugs, then the ptrace finding) and `00aecd93`
(Debian BIOS boot loop, then the missing `type` field). Their VMs are shut off and their disks kept.

Evidence on the VM host: `/mnt/tank/claude-vms/artifacts/m11-network-0bc2a917/{ubuntu-a,debian-a}/`
(console and check logs, API JSON, ss/nft/ufw/bonding captures, journals, screenshots). Passwords were
generated in the guests and never copied out.

## Known limitations

- The firewall view is the **saved configuration** of the active backend, not the live ruleset. Reading the
  live ruleset needs `CAP_NET_ADMIN` and the host network namespace in the broker, which needs its own review.
- Port owners for full-capability root daemons that are not socket-activated stay "not identified".
- Docker or Podman published ports appear as sockets owned by `docker-proxy` and similar processes. They are
  not mapped to containers, and ports that are only DNAT rules (userland proxy off) do not appear.
- bpfilter and XDP are not implemented. Fedora, openSUSE and Arch were not tested.
- The three new navigation entries use the generic module glyph, as most storage modules do.
