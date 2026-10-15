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

**Evidence:** see `docs/evidence/m1/README.md` (Go 94/94, UI 15/15, systemd 37/37 ×2, install 14/14 ×2,
browser 22/22 with zero WCAG AA violations, reproducible build).

**Security concerns:** a compromised hc-api can call any registered op (all are read-only and
argument-free — keep it that way until M2 adds confirmation + locking); DMI serials and Webmin usernames
are now visible to authorised users; legacy Webmin 2.670 lacks post-release upstream fixes.

**Remaining work / decisions:** run CI (push branch); re-run systemd/browser suites on the final commit;
VM validation on your hardware; approve ADR 0001/0003/0005/0008; PAM login; move to Webmin 2.680.
Do not start M2 without approval.
