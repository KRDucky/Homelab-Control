# Removing Homelab Control

Homelab Control installs its own binaries (`/usr/bin/hc-api`, `/usr/bin/hlctl`,
`/usr/lib/homelab-control/hc-broker` and the opt-in helpers), config (`/etc/homelab-control/`), logs
(`/var/log/homelab-control/`, `/var/log/homelab-control-broker/`), a system user (`homelab-control`) and
systemd units that are not enabled by default.

```bash
sudo systemctl disable --now homelab-control.service homelab-control-broker.socket homelab-control-broker.service
sudo apt remove homelab-control        # Debian/Ubuntu; keeps /etc/homelab-control/hc-api.env
sudo apt purge homelab-control         # also removes users.json; audit logs are kept
# Fedora/openSUSE: sudo dnf remove homelab-control / sudo zypper rm homelab-control
# Arch:            sudo pacman -Rns homelab-control
sudo rm -rf /var/log/homelab-control /var/log/homelab-control-broker   # optional, after review
```

From a source install: stop the process, then delete the binaries, `/etc/homelab-control` and
`/var/log/homelab-control`.

Changes made through Homelab Control (for example a managed APT settings file, a mirror change or a unit
drop-in) stay on the host after removal; each write's backup location is listed in the operation journal
and in the module's user guide.
