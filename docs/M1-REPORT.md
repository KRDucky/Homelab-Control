# M1 completion report

**Implemented:** privileged broker (ADR 0008) with four read-only typed ops; systemd services list;
hardware inventory with WWN/serial/by-id identity + confidence; legacy Webmin status with security review
(TLS, bind, referer checks, RPC-capable accounts, version vs pin); capabilities API; capability-aware UI;
hardened broker units; deb/rpm/Arch builders; systemd smoke, no-systemd install test, browser+axe e2e;
cross-distro CI with SHA-pinned actions; OpenAPI + contract test.

**Defects found and fixed during M1 validation:** `make lint` failed on fresh clone (M0); overlay root
omitted from overview; client lost broker rejections; systemctl-present-but-not-booted reported as
available; openSUSE RPM dependency name; git hash treated as Debian release version; secret-scan hit a
test fixture; virtual disks labelled HDD.

**Defects found during final verification (2026-10-16):** CI security gate failed on `main` since M0 —
govulncheck found a reachable stdlib `net/http` advisory and Go 1.24 is EOL; CI now builds with Go 1.26.x.
**Arch package bug:** `post_install` ran before pacman's sysusers hook, so `chgrp` silently failed and
`/etc/homelab-control` stayed `root:root 0750` (service could not read its config); fixed. The `.deb`
matrix used `dpkg -i`, which cannot resolve `adduser` on current minimal images; now `apt-get install`.
systemd containers failed on cgroup-v2 hosts and on 26.04's tmpfs `/tmp`; fixed. shellcheck 0.9 SC2015 ×2.

**Evidence (commit `da41162a`):** see `docs/evidence/m1/README.md`. Local: Go 95/95 (non-root), Vitest 15/15,
live API 15/15, no-systemd install 14/14, reproducible build. CI run 38852873505: all 15 jobs pass — 9-distro
install matrix (incl. Ubuntu 26.04), real systemd on Ubuntu 24.04 and 26.04, browser e2e, govulncheck,
upstream Webmin tests. The earlier figures (systemd 37/37, browser+axe 22/22) were never committed and are
withdrawn; the axe scan has not been re-run.

**Security concerns:** a compromised hc-api can call any registered op (all are read-only and
argument-free — keep it that way until M2 adds confirmation + locking); DMI serials and Webmin usernames
are now visible to authorised users; legacy Webmin 2.670 lacks post-release upstream fixes.

**Remaining work / decisions:** re-run the axe/full-stack browser suite (`run-m1-e2e.sh`) on a host with
Docker Hub access, or add it to CI; VM validation on your hardware; approve ADR 0001/0003/0005/0008; PAM login; move to Webmin 2.680.
Do not start M2 without approval.
