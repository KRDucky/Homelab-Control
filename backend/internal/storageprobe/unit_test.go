// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storageprobe

import (
	"homelab-control/backend/internal/storagetools"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
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

// TestStorageProbeServiceFailClosed is the static service-unit guard for the
// O-1 read-only block-device helper (docs/m2/M2-4-O1-DEVICE-POLICY.md): a
// throwaway unprivileged user, no capabilities, a closed device policy that
// grants exactly the reviewed block groups read-only, an opt-in marker so the
// unit is disabled by default, and fail-closed MAC bindings. Each critical key
// is a singleton with an exact value. tests/packaging/storage-probe-policy-check.sh
// is the complete, shell-level version of this guard (it also mutates the units
// to prove each rule rejects violations).
func TestStorageProbeServiceFailClosed(t *testing.T) {
	svc := readUnit(t, "homelab-control-storage-probe@.service")

	for key, want := range map[string]string{
		"ConditionPathExists":     "/etc/homelab-control/storage-probe.enabled",
		"RefuseManualStart":       "yes",
		"PrivateDevices":          "no",
		"DevicePolicy":            "closed",
		"CapabilityBoundingSet":   "CAP_SYS_RAWIO CAP_SYS_ADMIN",
		"AmbientCapabilities":     "CAP_SYS_RAWIO CAP_SYS_ADMIN",
		"NoNewPrivileges":         "yes",
		"StandardInput":           "socket",
		"StandardOutput":          "socket",
		"PrivateNetwork":          "yes",
		"ProtectSystem":           "strict",
		"ProtectKernelModules":    "yes",
		"RestrictAddressFamilies": "AF_UNIX",
		"LockPersonality":         "yes",
		"MemoryDenyWriteExecute":  "yes",
		"SystemCallErrorNumber":   "EPERM",
		"SELinuxContext":          "system_u:system_r:hc_storage_probe_t:s0",
		"AppArmorProfile":         "hc-storage-probe",
		"MemoryMax":               "128M",
		"TasksMax":                "16",
		"LimitCORE":               "0",
		"TimeoutStartSec":         "60",
		"Type":                    "oneshot",
		"ExecStart":               "/usr/lib/homelab-control/hc-storage-probe",
	} {
		if got, ok := unitSetting(t, svc, key); !ok || got != want {
			t.Errorf("service %s=%q, want %q (present=%v)", key, got, want, ok)
		}
	}
	if !strings.Contains(svc, "SystemCallFilter=~@") || !strings.Contains(svc, "@raw-io") {
		t.Error("service must deny raw-io and other system-call groups")
	}

	// DeviceAllow is the only grant of raw block access: exactly the reviewed
	// groups, each read-only, with no duplicates and no empty reset.
	want := []string{"block-sd r", "block-blkext r", "block-virtblk r", "block-md r", "block-device-mapper r", "/dev/zfs rw"}
	got := unitAssignments(svc, "DeviceAllow")
	if len(got) != len(want) {
		t.Errorf("DeviceAllow=%q, want exactly %q", got, want)
	}
	for _, w := range want {
		found := 0
		for _, g := range got {
			if g == w {
				found++
			}
		}
		if found != 1 {
			t.Errorf("DeviceAllow %q appears %d times, want 1", w, found)
		}
	}

	// systemd grants block-loop and block-blkext "rwm" for these keys
	// (systemd.exec(5)), which would make blkext writable.
	for _, key := range []string{
		"User", "Group", "WantedBy", "RequiredBy", "Alias", "Also",
		"ExecStartPost", "ExecStop", "ExecReload",
		"RootImage", "RootDirectory", "RootVolatile", "MountImages", "ExtensionImages",
		"BindPaths", "BindReadOnlyPaths", "TemporaryFileSystem",
	} {
		if unitHasKey(svc, key) {
			t.Errorf("service must not set %s", key)
		}
	}
	// Exec lines run inside the helper's MAC domain, which cannot exec grep or a
	// shell; the /proc/devices self-check belongs to the helper itself.
	for _, key := range []string{"ExecStartPre", "ExecStopPost", "ExecCondition"} {
		if unitHasKey(svc, key) {
			t.Errorf("service must not set %s", key)
		}
	}
	// No char devices, loop/dm/other block classes, wildcards, device paths
	// (except the OpenZFS control node) or write access to a block device.
	for _, sub := range []string{"char-", "block-loop", "block-*", "block-dm", "block-nbd", "block-zram", "/dev", " rw", " rm", " w", " m\n"} {
		for _, line := range unitAssignments(svc, "DeviceAllow") {
			if line == "/dev/zfs rw" {
				continue
			}
			if strings.Contains(line+"\n", sub) {
				t.Errorf("DeviceAllow %q contains forbidden %q", line, sub)
			}
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
	if got, ok := unitSetting(t, sock, "ConditionPathExists"); !ok || got != "/etc/homelab-control/storage-probe.enabled" {
		t.Errorf("socket ConditionPathExists=%q (present=%v), want the opt-in marker", got, ok)
	}
	// Owner 2026-11-25: an administrator may enable the socket like the other
	// helpers; the package itself never enables it (tests/packaging).
	if got, ok := unitSetting(t, sock, "WantedBy"); !ok || got != "sockets.target" {
		t.Errorf("socket WantedBy=%q (present=%v), want sockets.target", got, ok)
	}
	if unitHasKey(sock, "RequiredBy") {
		t.Error("socket must not be required by another unit")
	}
}

// The unit must let the helper run its longest budget and answer: systemd
// kills a connection's service after TimeoutStartSec.
func TestUnitOutlastsHelperBudgets(t *testing.T) {
	v := unitAssignments(readUnit(t, "homelab-control-storage-probe@.service"), "TimeoutStartSec")
	if len(v) != 1 {
		t.Fatalf("TimeoutStartSec=%q, want exactly one", v)
	}
	secs, err := strconv.Atoi(v[0])
	if err != nil {
		t.Fatalf("TimeoutStartSec=%q: %v", v[0], err)
	}
	for _, scope := range []string{"", storagetools.ScopeZFS, storagetools.ScopeBtrfs, storagetools.ScopeMd, storagetools.ScopeLVM, storagetools.ScopeExt4, storagetools.ScopeXFS} {
		if b := storagetools.HelperBudget(scope); time.Duration(secs)*time.Second <= b {
			t.Errorf("TimeoutStartSec=%ds does not exceed the %q budget %v", secs, scope, b)
		}
	}
}
