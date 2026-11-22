// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import "testing"

func TestSystemdRunIsScopeOnly(t *testing.T) {
	ok := []string{systemdRun, "--scope", "--collect", "--quiet", "/usr/bin/mergerfs", "-o", "category.create=epmfs,allow_other", "/mnt/disk1:/mnt/disk2", "/mnt/pool"}
	if !argvAllowed(ok) {
		t.Fatal("scope wrapper rejected")
	}
	for _, argv := range [][]string{
		{systemdRun, "--scope", "--collect", "--quiet", "/bin/sh"},
		{systemdRun, "/usr/bin/mergerfs"},
		{"/bin/systemd-run", "--scope", "--collect", "--quiet", "/usr/bin/mergerfs"},
		{"/bin/sh", "-c", "mergerfs"},
	} {
		if argvAllowed(argv) {
			t.Fatalf("accepted %q", argv)
		}
	}
	if !argvAllowed([]string{"/usr/bin/snapraid", "sync"}) {
		t.Fatal("snapraid sync rejected")
	}
}
