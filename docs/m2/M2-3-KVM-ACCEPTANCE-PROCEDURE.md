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
4. **Retaining** the new guests afterwards (shut off, autostart disabled). **Deleting them is not part of this
   procedure**; it needs a separate cleanup approval.

Not covered and not to be touched: the retained Ubuntu, Fedora and Arch acceptance VMs, their disks and
snapshots, `AGENTS.md`, the file-manager worktree, production hosts.

> **Not ready for authorization while blocker B-2 is open** (`OWNER-DECISIONS.md` P-8): on netplan-based
> Ubuntu hosts the current build refuses every enable and disable (measured on the GitHub runner VM, CI run
> 41977140563), so Phase A on the Ubuntu guests would fail by design. The artifact fields below stay unfilled
> until the product revision that resolves B-2 has passed CI; the most recent CI artifacts (run 41977140563,
> head `b1f4c45a`, merge ref `d8f9fce25991f2c25ad3bb55d9e59b1917751ee1` with a tree identical to the head's) are
> recorded in `docs/evidence/m2/m2-3/review-d9946a4a/ci/build-artifact-41977140563.sha256` for provenance only.

## Inputs (final validated revision)
- **PR #36 head:** `{{HEAD}}`. **Product commit:** `d9946a4a8e1953eee81ab3f7989d3ce8d151f077` (later commits
  change only tests, harness or documentation; see `docs/m2/M2-3-ACCEPTANCE.md`, "Revisions").
- **CI run:** `{{RUN}}` on that head, `{{JOBS}}`. GitHub builds `pull_request` runs from the merge of the head
  into `main` (merge ref `{{MERGE}}`), which is what the package versions name.
- **Packages:** `gh run download {{RUN}} -n build`, then `sha256sum -c SHA256SUMS` (binaries) and check:

  | File | SHA-256 |
  |---|---|
  | `{{DEB}}` | `{{DEBSHA}}` |
  | `{{RPM}}` | `{{RPMSHA}}` |

  Recorded in `docs/evidence/m2/m2-3/review-d9946a4a/ci/build-artifact-{{RUN}}.sha256`. Do not build packages
  locally for acceptance. If the artifact has expired, re-run CI for the same head and record the new run and
  hashes (they will differ: builds are not bit-reproducible across runs).
- **Tests:** `tests/systemd/` and `compat/webmin/` from the PR head.
- **Images:** official cloud images, verified against their published checksums; record names and SHA-256.

## Guest setup (each guest)
1. 2 vCPU, 4 GiB RAM, a fresh overlay disk on the cloud image, cloud-init with an SSH key only.
2. Record `systemctl --version`, `uname -r`, `cat /etc/os-release`. Fedora: `getenforce` → `Enforcing`,
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

**Browser (manual checklist).** The `tests/e2e/m2_3_browser.py` harness drives a Docker container and cannot run
against a VM as is. Through a workstation-loopback SSH tunnel (`ssh -L 127.0.0.1:9180:127.0.0.1:9180 guest`),
with a fresh browser profile in Chromium and Firefox, perform its steps B1–B18 with an independent `systemctl`
observation over SSH for each, and record them in `E2-BROWSER-RECORD.md` as for M2-2.

**Phase A checks afterwards.**
1. `find /etc/systemd/system -type l -printf '%p -> %l\n' | sort | diff /root/links-before.txt -` → empty.
2. SSH `is-enabled` as at baseline; `MainPID` unchanged within each boot (record the values).
3. E4: `systemd-analyze security homelab-control.service homelab-control-broker.service` (last lines); compare
   with API 1.6 / broker 1.5; `systemctl show -p CapabilityBoundingSet homelab-control-broker.service` →
   `cap_setgid cap_setuid`.
4. `hlctl audit verify --file /var/log/homelab-control-broker/operations.jsonl` → exit 0.
5. **Fedora SELinux (normal acceptance):** `getenforce` → `Enforcing`; `ausearch -m AVC,USER_AVC -ts boot` →
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
   (tested in containers: `docs/evidence/m2/m2-3/review-d9946a4a/phase-b/`). The log prints `wrapper window starts/ends`;
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
