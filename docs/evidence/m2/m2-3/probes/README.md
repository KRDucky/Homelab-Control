# M2-3 systemd behaviour probes (2026-11-04)

Run by Claude Code (owner-authorized M2-3 isolated testing) in throwaway Docker containers built from
`Dockerfile.ubuntu` (ubuntu:26.04, systemd 259.9) and `Dockerfile.fedora` (fedora:44, systemd 259.10).
Containers ran with `--privileged --cgroupns=private` and **no** bind mount of the host cgroup tree,
udev masked, own network namespace; they were removed afterwards. Not VM evidence.

`probe1.sh` → `probe1.out`; `probe2.sh` → `probe2.out` (re-run in the same containers after probe 1);
`probe3.sh` → `probe3.out` (fresh containers). `SECRET=hunter2` in `probe1.sh` is a labelled test
fixture in a 0600 drop-in, used to show that an unprivileged `systemctl cat` does not print it
(`probe1.out` contains no occurrence). Findings are summarised in `docs/m2/M2-3-DESIGN-REVIEW.md` §1.

## Probe 7: PR #36 review (2026-11-04)
Scripts: `dev/disposable/probes/m2-3-review-probe.sh` and `m2-3-review-probe-loaded.sh`, run with
`dev/disposable/probe-container.sh <image> <script>` (same isolation; the container is restarted once
between phase `pre` and `post`, a fresh PID 1 boot). Homelab Control is not installed; fixtures are `hcp-*`.
Outputs: `probe7-review-{ubuntu-2604,ubuntu-2204,fedora-44}.out` (systemd 259.9, 249.11, 259.10) and
`probe7b-loaded-{ubuntu-2604,ubuntu-2204}.out`. Findings: `docs/m2/M2-3-DESIGN-REVIEW.md` §1.1 and §7.
