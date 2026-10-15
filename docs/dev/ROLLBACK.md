# Safe rollback to upstream Webmin

Homelab Control (M0/M1) **does not modify** an existing Webmin installation: it installs its own binaries
(`/usr/bin/hc-api`, `/usr/bin/hlctl`, `/usr/lib/homelab-control/hc-broker`), config
(`/etc/homelab-control/`), logs (`/var/log/homelab-control/`, `/var/log/homelab-control-broker/`), a
system user (`homelab-control`) and three systemd units that are not enabled by default. Since M1 the
root broker *reads* Webmin's configuration (allow-listed fields only); nothing writes under `/etc/webmin`.

## Remove Homelab Control, keep upstream Webmin
```bash
sudo systemctl disable --now homelab-control.service homelab-control-broker.socket homelab-control-broker.service
sudo apt remove homelab-control        # Debian/Ubuntu; keeps /etc/homelab-control/hc-api.env
sudo apt purge homelab-control         # also removes users.json; audit logs are kept
# Fedora/openSUSE: sudo dnf remove homelab-control / sudo zypper rm homelab-control
# Arch:            sudo pacman -Rns homelab-control
sudo rm -rf /var/log/homelab-control /var/log/homelab-control-broker   # optional, after review
```
From a source install: stop the process, delete the two binaries, `/etc/homelab-control` and
`/var/log/homelab-control`. Upstream Webmin keeps running on its port with its own accounts.

## Return a host to stock upstream Webmin packages
Because M0 does not ship a modified Webmin, there is nothing to roll back in Webmin itself. If you
installed Webmin from `compat/webmin` via `setup.sh` (development only), reinstall the official package:
1. Back up `/etc/webmin` (`sudo tar czf webmin-etc-backup.tgz /etc/webmin`).
2. Install the upstream package for your distro from webmin.com or your distro; it reuses `/etc/webmin`.
3. Verify login on port 10000, then remove the old `setup.sh` install directory.

## Future milestones
When later milestones change Webmin files (recorded in `compat/PATCHES.md`) or migrate Webmin
configuration, they must ship: a pre-migration backup of `/etc/webmin` and the app DB, a tested
`hlctl migrate rollback`, and VM evidence of rollback. No such migration exists in M0.
