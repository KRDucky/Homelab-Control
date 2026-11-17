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

// unitAssignments returns every value assigned to a systemd key in a unit
// body, ignoring comments and section headers.
func unitAssignments(body, key string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		if strings.HasPrefix(line, key+"=") {
			out = append(out, strings.TrimPrefix(line, key+"="))
		}
	}
	return out
}

// unitSetting returns systemd's effective value (last assignment wins). It
// fails the test if a key is assigned more than once, so a later conflicting
// assignment (e.g. a second PrivateDevices=no) cannot hide behind a first.
func unitSetting(t *testing.T, body, key string) (string, bool) {
	t.Helper()
	vals := unitAssignments(body, key)
	if len(vals) == 0 {
		return "", false
	}
	if len(vals) > 1 {
		t.Errorf("unit key %s assigned %d times %q; systemd uses the last", key, len(vals), vals)
	}
	return vals[len(vals)-1], true
}

// TestStorageProbeServiceFailClosed is the static service-unit guard: the
// helper must run as a throwaway unprivileged user with no capabilities and no
// device access (fail closed) until the device-open mechanism is authorised.
// Each critical key is asserted as a singleton with an exact value.
func TestStorageProbeServiceFailClosed(t *testing.T) {
	svc := readUnit(t, "homelab-control-storage-probe@.service")

	for key, want := range map[string]string{
		"DynamicUser":             "yes",
		"PrivateDevices":          "yes",
		"CapabilityBoundingSet":   "",
		"AmbientCapabilities":     "",
		"NoNewPrivileges":         "yes",
		"StandardInput":           "socket",
		"StandardOutput":          "socket",
		"PrivateNetwork":          "yes",
		"ProtectSystem":           "strict",
		"ProtectKernelModules":    "yes",
		"RestrictAddressFamilies": "AF_UNIX",
		"LockPersonality":         "yes",
		"MemoryDenyWriteExecute":  "yes",
		"MemoryMax":               "64M",
		"TasksMax":                "16",
		"LimitCORE":               "0",
		"TimeoutStartSec":         "30",
		"Type":                    "oneshot",
	} {
		if got, ok := unitSetting(t, svc, key); !ok || got != want {
			t.Errorf("service %s=%q, want %q (present=%v)", key, got, want, ok)
		}
	}
	if !strings.Contains(svc, "SystemCallFilter=~@") || !strings.Contains(svc, "@raw-io") {
		t.Error("service must deny raw-io and other system-call groups")
	}

	for _, key := range []string{
		"DeviceAllow", "User", "WantedBy", "RequiredBy",
		"ExecStartPre", "ExecStartPost", "ExecStop",
	} {
		if unitHasKey(svc, key) {
			t.Errorf("service must not set %s", key)
		}
	}
	for _, sub := range []string{"char-", "block-"} {
		if strings.Contains(svc, sub) {
			t.Errorf("service must not contain device grant %q", sub)
		}
	}
	// No [Install] section header (a comment mentioning it is allowed).
	for _, line := range strings.Split(svc, "\n") {
		if strings.TrimSpace(line) == "[Install]" {
			t.Error("service must not have an [Install] section")
		}
	}
}

func unitHasKey(body, key string) bool {
	return len(unitAssignments(body, key)) > 0
}

func TestStorageProbeSocketAccept(t *testing.T) {
	sock := readUnit(t, "homelab-control-storage-probe.socket")
	if got, ok := unitSetting(t, sock, "Accept"); !ok || got != "yes" {
		t.Errorf("socket Accept=%q (present=%v), want yes", got, ok)
	}
	if got, ok := unitSetting(t, sock, "MaxConnections"); !ok || got != "1" {
		t.Errorf("socket MaxConnections=%q (present=%v), want 1", got, ok)
	}
	if got, ok := unitSetting(t, sock, "ListenStream"); !ok || got != "/run/homelab-control/storage-probe.sock" {
		t.Errorf("socket ListenStream=%q (present=%v)", got, ok)
	}
	if got, ok := unitSetting(t, sock, "SocketMode"); !ok || got != "0600" {
		t.Errorf("socket SocketMode=%q (present=%v), want 0600", got, ok)
	}
	if unitHasKey(sock, "WantedBy") || unitHasKey(sock, "RequiredBy") {
		t.Error("socket must not be enabled at boot")
	}
	for _, line := range strings.Split(sock, "\n") {
		if strings.TrimSpace(line) == "[Install]" {
			t.Error("socket must not have an [Install] section")
		}
	}
}
