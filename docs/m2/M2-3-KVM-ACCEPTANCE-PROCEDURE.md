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
   disposable `hc-test-*` units, broker restarts, two guest reboots, the browser checklist), including the
   distribution-native test dependencies of "Guest setup" step 4 from the default repositories and one
   retained external disk-only snapshot `pre-m2-3-phase-a` per new guest (step 6).
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
- **Product commit:** `c0f620dec2f9623350a03f9bf51505433071d1e2` (see `docs/m2/M2-3-ACCEPTANCE.md`, "Revisions").
- **PR #36 head validated by CI:** `40801659e44a77749a6a172a8f6564ae1c733419`. Between the product commit and that
  head only documentation, evidence and tests changed
  (`git diff --stat c0f620de 40801659 -- backend frontend schemas packaging cmd` is empty). Commits after
  `40801659` change only documentation (this provenance) — verify with
  `git diff --stat 40801659 HEAD -- backend frontend schemas packaging cmd tests dev` (must be empty) before use.
- **CI run:** `42026882569` on that head, attempt 1: **21 of 21 jobs succeeded**, including `systemd-ubuntu` on the
  stock runner VM with netplan's runtime entries (M2-3 140/0, M2-2 86/0, smoke 70/0) and `systemd-ubuntu-2604`
  (M2-3 141/0, M2-2 86/0, smoke 70/0). GitHub builds `pull_request` runs from the merge of the head into `main`:
  merge ref `71fb621cf7d2a0169fad3e2198851a5b5e0c9009` (parents `81ee254f6fd1fb22531116a390903afd26b3918f`, `main`
  on GitHub, and the head; tree `3eaa5278fd2163322c21aa5e6c4c0166f25b4b45`, **identical to the head's tree**). The
  package versions name the merge ref (`g71fb621c`), not the head: same tree, so the same product code.
- **Packages:** `gh run download 42026882569 -n build`, then `sha256sum -c SHA256SUMS` (binaries) and check:

  | File | SHA-256 |
  |---|---|
  | `homelab-control_0.1.0+dev.1+100+g71fb621c_amd64.deb` | `9d313feb03fcd84095f8d5ca72e81b59308fca3d75dc1fc192429bec05de92e9` |
  | `homelab-control-0.1.0-0.m1.v0_1_0_dev_1_100_g71fb621c.x86_64.rpm` | `21ec32f71d624563b5351fcfee1b6bb69caf5c08f6bf6b9cd6dfe26ac8e0d5d4` |

  Recorded in `docs/evidence/m2/m2-3/review-c0f620de/ci/build-artifact-42026882569.sha256` (with the binaries'
  hashes) and `ci/provenance.txt`. Superseded: run `42008315859` (product `eb2fc2d4`,
  `review-eb2fc2d4/ci/`) — do not use its packages. Do not build packages locally for acceptance. If the
  artifact has expired, re-run CI for the same head and record the new run and hashes. A new `pull_request`
  run builds a **new merge commit**; the package version (`git describe`: commit count and `g<merge sha>`) and
  `SOURCE_DATE_EPOCH` (the merge commit's time, `Makefile`) come from it, so file names and hashes change even
  though the product tree is the same. This is not a contradiction of the reproducibility gate
  (`release-dry-run` builds the same commit twice and compares): reproducibility holds for identical inputs,
  and the merge commit is an input.
- **Tests:** `tests/systemd/` and `compat/webmin/` from the PR head.
- **Images:** official cloud images, verified against their published checksums; record names and SHA-256.

## Guest setup (each guest)
**Names used below.** On the workstation: `GUEST` = the guest's libvirt domain name, `GIP` = its address on the
isolated NAT network (`virsh domifaddr "$GUEST"`), `GUSER` = the account cloud-init provisions (`ubuntu` on
Ubuntu cloud images, `fedora` on Fedora Cloud; verify, do not assume). Remote commands are quoted as one
argument: `ssh "$GUSER@$GIP" 'sudo …'`. Every step marked **(root)** runs in one guest root shell entered with
`ssh -t "$GUSER@$GIP" sudo -i` (cloud-init grants `GUSER` passwordless sudo; record `id` and `sudo -l`), so that
redirects into `/root` and `${PIPESTATUS[0]}` refer to the root process.

1. **Images (workstation).** Download the cloud image and the distribution's signed checksum list; verify the
   signature first, then the checksum: Ubuntu `SHA256SUMS` with `SHA256SUMS.gpg` (`gpg --verify`, key fingerprint
   recorded and compared with the Ubuntu cloud image signing key published by Canonical); Fedora the `CHECKSUM` file
   (`gpgv --keyring <fedora.gpg> *-CHECKSUM`, key from `https://fedoraproject.org/fedora.gpg`, fingerprint recorded).
   Record image name, SHA-256 and the verification output. An unverifiable signature stops the procedure.
2. **Guest definition (workstation).** 2 vCPU, 4 GiB RAM, a **new** overlay disk on the verified image, cloud-init
   with an SSH key only; an isolated libvirt NAT network; **no host directory shares** (no virtiofs/9p, no
   `<filesystem>` element); no NAT port forward to the guest. Record and check before any root scenario:
   `virsh domuuid "$GUEST"` (new UUID, not one of the retained guests), `virsh domblklist "$GUEST"` (only the new
   overlay; its backing file is the verified image), `virsh domiflist "$GUEST"` and `virsh net-dumpxml <net>`
   (isolated NAT, no `<forward>` port mapping), `virsh dumpxml "$GUEST" | grep -c '<filesystem'` → `0`.
3. **Guest identity and readiness (root).** `systemctl --version`, `uname -r`, `cat /etc/os-release`,
   `cat /etc/machine-id`, the manager's unit search path `systemctl show -p UnitPath --value` (both actions are
   refused unless it is exactly the documented path, `M2-3-DESIGN-REVIEW.md` §12; a different path is a
   **finding** to report, not a reason to change the check). Fedora: `getenforce` → `Enforcing`, `sestatus`
   (policy `targeted`); do **not** change SELinux mode or policy. Ubuntu: `aa-status --enabled; echo $?`.
   Wait until the guest is idle: `cloud-init status --wait`; `timedatectl` shows `System clock synchronized:
   yes`; Ubuntu: no `apt`/`dpkg`/`unattended-upgrade` process (`pgrep -a 'apt|dpkg|unattended'` empty) and
   `systemctl is-active unattended-upgrades.service apt-daily.service apt-daily-upgrade.service` shows no
   `activating` (U1 history: an overdue unattended upgrade ran into the M2-2 scenario); Fedora: no running
   `dnf` (`pgrep -a dnf` empty). Do not disable automatic updates; wait instead, and record what was waited for.
4. **Test dependencies (root),** distribution-native packages only, from the distribution's default
   repositories (no third-party repository): Ubuntu `apt-get install -y python3 curl perl procps iproute2`;
   Fedora `dnf -y install python3 curl perl procps-ng iproute audit policycoreutils attr` (`ausearch`,
   `restorecon`, `getfattr`). Record `dpkg -l`/`rpm -q` of these. These are the tools the disposable images
   install (`dev/disposable/images/*.Dockerfile`) plus the SELinux/audit tools used below.
5. **Disposable marker (root).** `touch /etc/hc-disposable`; record `cat /etc/machine-id` next to the domain UUID
   in the evidence README.
6. **Recovery snapshot (workstation, needs the owner's authorization like every guest operation).** Shut the
   guest down, take an external disk-only snapshot `pre-m2-3-phase-a` of the new overlay (`virsh snapshot-create-as
   "$GUEST" pre-m2-3-phase-a --disk-only --atomic`), record `virsh snapshot-list "$GUEST"`, start it again and
   repeat the idle gate of step 3. The snapshot is retained; it is not reverted to and not deleted by this procedure.
7. **Baseline (root).** SSH unit (`ssh.service` on Ubuntu, `sshd.service` on Fedora) `is-enabled` and
   `MainPID`; `find /etc/systemd/system -type l -printf '%p -> %l\n' | sort > /root/links-before.txt`;
   `journalctl --list-boots > /root/boots-before.txt`. Fedora: `ausearch -m AVC,USER_AVC -ts boot >
   /root/avc-before.txt 2>&1 || true`.
8. **Inputs (workstation, then root).** Lay them out as `docker-systemd.sh` does:
   `ssh "$GUSER@$GIP" 'sudo install -d -o "$(id -un)" -g "$(id -gn)" /opt/hc'` (`$(id -un)` is evaluated in the
   guest, i.e. `GUSER`); `scp <pkg> "$GUSER@$GIP:/opt/hc/pkg.deb"` (or `pkg.rpm`); `scp -r tests/systemd
   "$GUSER@$GIP:/opt/hc/tests"`; `scp -r compat/webmin "$GUSER@$GIP:/opt/hc/webmin"`; `scp
   dev/disposable/probes/m2-3-probe9-fresh-install.sh "$GUSER@$GIP:/opt/hc/"`. Then (root) `sha256sum
   /opt/hc/pkg.*` → must equal the hash under "Inputs"; `chown -R root:root /opt/hc`.
9. **First-boot gate inputs (root),** before any `daemon-reload` of your own: `bash
   /opt/hc/m2-3-probe9-fresh-install.sh pre | tee /root/probe9.txt`. It lists what the broker's unloaded-change
   gate would count right after the cloud-image boot (cloud-init, netplan), after installing `/opt/hc/pkg.*` and
   after `systemctl enable --now` of the broker socket. Expected (containers): nothing listed. Anything listed is
   recorded as a P-6 finding (the gate would refuse enable until a reload or reboot), not silently cleared.
   Record whether the probe left the package installed (`dpkg -s homelab-control` / `rpm -q homelab-control`);
   if it did, remove and purge it before A1 (`apt-get purge -y homelab-control` / `dnf -y remove
   homelab-control`) so that A1 starts from a clean host.

## Phase A — normal acceptance (no test wrapper)
**Order matters:** `systemd-smoke.sh` installs the package, runs the scenario and then **removes and purges** it
(and checks that binaries and units are gone). A1 and A2 are therefore separate smoke invocations, each starting
from a host without the package; the reboots and the browser (A3–A5) need an explicit reinstall of the same
package. All steps **(root)**, in the root shell of "Guest setup".

**A1 — scenario (smoke + M1 + M2-2 + M2-3, long waits).**
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
`docs/evidence/m2/m2-3/review-c0f620de/containers/`). The log must contain the passing `binaries removed` and `unit removed`
passing.

**A2 — disable survey (plan only, measurement).** Same environment as A1 without `HC_M2_LONG`, with
`HC_SMOKE_EXTRA_CHECKS=/opt/hc/tests/m2-3-disable-survey.sh`, `tee /root/m2-3-disable-survey.log`, and the same
`PIPESTATUS` line. It **plans** a disable of every enabled service of the cloud image (never applies one) and
records allowed / refused with the reason and the plan duration, plus a fixture with
`Wants=network-online.target` (enabled and disabled again by the harness). Record the tally as P-7
over-refusal data; a plan duration near 15 s is a finding. It also purges the package at the end.

**A3 — reinstall for reboots and browser (exact package).**
1. `sha256sum /opt/hc/pkg.$HC_PKG_KIND` → the hash under "Inputs". Install: Ubuntu `apt-get install -y
   /opt/hc/pkg.deb`; Fedora `dnf -y install /opt/hc/pkg.rpm` (local file; no repository is added). Record
   `dpkg -s homelab-control` / `rpm -qi homelab-control` and `sha256sum /usr/bin/hc-api /usr/bin/hc-broker
   /usr/bin/hlctl` → must equal the binaries' hashes in `build-artifact-42026882569.sha256`.
2. Loopback only: `sed -i 's|^HC_API_ARGS=.*|HC_API_ARGS=-listen 127.0.0.1:9180 -dev-insecure-http|'
   /etc/homelab-control/hc-api.env`; `grep HC_API_ARGS /etc/homelab-control/hc-api.env`.
3. Accounts (passwords typed at the prompt, never on the command line or in logs): `hlctl user add --file
   /etc/homelab-control/users.json --username e2-operator --scopes
   system.read,services.read,services.logs,services.files,services.operate,services.configure,audit.read`;
   `hlctl user add --file /etc/homelab-control/users.json --username nofiles --scopes
   system.read,services.read,services.logs,services.operate,services.configure,audit.read`; `chown
   homelab-control:homelab-control /etc/homelab-control/users.json`.
4. Browser fixtures, exactly the `FIXTURES` of `tests/e2e/m2_3_browser.py` (`hc-test-en-ui.service`,
   `hc-test-en-ui-timer.service`, `hc-test-en-ui.timer`, labelled examples) written to `/etc/systemd/system/`,
   plus `hc-test-boot.service` (`[Unit] Description=M2-3 reboot fixture (example)`, `[Service]
   ExecStart=/bin/sleep infinity`, `[Install] WantedBy=multi-user.target`). Then `sleep 1; systemctl
   daemon-reload; sleep 1` (harness step, so that the review sees no unloaded change) and `systemctl enable
   --now homelab-control-broker.socket homelab-control.service`; `systemctl start hc-test-en-ui.timer`.
5. Checks: `ss -ltnp | grep 9180` shows `127.0.0.1:9180` only; `curl -fsS http://127.0.0.1:9180/api/v1/health`;
   `journalctl -u homelab-control.service -u homelab-control-broker.service -b --no-pager > /root/a3-journal.txt`.
6. Workstation: `ssh -N -L 127.0.0.1:9180:127.0.0.1:9180 "$GUSER@$GIP"` (bound to the workstation's loopback;
   never a NAT port forward or a non-loopback address); from another host on the NAT network the guest's port
   9180 must not answer (record the attempt from the workstation: `curl -m 3 "http://$GIP:9180/"` fails).

**A4 — real reboots.** Enable `hc-test-boot.service` **through the UI or API** as `e2-operator` (plan →
password → apply), `systemctl reboot`; after the boot `systemctl is-active hc-test-boot.service` → `active`
and SSH reachable. Disable it through the UI/API, reboot, check `inactive`. Record both boots
(`journalctl --list-boots`), SSH `MainPID` per boot and the `/run/systemd/system` listing after each boot.
**Unloaded-change refusal on a real boot (recommended):** after the first reboot write
`/etc/systemd/system/hc-test-boot.service.d/zz.conf` (`[Unit]` with `Description=x`) without a reload and
confirm that planning enable is refused ("changed on disk after systemd last loaded its configuration"), then
remove it and reload.

**A5 — browser (manual checklist), workstation-loopback only.** The `tests/e2e/m2_3_browser.py` harness drives a
Docker container and cannot run against a VM as is; A3 reproduces its `setup()` (accounts, fixtures,
loopback listener) without its container relay. With a fresh browser profile in Chromium (and Firefox if
available) at `http://127.0.0.1:9180/`, signed in as `e2-operator`, perform the steps B1–B18 as the harness
defines them (each `record("B…", title, condition)` call names the step and the condition that must hold;
the preceding browser actions are the steps to perform), each with
an independent `systemctl` observation over SSH; B19–B20 as `nofiles`: no unit-file button, the reason names
`services.files`, and `GET /api/v1/services/units/hc-test-en-ui.service/contents` answers 403 while
`GET /api/v1/services/units/homelab-control.service/logs` answers 200. B17 writes `/etc/homelab-control/modules.json` (`{"version":1,"disabled":["services"]}`, root-owned, 0644),
stops the broker and restarts `homelab-control.service`, as the harness does; afterwards remove the file (a
missing file enables every module) and restart both again. Record them in `E2-BROWSER-RECORD.md` as for
M2-2 (`docs/evidence/m2/vm-final-main-d812995e/*/browser-e2/`). Afterwards remove the fixtures (`rm` +
`systemctl daemon-reload`) and record `systemctl is-enabled` of SSH and of the fixtures.

**Phase A checks afterwards.**
1. `find /etc/systemd/system -type l -printf '%p -> %l\n' | sort | diff /root/links-before.txt -` → only the
   links A3 created for `homelab-control.service` and `homelab-control-broker.socket` (list them); any other
   difference is a finding.
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
Only after Phase A has been recorded. Its smoke invocation installs and purges the package itself: first
remove and purge the A3 installation (`apt-get purge -y homelab-control` / `dnf -y remove homelab-control`) and
confirm `/usr/bin/hc-api` is gone. It changes a system binary temporarily; its denials are classified
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
   (tested in containers: standalone `docs/evidence/m2/m2-3/review-00fb7f2b/phase-b/`; the same interruption checks 20/0 inside the full runs on `eb2fc2d4` and `c0f620de`, `review-eb2fc2d4/containers/`, `review-c0f620de/containers/`). The log prints `wrapper window starts/ends`;
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
| SELinux Enforcing, normal operation (Fedora) | Phase A check 6 |
| Long token/step-up expiry on real systemd | Phase A `HC_M2_LONG=1` lines |
| Real broker kill during a boot-time change, binary identity restored | Phase B |
| Real-backend browser workflow in a VM | Browser record |
| E4 in a VM | Phase A check 3 |

Not closed by it: owner approval of P-1…P-6, D-10, merge, production activation of `services.configure`.
