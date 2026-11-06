# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
# SPDX-License-Identifier: GPL-3.0-or-later
# Disposable Ubuntu 26.04 host with systemd as PID 1 (M2-3 container evidence).
# udev and update timers are masked so the container cannot act on host devices
# or change its own baseline mid-run (finding U1).
FROM ubuntu:26.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends systemd systemd-sysv dbus openssh-server \
      python3 procps curl ca-certificates iproute2 perl && apt-get clean && rm -rf /var/lib/apt/lists/*
RUN systemctl mask systemd-udevd.service systemd-udevd-kernel.socket systemd-udevd-control.socket \
      systemd-udev-trigger.service getty.target apt-daily.timer apt-daily-upgrade.timer unattended-upgrades.service || true
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
