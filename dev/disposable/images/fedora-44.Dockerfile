# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
# SPDX-License-Identifier: GPL-3.0-or-later
# Disposable Fedora 44 host with systemd as PID 1 (M2-3 container evidence).
# Fedora 44, not 45: rpm 6.1 in 45 rejects unsigned development RPMs with rpm -Uvh.
FROM fedora:44
RUN dnf -y install systemd openssh-server python3 procps-ng curl iproute perl && dnf clean all
RUN systemctl mask systemd-udevd.service systemd-udevd-kernel.socket systemd-udevd-control.socket \
      systemd-udev-trigger.service getty.target dnf-makecache.timer || true; systemctl enable sshd.service
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
