#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later

# M2-3 review probes (probe 7): plain systemd behaviour that the enablement
# review depends on. Runs ONLY inside a disposable container created by
# dev/disposable/probe-container.sh (it refuses to run without the
# /etc/hc-disposable marker). Homelab Control is not installed or used here;
# fixtures are hcp-* units. Phase "pre" sets up and enables; the container is
# then restarted (a real boot of a fresh PID 1) and phase "post" observes.
set -uo pipefail
[ -f /etc/hc-disposable ] || { echo "refusing: not a disposable container"; exit 2; }
phase=${1:?pre|post}
E=/etc/systemd/system
U=/usr/lib/systemd/system
say() { printf '\n## %s\n' "$*"; }
links() { find "$E" -name 'hcp-*' -type l -printf '%p -> %l\n' 2>/dev/null | sort; }
unit() { # name wantedby [extra install lines]
  printf '[Unit]\nDescription=probe %s\n[Service]\nExecStart=/bin/sleep infinity\n[Install]\nWantedBy=%s\n%s\n' "$1" "$2" "${3:-}" >"$E/$1"
}
target() { printf '[Unit]\nDescription=probe target %s\n' "$1" >"$E/$1"; }
show() { systemctl show -p FragmentPath -p DropInPaths -p NeedDaemonReload -p UnitFileState "$1" | sed 's/^/  /'; echo "  is-enabled: $(systemctl is-enabled "$1" 2>&1)"; }

if [ "$phase" = pre ]; then
  systemctl --version | head -n1
  for t in hcp-x.target hcp-y.target hcp-z.target hcp-other.target hcp-lazy.target hcp-dep.service; do target "$t"; done
  printf '[Unit]\nDescription=probe dep\n[Service]\nExecStart=/bin/sleep infinity\n' >"$E/hcp-dep.service"
  unit hcp-a.service multi-user.target
  unit hcp-b.service multi-user.target
  printf '[Unit]\nDescription=probe c (vendor)\n[Service]\nExecStart=/bin/sleep infinity\n[Install]\nWantedBy=multi-user.target\n' >"$U/hcp-c.service"
  unit hcp-d.service multi-user.target
  for t in sysinit basic local-fs sockets; do unit "hcp-e-$t.service" "$t.target"; done
  unit hcp-f.service multi-user.target
  unit hcp-g.service hcp-lazy.target
  unit hcp-h.service multi-user.target
  systemctl daemon-reload # setup only

  say "A1 new drop-in on disk, no reload: does systemctl enable honour it, and does show list it?"
  mkdir -p "$E/hcp-a.service.d"
  printf '[Install]\nWantedBy=hcp-x.target\nAlias=hcp-a-alias.service\n' >"$E/hcp-a.service.d/zz.conf"
  show hcp-a.service
  systemctl --no-reload enable hcp-a.service 2>&1 | sed 's/^/  enable: /'
  links | grep hcp-a | sed 's/^/  link: /'

  say "A2 prefix drop-in (hcp-.service.d) loaded by PID 1: does systemctl enable honour it?"
  mkdir -p "$E/hcp-.service.d"
  printf '[Install]\nWantedBy=hcp-y.target\n' >"$E/hcp-.service.d/zz.conf"
  systemctl daemon-reload # setup only: so PID 1 lists it
  show hcp-b.service
  systemctl --no-reload enable hcp-b.service 2>&1 | sed 's/^/  enable: /'
  links | grep hcp-b | sed 's/^/  link: /'
  rm -rf "$E/hcp-.service.d"; systemctl daemon-reload

  say "A3 type-level drop-in (service.d) loaded by PID 1: does systemctl enable honour it?"
  mkdir -p "$E/service.d"
  printf '[Install]\nWantedBy=hcp-z.target\n' >"$E/service.d/hcp-zz.conf"
  systemctl daemon-reload
  systemctl disable hcp-b.service >/dev/null 2>&1; rm -f "$E"/hcp-y.target.wants/hcp-b.service
  show hcp-b.service
  systemctl --no-reload enable hcp-b.service 2>&1 | sed 's/^/  enable: /'
  links | grep hcp-b | sed 's/^/  link: /'
  rm -rf "$E/service.d"; systemctl daemon-reload

  say "A4 new /etc fragment shadowing a loaded /usr/lib one, no reload"
  printf '[Unit]\nDescription=probe c (admin)\n[Service]\nExecStart=/bin/sleep infinity\n[Install]\nWantedBy=hcp-z.target\n' >"$E/hcp-c.service"
  show hcp-c.service
  systemctl --no-reload enable hcp-c.service 2>&1 | sed 's/^/  enable: /'
  links | grep hcp-c | sed 's/^/  link: /'

  say "B disable removes links its [Install] does not describe"
  systemctl --no-reload enable hcp-d.service >/dev/null 2>&1
  mkdir -p "$E/hcp-other.target.wants" "$E/hcp-dep.service.requires" "$E/multi-user.target.wants"
  ln -s "$E/hcp-d.service" "$E/hcp-other.target.wants/hcp-d.service"
  ln -s "$E/hcp-d.service" "$E/hcp-dep.service.requires/hcp-d.service"
  ln -s "$E/hcp-d.service" "$E/hcp-d-manual.service"
  ln -s "$E/hcp-d.service" "$E/multi-user.target.wants/hcp-renamed.service"
  mkdir -p "$E/deep/er"; ln -s "$E/hcp-d.service" "$E/deep/er/hcp-d.service"
  echo "  before:"; links | grep -E 'hcp-d|hcp-renamed' | sed 's/^/    /'
  systemctl --no-reload disable hcp-d.service 2>&1 | sed 's/^/  disable: /'
  echo "  after:"; links | grep -E 'hcp-d|hcp-renamed' | sed 's/^/    /'

  say "C early-boot WantedBy= (default dependencies), enabled for the next boot"
  for t in sysinit basic local-fs sockets; do systemctl --no-reload enable "hcp-e-$t.service" 2>&1 | sed 's/^/  enable: /'; done

  say "D --no-reload semantics: enable (hcp-f wanted by multi-user, hcp-g by a not-loaded target)"
  systemctl --no-reload enable hcp-f.service hcp-g.service 2>&1 | sed 's/^/  enable: /'
  show hcp-f.service
  echo "  multi-user.target Wants contains hcp-f: $(systemctl show -p Wants multi-user.target | grep -c hcp-f)"
  echo "  hcp-lazy.target load state before start: $(systemctl show -p LoadState --value hcp-lazy.target)"
  systemctl start hcp-lazy.target
  echo "  after 'systemctl start hcp-lazy.target' (no reload): hcp-g is $(systemctl is-active hcp-g.service)"
  systemctl start multi-user.target
  echo "  after 'systemctl start multi-user.target' (no reload): hcp-f is $(systemctl is-active hcp-f.service)"
  systemctl daemon-reload # harness only: what any package install would do
  echo "  after daemon-reload: Wants contains hcp-f: $(systemctl show -p Wants multi-user.target | grep -c hcp-f); hcp-f is $(systemctl is-active hcp-f.service)"
  systemctl start multi-user.target
  echo "  after daemon-reload + 'systemctl start multi-user.target': hcp-f is $(systemctl is-active hcp-f.service)"
  systemctl stop hcp-f.service hcp-g.service

  say "D' --no-reload semantics: disable a running, enabled unit (hcp-h)"
  systemctl enable --now hcp-h.service >/dev/null 2>&1
  systemctl --no-reload disable hcp-h.service 2>&1 | sed 's/^/  disable: /'
  show hcp-h.service
  echo "  hcp-h is $(systemctl is-active hcp-h.service); multi-user.target Wants contains hcp-h: $(systemctl show -p Wants multi-user.target | grep -c hcp-h)"
  echo "  links now:"; links | sed 's/^/    /'
  exit 0
fi

say "after restart (fresh PID 1, fresh boot of the container)"
systemctl --version | head -n1
for u in hcp-a hcp-c hcp-f hcp-g hcp-h hcp-e-sysinit hcp-e-basic hcp-e-local-fs hcp-e-sockets; do
  echo "  $u.service: active=$(systemctl is-active $u.service) enabled=$(systemctl is-enabled $u.service 2>&1)"
done
echo "  system state: $(systemctl is-system-running)"
say "ordering cycles and deleted jobs this boot"
journalctl -b --no-pager -o cat | grep -iE 'ordering cycle|to break ordering|deleted to break|Job .* deleted' | sed 's/^/  /' | head -n 40
echo "  (end)"
