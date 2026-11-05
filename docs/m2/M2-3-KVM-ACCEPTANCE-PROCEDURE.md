# M2-3 KVM acceptance procedure (prepared for owner authorization; NOT run)

**Status:** procedure only. No KVM guest has been created, started or modified for M2-3. Nothing below may be
done before the owner's explicit authorization (next section). By the standing assignment VM work is Codex's
(`docs/evidence/m2/vm-final-main-d812995e/README.md`); Claude Code does not operate VMs. Container evidence
(`docs/evidence/m2/m2-3/`) does **not** satisfy this procedure: containers share the workstation kernel, have
no real boot and ran with SELinux disabled.

## Authorization required before any guest operation
The owner must authorize, in their own words, after reviewing the safety corrections:
1. **Creating** new disposable KVM guests for M2-3 — Ubuntu 26.04.1 (systemd 259), **Ubuntu 22.04.5 (systemd
   249, required: the only version where the unloaded-change gate relies on the generator directory as reload
   marker)** and Fedora 44 with SELinux **Enforcing** (optionally upstream Arch) — from distribution cloud
   images, on an isolated libvirt NAT network, with no host directory shares.
2. **Installing** the CI-built packages named below and running Phase A (normal acceptance: real systemd,
   disposable `hc-test-*` units, broker restarts, two guest reboots, the browser checklist).
3. Separately, **Phase B** (deliberate test-wrapper run): temporarily replacing `/usr/bin/systemctl` in the
   guest with a root-owned test wrapper and SIGKILLing the broker on purpose. Phase B may be authorized or
   declined independently of Phase A.
4. **Retaining** the new guests afterwards: defined, shut off, autostart disabled (`virsh autostart --disable`;
   record `virsh dominfo` showing `Autostart: disable` and state `shut off`). **Deleting them is not part of this
   procedure**; it needs a separate cleanup approval.

Not covered and not to be touched: the retained Ubuntu, Fedora and Arch acceptance VMs, their disks and
snapshots, `AGENTS.md`, the file-manager worktree, production hosts.

> B-2 is addressed since `00fb7f2b` (P-8 option 3) and the final safety review in `f8d3af2e`/`eb2fc2d4` (P-9:
> enable judged against the next boot; hidden runtime configuration refused); implementations pending owner
> review. Ubuntu cloud images use netplan: record `/run/systemd/system` (`find /run/systemd/system -printf
> '%y %p %l\n'`) and `/run/systemd/system.control` after the first boot and in the Phase A log; the scenario
> prints the former before section 6f.

## Inputs (final validated revision)
- **Product commit:** `eb2fc2d45267fa3cef1d2340d737b27c969b14db` (see `docs/m2/M2-3-ACCEPTANCE.md`, "Revisions").
- **PR #36 head validated by CI:** `660151b03fb119f9d1a2e044b486d7460320aae4`. Between the product commit and that
  head only documentation, evidence and measurement scripts changed
  (`git diff --stat eb2fc2d4 660151b0 -- backend frontend schemas packaging cmd` is empty). Commits after
  `660151b0` change only documentation (this provenance) — verify with
  `git diff --stat 660151b0 HEAD -- backend frontend schemas packaging cmd tests dev` (must be empty) before use.
- **CI run:** `42008315859` on that head, attempt 1: **21 of 21 jobs succeeded**, including `systemd-ubuntu` on the
  stock runner VM with netplan's runtime entries (M2-3 137/0, M2-2 86/0) and `systemd-ubuntu-2604` (M2-3
  138/0). GitHub builds `pull_request` runs from the merge of the head into `main`: merge ref
  `f7c6bfca689636a5dfdd92bc764eeba2c1159d8d` (parents `81ee254f6fd1fb22531116a390903afd26b3918f`, `main` on GitHub,
  and the head; tree `21cafccc5b6c48af151ec7b469290104702aaf3e`, **identical to the head's tree**). The package
  versions name the merge ref (`gf7c6bfca`), not the head: same tree, so the same product code.
- **Packages:** `gh run download 42008315859 -n build`, then `sha256sum -c SHA256SUMS` (binaries) and check:

  | File | SHA-256 |
  |---|---|
  | `homelab-control_0.1.0+dev.1+95+gf7c6bfca_amd64.deb` | `79102d1da8705b45432d607ea3e18dbc395035dbfeb51c711dd1e5be4745bc45` |
  | `homelab-control-0.1.0-0.m1.v0_1_0_dev_1_95_gf7c6bfca.x86_64.rpm` | `40a07dcbad9f83b6d0f79960424d15df98417de6464eb1934a9dc3cf2cf1abe1` |

  Recorded in `docs/evidence/m2/m2-3/review-eb2fc2d4/ci/build-artifact-42008315859.sha256`. Do not build packages
  locally for acceptance. If the artifact has expired, re-run CI for the same head and record the new run and
  hashes (they will differ: builds are not bit-reproducible across runs).
- **Tests:** `tests/systemd/` and `compat/webmin/` from the PR head.
- **Images:** official cloud images, verified against their published checksums; record names and SHA-256.

## Guest setup (each guest)
1. 2 vCPU, 4 GiB RAM, a fresh overlay disk on the cloud image, cloud-init with an SSH key only; an isolated
   libvirt NAT network; **no host directory shares** (no virtiofs/9p, no `<filesystem>` element); no NAT port
   forward to the guest. The workstation reaches the guest only over SSH to the guest's NAT address.
2. Record `systemctl --version`, `uname -r`, `cat /etc/os-release`, and the manager's unit search path
   `systemctl show -p UnitPath --value` (both actions are refused unless it is exactly the documented path,
   `M2-3-DESIGN-REVIEW.md` §12; a different path is a **finding** to report, not a reason to change the check). Fedora: `getenforce` → `Enforcing`,
   `sestatus` (policy `targeted`). Do **not** change SELinux mode or policy. Ubuntu: `aa-status --enabled; echo $?`.
3. `sudo touch /etc/hc-disposable`.
4. Baseline: SSH unit (`ssh.service` on Ubuntu, `sshd.service` on Fedora) `is-enabled` and `MainPID`;
   `find /etc/systemd/system -type l -printf '%p -> %l\n' | sort > /root/links-before.txt`.
5. Copy inputs as `docker-systemd.sh` lays them out: `ssh guest sudo install -d -o "$USER" /opt/hc`;
   `scp <pkg> guest:/opt/hc/pkg.deb` (or `pkg.rpm`); `scp -r tests/systemd guest:/opt/hc/tests`;
   `scp -r compat/webmin guest:/opt/hc/webmin`.
6. Fedora: `ausearch -m AVC,USER_AVC -ts boot > /root/avc-before.txt 2>&1 || true`.
7. **First-boot gate inputs and install** (before any `daemon-reload` of your own): copy
   `dev/disposable/probes/m2-3-probe9-fresh-install.sh` to the guest and run `sudo bash m2-3-probe9-fresh-install.sh
   pre | tee /root/probe9.txt`. It lists what the broker's unloaded-change gate would count right after the
   cloud-image boot (cloud-init, netplan), after installing `/opt/hc/pkg.*` and after `systemctl enable --now`
   of the broker socket. Expected (containers): nothing listed. Anything listed is recorded as a P-6 finding
   (the gate would refuse enable until a reload or reboot), not silently cleared.

## Phase A — normal acceptance (no test wrapper)
```sh
cd /opt/hc
export HC_PKG_KIND=deb   # rpm on Fedora
HC_M2_LONG=1 \
HC_SMOKE_EXTRA_UNITS=homelab-control-broker.socket \
HC_SMOKE_EXTRA_CHECKS="/opt/hc/tests/m1-checks.sh /opt/hc/tests/m2-service-ops.sh /opt/hc/tests/m2-3-checks.sh" \
HC_WEBMIN_SRC=/opt/hc/webmin HC_SMOKE_SCOPES=system.read,services.read,services.logs,hardware.read \
  /opt/hc/tests/systemd-smoke.sh /opt/hc/pkg.$HC_PKG_KIND 2>&1 | tee /root/m2-3-phase-a.log
echo "exit ${PIPESTATUS[0]}" | tee -a /root/m2-3-phase-a.log
```
`HC_M2_INTERRUPT` is **not** set: the broker-interruption scenario prints `SKIP` here and runs only in Phase B.
Expected: exit 0, no `FAIL`, no `SKIP` for the expiry waits; counts recorded (container reference:
`docs/evidence/m2/m2-3/README.md`).

**Real reboots.** Create `/etc/systemd/system/hc-test-boot.service` (`ExecStart=/bin/sleep infinity`,
`[Install] WantedBy=multi-user.target`), run `systemctl daemon-reload` (harness step, so that the review sees no
unloaded change), enable it **through the UI or API** (plan → password → apply), `systemctl reboot`; after the
boot `systemctl is-active hc-test-boot.service` → `active` and SSH reachable. Disable it through the UI/API,
reboot, check `inactive`. Then remove the fixture (`rm` + `systemctl daemon-reload`). Record both boots.

**Unloaded-change refusal on a real boot (recommended).** After the first reboot, write
`/etc/systemd/system/hc-test-boot.service.d/zz.conf` (`[Unit]` with `Description=x`) without a reload and
confirm that planning enable is refused ("changed on disk after systemd last loaded its configuration"), then
remove it and reload.

**Disable survey (plan only, measurement).** After the scenario, run the smoke once more with
`HC_SMOKE_EXTRA_CHECKS=/opt/hc/tests/m2-3-disable-survey.sh` (same environment as above, without `HC_M2_LONG`).
It **plans** a disable of every enabled service of the cloud image (never applies one) and records allowed /
refused with the reason and the plan duration, plus a fixture with `Wants=network-online.target` (enabled and
disabled again by the harness). Record the tally as P-7 over-refusal data; a plan duration near 15 s is a
finding.

**Browser (manual checklist), workstation-loopback only.** `hc-api` in the guest listens on the guest's
loopback only (`HC_API_ARGS=-listen 127.0.0.1:9180 -dev-insecure-http` for this test); the workstation reaches
it only through an SSH local forward bound to the workstation's loopback
(`ssh -N -L 127.0.0.1:9180:127.0.0.1:9180 guest`), never through a NAT port forward or a non-loopback address.
The `tests/e2e/m2_3_browser.py` harness drives a Docker container and cannot run against a VM as is. With a
fresh browser profile in Chromium and Firefox at `http://127.0.0.1:9180/`, perform its steps B1–B20 with an
independent `systemctl` observation over SSH for each, and record them in `E2-BROWSER-RECORD.md` as for M2-2.
The browser account needs `services.files` for B10 (`hlctl user scopes … --scopes …,services.files,…`);
B19–B20 use a second account with every permission **except** `services.files`: no unit-file button, the reason
names `services.files`, and `GET /api/v1/services/units/<unit>/contents` answers 403 while its logs answer 200.

**Phase A checks afterwards.**
1. `find /etc/systemd/system -type l -printf '%p -> %l\n' | sort | diff /root/links-before.txt -` → empty.
2. SSH `is-enabled` as at baseline; `MainPID` unchanged within each boot (record the values).
3. E4: `systemd-analyze security homelab-control.service homelab-control-broker.service` (last lines); compare
   with API 1.6 / broker 1.5; `systemctl show -p CapabilityBoundingSet homelab-control-broker.service` →
   `cap_setgid cap_setuid`.
4. `hlctl audit verify --file /var/log/homelab-control-broker/operations.jsonl` → exit 0.
5. **`services.files`:** the scenario log contains `PASS  contents without services.files (operator): 403` and
   `PASS  contents with every scope but services.files: 403`.
6. **Fedora SELinux (normal acceptance):** `getenforce` → `Enforcing`; `ausearch -m AVC,USER_AVC -ts boot` →
   no denial for `hc-broker`, `hc-api`, `systemctl` or the units, compared with `avc-before.txt`; classify any
   other denial as in the M2-2 closeout. A relevant AVC is a failure. This phase never replaces a system binary,
   so no test-wrapper denial can occur in it.

## Phase B — deliberate broker kill with a test wrapper (separately authorized)
Only after Phase A has been recorded. It changes a system binary temporarily; its denials are classified
separately and count neither for nor against normal SELinux acceptance.
1. **Identity before:** `stat -c '%s %a %u:%g %i' /usr/bin/systemctl`, `sha256sum /usr/bin/systemctl`,
   (Fedora) `ls -Z /usr/bin/systemctl` and `getfattr -n security.selinux /usr/bin/systemctl`, and package
   verification `rpm -V systemd` (Fedora) or `dpkg --verify systemd` (Ubuntu); save to `/root/phase-b-before.txt`.
2. Note the time, then run the interruption scenario as its own smoke invocation (the smoke creates the admin
   account and session; `m2_3_interrupt.py` grants `services.configure` itself when it is missing):
   ```sh
   cd /opt/hc
   HC_M2_INTERRUPT=1 HC_SMOKE_EXTRA_UNITS=homelab-control-broker.socket \
   HC_SMOKE_EXTRA_CHECKS=/opt/hc/tests/m2-3-interrupt-checks.sh \
   HC_WEBMIN_SRC=/opt/hc/webmin HC_SMOKE_SCOPES=system.read,services.read,services.logs,hardware.read \
     /opt/hc/tests/systemd-smoke.sh /opt/hc/pkg.$HC_PKG_KIND 2>&1 | tee /root/m2-3-phase-b.log
   ```
   (tested in containers: standalone `docs/evidence/m2/m2-3/review-00fb7f2b/phase-b/`; the same interruption checks 20/0 inside the full runs on `eb2fc2d4`, `review-eb2fc2d4/containers/`). The log prints `wrapper window starts/ends`;
   the script moves the original aside, installs the wrapper, restores the original with `os.replace` (same
   inode), runs `restorecon` where SELinux is present, and checks size, mode, owner, SHA-256 and label.
3. **Identity after:** repeat step 1 into `/root/phase-b-after.txt`; `diff` must be empty (inode, size, mode,
   owner, SHA-256, label, package verification); `restorecon -n -v /usr/bin/systemctl` must print nothing.
4. **Denials:** `ausearch -m AVC,USER_AVC -ts <start time>`; denials inside the wrapper window are
   **test-wrapper** denials (recorded, not product findings); denials outside it are reviewed as product denials.
5. Expected in the log: per variant one intent and one `interrupted` record, observed state "unchanged" /
   "changed, not verified", nothing retried, hash chain verifies.

## Afterwards (each guest)
Copy the logs (no passwords, cookies, plan tokens or session stores) to
`docs/evidence/m2/m2-3/kvm-<date>/`, then shut the guest down and disable autostart. **Retain the guest**; do
not delete it or its disk until a separate cleanup approval.

## Gates this procedure can close (the owner decides)
| Gate | Closed by |
|---|---|
| M2-3 on real kernel boots (Ubuntu, Fedora; optionally 249 and Arch) | Phase A + reboots |
| SELinux Enforcing, normal operation (Fedora) | Phase A check 5 |
| Long token/step-up expiry on real systemd | Phase A `HC_M2_LONG=1` lines |
| Real broker kill during a boot-time change, binary identity restored | Phase B |
| Real-backend browser workflow in a VM | Browser record |
| E4 in a VM | Phase A check 3 |

Not closed by it: owner approval of P-1…P-6, D-10, merge, production activation of `services.configure`.
