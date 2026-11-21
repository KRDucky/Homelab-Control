// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsapply

import (
	"os"
	"strings"
	"testing"
)

func TestApplyUnitIsOptIn(t *testing.T) {
	svc, err := os.ReadFile("../../../packaging/common/homelab-control-mergerfs-apply@.service")
	if err != nil {
		t.Fatal(err)
	}
	text := string(svc)
	for _, want := range []string{
		"ExecStart=/usr/lib/homelab-control/hc-mergerfs-apply",
		"ConditionPathExists=/etc/homelab-control/mergerfs-apply.enabled",
		"RefuseManualStart=yes",
		"PrivateDevices=no",
		"DeviceAllow=/dev/fuse rwm",
		"User=root",
		"CapabilityBoundingSet=CAP_SYS_ADMIN CAP_DAC_OVERRIDE CAP_DAC_READ_SEARCH CAP_FOWNER CAP_CHOWN",
		"SystemCallFilter=@system-service @mount",
		"KillMode=process",
		"PrivateMounts=no",
		"PrivateNetwork=no",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("unit missing %s", want)
		}
	}
	for _, banned := range []string{"[Install]", "AppArmorProfile=", "SELinuxContext=", "homelab-control-storage-helper", "~@mount", "ProtectSystem=", "ProtectHome=", "PrivateTmp=", "PrivateNetwork=yes", "ExecStart=/bin/false"} {
		if strings.Contains(text, banned) {
			t.Errorf("unit contains %s", banned)
		}
	}
	sock, err := os.ReadFile("../../../packaging/common/homelab-control-mergerfs-apply.socket")
	if err != nil {
		t.Fatal(err)
	}
	s := string(sock)
	if strings.Contains(s, "[Install]") || !strings.Contains(s, "SocketMode=0600") || !strings.Contains(s, "mergerfs-apply.enabled") {
		t.Fatalf("socket:\n%s", s)
	}
}
