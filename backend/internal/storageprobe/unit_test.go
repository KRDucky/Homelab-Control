// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storageprobe

import (
	"os"
	"strings"
	"testing"
)

// unitDir locates the packaging units relative to this package directory
// (go test runs with the package directory as its working directory).
const unitDir = "../../../packaging/common"

func readUnit(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(unitDir + "/" + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// TestStorageProbeServiceFailClosed is the static service-unit guard: the
// helper must run unprivileged with no capabilities and no device access
// (fail closed) until the device-open mechanism is authorised.
func TestStorageProbeServiceFailClosed(t *testing.T) {
	svc := readUnit(t, "homelab-control-storage-probe@.service")
	for _, want := range []string{
		"User=homelab-control",
		"PrivateDevices=yes",
		"CapabilityBoundingSet=\n",
		"AmbientCapabilities=\n",
		"NoNewPrivileges=yes",
		"StandardInput=socket",
		"StandardOutput=socket",
		"PrivateNetwork=yes",
		"ProtectSystem=strict",
		"RestrictAddressFamilies=AF_UNIX",
		"@raw-io",
	} {
		if !strings.Contains(svc, want) {
			t.Fatalf("service missing %q", want)
		}
	}
	for _, banned := range []string{
		"\nDeviceAllow", // no cgroup device grants at all
		"char-*",
		"block-*",
		"\n[Install]", // no enablement section
		"User=root",
		"CapabilityBoundingSet=CAP",
		"AmbientCapabilities=CAP",
	} {
		if strings.Contains(svc, banned) {
			t.Fatalf("service must not contain %q", banned)
		}
	}
}

func TestStorageProbeSocketAccept(t *testing.T) {
	sock := readUnit(t, "homelab-control-storage-probe.socket")
	if !strings.Contains(sock, "Accept=yes") {
		t.Fatalf("socket missing Accept=yes")
	}
	if !strings.Contains(sock, "MaxConnections=1") {
		t.Fatalf("socket missing MaxConnections=1")
	}
	if !strings.Contains(sock, "ListenStream=/run/homelab-control/storage-probe.sock") {
		t.Fatalf("socket missing ListenStream")
	}
	if strings.Contains(sock, "\n[Install]") || strings.Contains(sock, "WantedBy=") {
		t.Fatalf("socket must not be enabled at boot")
	}
}
