# Disposable libvirt VMs for M1 validation (workstation)

Replaces the earlier Vagrantfile (never run; it used libvirt's default storage pool). Everything here
runs as your normal user against `qemu:///system`; nothing uses `sudo` on the host.

## Safety limits (enforced by `hc-vm.sh`, not just documented)
- VM and volume names must start with `hc-test-`.
- Disks are volumes in the existing **`hc-test`** pool only; no host block devices, no other paths.
  The pool is never created, changed or deleted by the scripts.
- Network: libvirt `default` (NAT) only. Inside the guest, hc-api and Webmin listen on loopback only.
- `destroy` refuses domains without the harness's metadata marker or with any disk outside `hc-test`,
  and needs `--yes`. Existing VMs are never touched.
- Ubuntu cloud images come only from `cloud-images.ubuntu.com`, and are used only after `SHA256SUMS`
  carries a valid signature from the pinned Ubuntu cloud-image key
  (`D2EB 4462 6FDD C30B 513D 5BB7 1A5D 6C4C 7DB8 7C81`) and the image matches its checksum.
- Harness state (images, SSH key, seed ISOs, GPG keyring) lives in `.dev/vm/` (git-ignored).

## 1. One-time host prerequisites (CachyOS / Arch) — you run these
```bash
sudo pacman -S --needed libvirt qemu-base virt-install cloud-image-utils gnupg openssh dpkg go nodejs npm make
sudo usermod -aG libvirt "$USER"          # log out/in afterwards
virsh -c qemu:///system pool-info hc-test  # must exist and be active (create it yourself)
virsh -c qemu:///system net-info default   # must be active
```
### Host firewall
With **ufw** active (CachyOS default in some setups), guests on `default` get no DHCP lease: ufw's input
policy drops their requests on `virbr0` (found on the first workstation run, 2026-10-16). Allow only the
libvirt bridge, and undo with `sudo ufw delete …` using the same arguments:
```bash
sudo ufw allow in on virbr0 to any port 67 proto udp comment 'libvirt DHCP'
sudo ufw allow in on virbr0 to any port 53 comment 'libvirt DNS'
sudo ufw route allow in on virbr0     # only if `ufw status verbose` shows "deny (routed)"
sudo ufw route allow out on virbr0
```
`hc-vm.sh preflight` prints a note when ufw is active.

`cloud-image-utils` provides `cloud-localds`; `xorriso` or `cdrtools` (`mkisofs`) also work.
`dpkg` provides `dpkg-deb` for building the `.deb` on Arch.

## 2. Build the package
```bash
cd ~/Projects/Homelab-Control---Webmin-Fork
make deps && make deb            # needs Go >= 1.24, Node >= 22; output: build/homelab-control_*.deb
```
Or download the `build` artifact of a green CI run with `gh run download <run-id> -n build -D build/`.

## 3. Run the whole M1 validation (one command)
```bash
dev/vm/run-m1-validation.sh hc-test-u2604 26.04 build/homelab-control_*.deb
```
This runs `hc-vm.sh preflight`, creates `hc-test-u2604` if absent (4 GiB RAM, 2 vCPU, 20 GiB qcow2
overlay on the verified cloud image, cloud-init user `hc` with a harness-only SSH key), stages the
package, tests and the pinned `compat/webmin`, and runs `guest-validate.sh` as root in the guest:

| Step | What it verifies |
|---|---|
| 1. systemd smoke + M1 checks | install does not enable anything; hc-api runs as `homelab-control` with an empty effective capability set; `systemd-analyze security` exposure for all units; **broker socket activation** (socket unit active, `/run/homelab-control/broker.sock` mode 660 `root:homelab-control`, broker service active after the first client connection, runs as root with CapEff = CAP_SETUID\|CAP_SETGID only and NoNewPrivs, other local users refused); login, `__Host-` cookie, CSRF, every M1 API route and scope; legacy Webmin detection; graceful degradation when the broker stops; remove + purge |
| 2. rollback / coexistence | `apt remove` stops everything, frees port 9180 and the broker socket but keeps config; reinstall reuses accounts and does not auto-enable; `apt purge` deletes `/etc/homelab-control`; upstream Webmin keeps answering throughout |
| 3. browser e2e + axe | Playwright Chromium against the packaged stack: navigation, Services, Hardware, Legacy, mobile layout, history/deep links, broker-stopped degradation, no CSP violations; axe-core WCAG 2.1 A/AA on each page (serious/critical = fail; axe missing = fail) |
| 4. final removal | package purged; no processes, units or listener left |

Results land in `docs/evidence/m1/vm-26.04/` (`SUMMARY.txt`, per-step logs, screenshots, `axe-report.json`,
`environment.txt`, `host-environment.txt`). The script exits non-zero if any step fails.

## 4. Manual inspection and individual commands
```bash
dev/vm/hc-vm.sh status                     # hc-test-* domains and hc-test volumes
dev/vm/hc-vm.sh ssh hc-test-u2604          # shell in the guest
dev/vm/hc-vm.sh ssh hc-test-u2604 'sudo systemctl status homelab-control-broker.socket'
dev/vm/hc-vm.sh ssh hc-test-u2604 'sudo journalctl -u "homelab-control*" --no-pager | tail -50'
```

## 5. Rollback and cleanup
- **Inside the guest** (what a real host would do): `sudo apt remove homelab-control` keeps
  `/etc/homelab-control`; `sudo apt purge homelab-control` removes it. Neither touches an installed
  upstream Webmin — rollback to plain Webmin is "remove our package"; see `docs/dev/ROLLBACK.md`.
- **The VM itself:** `dev/vm/hc-vm.sh destroy hc-test-u2604 --yes` removes the domain, its overlay and
  seed volumes; the verified base image volume `hc-test-base-ubuntu-26.04.qcow2` is kept for reuse
  (`virsh -c qemu:///system vol-delete --pool hc-test hc-test-base-ubuntu-26.04.qcow2` to drop it).

## Other releases
`hc-vm.sh` also accepts `24.04`. Additional distributions are intentionally not wired in until the
26.04 VM passes the full suite.
