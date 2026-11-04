# M2-3 KVM acceptance procedure (prepared for owner authorization; NOT run)

**Status:** procedure only. No guest has been created, started or modified for M2-3. Running it needs an
explicit owner authorization (below). By the standing assignment, VM work is Codex's
(`docs/evidence/m2/vm-final-main-d812995e/README.md`); Claude Code does not touch the VMs.
Container evidence (`docs/evidence/m2/m2-3/`) does **not** satisfy this procedure: containers share the
workstation kernel, have no real boot, and ran with SELinux disabled.

## Authorization required before any guest operation
The owner must authorize, in their own words:
1. creating **new, disposable** KVM guests for M2-3 (Ubuntu 26.04.1, Fedora 44 with SELinux **Enforcing**,
   and optionally upstream Arch Linux) from distribution cloud images, on an isolated libvirt network;
2. installing the CI-built packages of a named PR #36 revision in them and running the M2-3 scenario
   (it enables/disables disposable `hc-test-*` units, restarts the broker, SIGKILLs it once on purpose,
   and reboots the guest once);
3. deleting those guests afterwards.

The retained Ubuntu, Fedora and Arch acceptance VMs and their snapshots are **not** used, started, reverted
or changed by this procedure. If the owner prefers them, that is a separate authorization.

## Inputs
- **Revision:** the PR #36 head named in the authorization (at the time of writing `<HEAD>`, product commit
  `82c24a74` or later). Record `git rev-parse HEAD`.
- **Packages:** the `build` artifact of the CI run for exactly that head (`gh run download <run-id> -n build`).
  Check `sha256sum -c SHA256SUMS` in the artifact and record the hashes of the `.deb` and `.rpm`. Do not
  build packages locally for acceptance.
- **Tests:** `tests/systemd/` from the same revision (copied to the guest as `/opt/hc/tests`) and
  `compat/webmin` (for the smoke checks).
- **Images:** official cloud images with their published checksums (Ubuntu 26.04.1 server cloud image,
  Fedora 44 Cloud Base qcow2). Record image names and SHA-256.

## Guest setup (each guest)
1. Create the guest with 2 vCPU, 4 GiB RAM, a fresh overlay disk on the cloud image, cloud-init with an SSH
   key only (no password login), on an isolated NAT network, no host directory shares.
2. Fedora: confirm `getenforce` → `Enforcing` and `sestatus` → policy `targeted`; record both. Do **not**
   change SELinux mode or policy. Ubuntu: record `aa-status --enabled; echo $?`.
3. Mark the guest disposable: `sudo touch /etc/hc-disposable`.
4. Record the baseline: `systemctl --version`, `uname -r`, `systemctl is-enabled ssh.service` (Fedora:
   `sshd.service`), `systemctl show -p MainPID sshd.service`/`ssh.service`, and
   `find /etc/systemd/system -type l -printf '%p -> %l\n' | sort > /root/links-before.txt`.
5. Copy packages and tests (as `docker-systemd.sh` lays them out): `ssh guest sudo install -d -o $USER /opt/hc`, then
   `scp <pkg> guest:/opt/hc/pkg.deb` (or `pkg.rpm`), `scp -r tests/systemd guest:/opt/hc/tests`,
   `scp -r compat/webmin guest:/opt/hc/webmin`.
6. Fedora only: `sudo ausearch -m AVC,USER_AVC -ts boot > /root/avc-before.txt 2>&1 || true`.

## Run (each guest, as root, over SSH)
```sh
cd /opt/hc
export HC_PKG_KIND=deb   # rpm on Fedora
HC_M2_LONG=1 HC_M2_INTERRUPT=1 \
HC_SMOKE_EXTRA_UNITS=homelab-control-broker.socket \
HC_SMOKE_EXTRA_CHECKS="/opt/hc/tests/m1-checks.sh /opt/hc/tests/m2-service-ops.sh /opt/hc/tests/m2-3-checks.sh" \
  /opt/hc/tests/systemd-smoke.sh /opt/hc/pkg.$HC_PKG_KIND 2>&1 | tee /root/m2-3-kvm.log
echo "exit ${PIPESTATUS[0]}" | tee -a /root/m2-3-kvm.log
```
Expected: exit 0; no `FAIL` lines; no `SKIP` for the expiry waits or the broker interruption (both
`HC_M2_LONG=1` and `HC_M2_INTERRUPT=1` are set); the M2-2 and M2-3 counts are recorded (container
reference on `82c24a74`: `docs/evidence/m2/m2-3/README.md`).

### Real reboot check (each guest)
After the scenario (which cleans up its own fixtures), check boot behaviour with a real kernel boot:
```sh
cat > /etc/systemd/system/hc-test-boot.service <<'EOF'
[Unit]
Description=M2-3 KVM boot fixture
[Service]
ExecStart=/bin/sleep infinity
[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
```
Enable it **through the UI or API** (plan → step-up → apply), record the outcome, then `systemctl reboot`.
After the boot: `systemctl is-active hc-test-boot.service` → `active`; SSH reachable; then disable it through
the UI/API, reboot again, and check `inactive`. Remove the fixture
(`rm /etc/systemd/system/hc-test-boot.service; systemctl daemon-reload`).

### Browser (Ubuntu and Fedora)
Real backend through a workstation-loopback SSH tunnel (`ssh -L 127.0.0.1:9180:127.0.0.1:9180 guest`), a fresh
browser profile, Chromium and Firefox. Run the B1–B18 checklist of `tests/e2e/m2_3_browser.py` (its harness
drives a Docker container; in a VM the same steps are performed through the tunnel and recorded in an
`E2-BROWSER-RECORD.md` like M2-2's). Include keyboard-only enable, the phone (390 px) and laptop (1366 px)
layouts, axe on each page, and a check that the plan text says the change can take effect before a reboot.

## Afterwards (each guest)
1. `find /etc/systemd/system -type l -printf '%p -> %l\n' | sort | diff /root/links-before.txt -` → empty.
2. SSH: `is-enabled` and `MainPID` as in the baseline (MainPID changes only across the deliberate reboots;
   record both values).
3. E4: `systemd-analyze security homelab-control.service homelab-control-broker.service | tail -n 1` each;
   compare with API 1.6 / broker 1.5 (the M2-2 KVM closeout); any increase is a finding; `systemctl show -p CapabilityBoundingSet homelab-control-broker.service`
   → `cap_setgid cap_setuid`.
4. **Fedora SELinux:** `getenforce` → still `Enforcing`; `ausearch -m AVC,USER_AVC -ts boot` → no new denial
   for `hc-broker`, `hc-api`, `systemctl` or the broker's root templates compared with `avc-before.txt`;
   classify any other denial as in the M2-2 closeout. A relevant AVC is a failure.
5. `hlctl audit verify --file /var/log/homelab-control-broker/operations.jsonl` → exit 0; the journal shows
   the deliberate interruption as one intent plus one `interrupted` record for each variant.
6. Copy logs (no passwords, cookies, tokens or session stores), then shut the guest down and delete it if the
   authorization says so.

## Acceptance gates this procedure can close (owner decides)
| Gate | Closed by |
|---|---|
| M2-3 on a real kernel boot (Ubuntu, Fedora) | Run + reboot check |
| SELinux Enforcing (Fedora) | Step 2 and Afterwards 4 |
| Long token/step-up expiry on real systemd | `HC_M2_LONG=1` lines in the log |
| Real broker kill during a boot-time change | `m2_3_interrupt.py` lines in the log |
| Real-backend browser workflow in a VM | Browser record |
| E4 in a VM | Afterwards 3 |

Not closed by it: owner decisions O-1…O-5, D-10, merge, production activation of `services.configure`.
