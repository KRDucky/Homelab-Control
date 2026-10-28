#!/bin/sh
# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Decide whether the M2-2 scenario harness may `systemctl stop` a unit name.
# Never stop through an alias. Never stop a host-access unit unless this run
# created a disposable stand-in of that exact name (CREATED).
#
# Env:
#   H3_NAME       requested stop name
#   H3_ID         systemd Id
#   H3_NAMES      space-separated Names
#   H3_FRAGMENT   FragmentPath
#   H3_OWNED      space-separated units this test wrote (CHAINS + UNITS + CREATED)
#   H3_CREATED    space-separated stand-ins created by setup()
#   H3_PATHKIND   regular | symlink | missing | other  (of /etc/systemd/system/$H3_NAME)
#
# stdout: ALLOW | REFUSE <reason>
# exit: 0 allow, 1 refuse

name=${H3_NAME:-}
uid=${H3_ID:-}
names=${H3_NAMES:-}
frag=${H3_FRAGMENT:-}
owned=" ${H3_OWNED:-} "
created=" ${H3_CREATED:-} "
kind=${H3_PATHKIND:-missing}

refuse() { echo "cleanup refused to stop ${1}"; exit 1; }

in_list() {
	case "$2" in
	*" $1 "*) return 0 ;;
	*) return 1 ;;
	esac
}

h3_host_deny() {
	u=$1
	case "$u" in
	ssh.service | sshd.service | ssh.socket | sshd.socket) return 0 ;;
	homelab-control.service | homelab-control-broker.service | homelab-control-broker.socket) return 0 ;;
	systemd-*) return 0 ;;
	dbus | dbus.service | dbus.socket | dbus-broker.service | dbus-broker.socket) return 0 ;;
	dbus-*) return 0 ;;
	NetworkManager | NetworkManager.service | NetworkManager.*) return 0 ;;
	getty@*) return 0 ;;
	*) return 1 ;;
	esac
}

[ -n "$name" ] || refuse "(empty name) (no Id)"
[ -n "$uid" ] || refuse "$name (no Id)"

if h3_host_deny "$uid"; then
	if ! in_list "$uid" "$created"; then
		refuse "$name (resolves to $uid)"
	fi
fi

[ "$uid" = "$name" ] || refuse "$name (resolves to $uid)"

for n in $names; do
	if h3_host_deny "$n"; then
		if ! in_list "$n" "$created"; then
			refuse "$name (also named $n)"
		fi
	fi
done

expected="/etc/systemd/system/$name"
[ "$frag" = "$expected" ] || refuse "$name (FragmentPath $frag)"
[ "$kind" = "regular" ] || refuse "$name (not a regular file we wrote)"
in_list "$name" "$owned" || refuse "$name (not created by this test)"

echo ALLOW
exit 0
