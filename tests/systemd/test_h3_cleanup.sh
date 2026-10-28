#!/bin/sh
# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
#
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Nonprivileged regression tests for H3 cleanup (alias must never be stopped).
# Does not call systemctl, does not need root, python3, or a disposable host.

set -eu
here=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
helper="$here/h3_may_stop.sh"
n_pass=0
n_fail=0

run() {
	desc=$1
	want=$2
	shift 2
	got=$(H3_NAME="$1" H3_ID="$2" H3_NAMES="$3" H3_FRAGMENT="$4" H3_OWNED="$5" H3_CREATED="$6" H3_PATHKIND="$7" \
		/bin/sh "$helper" 2>/dev/null || true)
	# exit status is in the ALLOW/REFUSE prefix
	case "$got" in
	ALLOW*) prefix=ALLOW ;;
	cleanup\ refused\ to\ stop*) prefix=REFUSE ;;
	*) prefix=OTHER ;;
	esac
	if [ "$prefix" = "$want" ]; then
		echo "PASS  $desc"
		n_pass=$((n_pass + 1))
	else
		echo "FAIL  $desc (want $want, got $prefix: $got)"
		n_fail=$((n_fail + 1))
	fi
}

owned="hc-test-dummy.service hc-test-fail.service hc-test-slow.service hc-test-base.service hc-test-mid.service homelab-control-hctest.service hc-test-front.service hc-test-chain.service hc-test-needs.service"
owned_standin="$owned ssh.service"

# Fedora H3: alias of real sshd — the bug that stopped sshd.
run "alias of sshd.service is refused" REFUSE \
	hc-test-innocent.service sshd.service "sshd.service hc-test-innocent.service" \
	/usr/lib/systemd/system/sshd.service "$owned" "" symlink

# Ubuntu: alias of ssh.service (socket-activated; still must not stop).
run "alias of ssh.service is refused" REFUSE \
	hc-test-innocent.service ssh.service "ssh.service hc-test-innocent.service" \
	/usr/lib/systemd/system/ssh.service "$owned" "" symlink

# Disposable unit this test wrote.
run "owned hc-test-dummy.service is allowed" ALLOW \
	hc-test-dummy.service hc-test-dummy.service hc-test-dummy.service \
	/etc/systemd/system/hc-test-dummy.service "$owned" "" regular

# Protected-looking name that this test actually wrote.
run "owned homelab-control-hctest.service is allowed" ALLOW \
	homelab-control-hctest.service homelab-control-hctest.service homelab-control-hctest.service \
	/etc/systemd/system/homelab-control-hctest.service "$owned" "" regular

# Real SSH by its own name.
run "real sshd.service is refused" REFUSE \
	sshd.service sshd.service sshd.service \
	/usr/lib/systemd/system/sshd.service "$owned" "" other

run "real ssh.service is refused" REFUSE \
	ssh.service ssh.service ssh.service \
	/usr/lib/systemd/system/ssh.service "$owned" "" other

# Stand-in ssh.service created on images without OpenSSH.
run "CREATED stand-in ssh.service is allowed" ALLOW \
	ssh.service ssh.service ssh.service \
	/etc/systemd/system/ssh.service "$owned_standin" "ssh.service" regular

# Extra Names still pointing at SSH.
run "owned unit also named sshd is refused" REFUSE \
	hc-test-dummy.service hc-test-dummy.service "hc-test-dummy.service sshd.service" \
	/etc/systemd/system/hc-test-dummy.service "$owned" "" regular

run "dbus.service is refused" REFUSE \
	dbus.service dbus.service dbus.service \
	/usr/lib/systemd/system/dbus.service "$owned" "" other

run "systemd-journald.service is refused" REFUSE \
	systemd-journald.service systemd-journald.service systemd-journald.service \
	/usr/lib/systemd/system/systemd-journald.service "$owned" "" other

run "product broker unit is refused" REFUSE \
	homelab-control-broker.service homelab-control-broker.service homelab-control-broker.service \
	/usr/lib/systemd/system/homelab-control-broker.service "$owned" "" other

run "symlink unit file is refused" REFUSE \
	hc-test-dummy.service hc-test-dummy.service hc-test-dummy.service \
	/etc/systemd/system/hc-test-dummy.service "$owned" "" symlink

run "Id mismatch (alias) is refused" REFUSE \
	hc-test-dummy.service hc-test-h3-target.service "hc-test-h3-target.service hc-test-dummy.service" \
	/etc/systemd/system/hc-test-h3-target.service "$owned" "" regular

run "empty Id is refused" REFUSE \
	hc-test-dummy.service "" hc-test-dummy.service \
	/etc/systemd/system/hc-test-dummy.service "$owned" "" regular

echo "test_h3_cleanup: $n_pass passed, $n_fail failed"
[ "$n_fail" -eq 0 ]
