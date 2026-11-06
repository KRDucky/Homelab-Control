# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
# SPDX-License-Identifier: GPL-3.0-or-later
# Disposable Ubuntu 24.04 host like the GitHub ubuntu-24.04 runner image: netplan with the
# networkd renderer and a cloud-init style configuration, so netplan's systemd generator
# writes unit configuration into /run/systemd/system at boot and at every reload (B-2).
# Runs in its own network namespace; the workstation's networking is not touched.
FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends systemd systemd-sysv dbus openssh-server \
      python3 procps curl ca-certificates iproute2 perl netplan.io && apt-get clean && rm -rf /var/lib/apt/lists/*
RUN systemctl mask systemd-udevd.service systemd-udevd-kernel.socket systemd-udevd-control.socket \
      systemd-udev-trigger.service getty.target apt-daily.timer apt-daily-upgrade.timer unattended-upgrades.service || true
# Like the runner's /etc/netplan/50-cloud-init.yaml (match by name instead of the Hyper-V driver).
RUN printf 'network:\n  version: 2\n  ethernets:\n    eth0:\n      match:\n        name: eth0\n      dhcp4: true\n      dhcp4-overrides:\n        route-metric: 100\n      dhcp6: false\n      set-name: eth0\n' \
      > /etc/netplan/50-cloud-init.yaml && chmod 600 /etc/netplan/50-cloud-init.yaml
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
