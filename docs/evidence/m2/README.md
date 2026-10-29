# M2-2 evidence (service start/stop/restart)

**Current E2/E4 closure:** [final combined-main KVM evidence](vm-final-main-d812995e/README.md), owner approved 2026-10-29. Ubuntu, Fedora and Arch pass on `d812995e`. The original evidence below remains unchanged.

Collected 2026-10-26 on product code **`970341e6`** (`v0.1.0-dev.1-41-g970341e6`). The PR #28 head (`987ba011`) and the merge commit (`a3447efa`) differ from it
by four frontend/test files and documentation, and these logs were not regenerated on them. Results are summarised in
[`docs/m2/M2-2-ACCEPTANCE.md`](../../m2/M2-2-ACCEPTANCE.md). Logs were written outside the repository first
and copied here unchanged. They contain no passwords, cookies or plan tokens (the harness greps for them).

| File | What | Where it ran |
|---|---|---|
| `systemd-ubuntu-26.04.log` | Package smoke + M1 checks + M2-2 scenario (`HC_M2_LONG=1`: 121 s token and 5 min step-up expiry) | Disposable container, Ubuntu 26.04.1, systemd 259.9, systemd as PID 1 |
| `systemd-fedora-43.log` | The same, from the RPM | Disposable container, Fedora 43, systemd 258.11 |
| `go-tests-nonroot.txt` | `go test -v ./...` (test names and results) | Local, uid `hcdev` (non-root), Go 1.26.10 |
| `vitest.txt` | Frontend unit tests | Local |
| `e2e-chromium-webkit.log` | Live API checks + browser e2e (`hc_e2e.py`, CSSOM/CSP probe, HCIS matrix, `service-ops-ui.mjs`) | Local, real `hc-api` with labelled fixtures; Chromium 153 and WebKit 26.6 (Firefox does not start in this workspace) |
| `fuzz-write-args.txt`, `fuzz-logs-args.txt` | 60 s fuzzing of the write and logs argument decoders | Local |
| `make-check.log` | `make check` (lint, tests, security gate incl. govulncheck v1.9.0, reuse, SBOM, shellcheck) | Local |

## How the containers were run
The workspace kernel exposes cgroup v1, and systemd ≥ 258 needs cgroup v2. The CI harness
`dev/disposable/systemd-container.sh` therefore could not be used unchanged here. A local equivalent
did the following:
- mounted a cgroup2 hierarchy at `/srv/cg2`;
- started the container with `--privileged --cgroupns=host -v /srv/cg2:/sys/fs/cgroup`;
- copied in the package and `tests/systemd/`;
- ran `tests/systemd/systemd-smoke.sh` with
  `HC_SMOKE_EXTRA_CHECKS="m1-checks.sh m2-service-ops.sh"`.

The images were built from `ubuntu:26.04` and `fedora:43`, with systemd, D-Bus, OpenSSH, Python 3
and procps added. Only disposable `hc-test-*` units, and one disposable protected-name unit
(`homelab-control-hctest.service`), were started, stopped or restarted. Protected units were
planned only, never executed.

## Not covered by the original October 26 logs
- The owner's KVM VM, through the browser (E2): was outstanding and deferred, not waived, on 2026-10-27; completed by the later KVM evidence linked above.
- VM evidence for E4 (`systemd-analyze security`): only the container check is here.
- GitHub CI: overall results and run annotations are recorded in `docs/m2/M2-2-ACCEPTANCE.md`; individual job
  conclusions and logs were not seen, and the 121 s / 5 min expiry checks are not configured in CI.
- Firefox, run locally.
- Debian, openSUSE and Arch under real systemd (Arch is covered by the later KVM evidence).
