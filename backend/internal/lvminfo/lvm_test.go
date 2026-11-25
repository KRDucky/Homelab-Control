// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package lvminfo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseKernelDM(t *testing.T) {
	if !parseKernelDM("  1 mem\n253 device-mapper\n") {
		t.Fatal("last field device-mapper must count")
	}
	if parseKernelDM("253 device-mapper-extra\n254 device-mapper-foo\n") {
		t.Fatal("a longer name must not count")
	}
	if parseKernelDM("device-mapper 253\n") || parseKernelDM("") || parseKernelDM("  1 mem\n") {
		t.Fatal("device-mapper must be the last field")
	}
}

func TestProbeKernelDMFixture(t *testing.T) {
	dir := t.TempDir()
	if got := ProbeKernelDM(dir); got.Present || got.Reason != reasonDMUnread {
		t.Fatalf("missing proc: %+v", got)
	}
	if err := os.MkdirAll(filepath.Join(dir, "proc"), 0o755); err != nil {
		t.Fatal(err)
	}
	proc := filepath.Join(dir, "proc", "devices")
	if err := os.WriteFile(proc, []byte("253 device-mapper\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ProbeKernelDM(dir); !got.Present || got.Reason != "" {
		t.Fatalf("listed: %+v", got)
	}
	if err := os.WriteFile(proc, []byte("253 device-mapper-extra\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ProbeKernelDM(dir); got.Present || got.Reason != reasonDMMissing {
		t.Fatalf("not listed: %+v", got)
	}
	big := strings.Repeat("x", kernelDMMaxBytes+1)
	if err := os.WriteFile(proc, []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ProbeKernelDM(dir); got.Present || got.Reason != reasonDMUnread {
		t.Fatalf("oversized: %+v", got)
	}
}

func TestProbeToolingPackages(t *testing.T) {
	stat := func(string) bool { return false }
	debian := probeTooling("debian", stat)
	if debian.Family != "debian" || len(debian.Required) != 1 || len(debian.Optional) != 1 {
		t.Fatalf("%+v", debian)
	}
	if debian.Required[0].ID != "lvm" || debian.Required[0].Package != "lvm2" || debian.Required[0].Installed {
		t.Fatalf("debian required: %+v", debian.Required[0])
	}
	if debian.Optional[0].ID != "thin_check" || debian.Optional[0].Package != "thin-provisioning-tools" {
		t.Fatalf("debian optional: %+v", debian.Optional[0])
	}
	fedora := probeTooling("fedora", stat)
	if fedora.Required[0].Package != "lvm2" || fedora.Optional[0].Package != "device-mapper-persistent-data" {
		t.Fatalf("fedora: %+v %+v", fedora.Required[0], fedora.Optional[0])
	}
	suse := probeTooling("suse", stat)
	if suse.Required[0].Package != "lvm2" || suse.Optional[0].Package != "" || suse.Optional[0].ID != "thin_check" {
		t.Fatalf("suse must not invent a thin package: %+v", suse.Optional[0])
	}
	arch := probeTooling("arch", stat)
	if arch.Required[0].Package != "lvm2" || arch.Optional[0].Package != "thin-provisioning-tools" {
		t.Fatalf("arch: %+v %+v", arch.Required[0], arch.Optional[0])
	}
	unknown := probeTooling("unknown", stat)
	if unknown.Required[0].Package != "" || unknown.Optional[0].Package != "" || unknown.Optional == nil {
		t.Fatalf("unknown: %+v", unknown)
	}
}

func TestProbeToolingPrefersSbin(t *testing.T) {
	stat := func(path string) bool {
		return path == "/usr/sbin/lvm" || path == "/usr/bin/thin_check"
	}
	tool := probeTooling("debian", stat)
	if !tool.Required[0].Installed || tool.Required[0].Binary != "/usr/sbin/lvm" {
		t.Fatalf("lvm: %+v", tool.Required[0])
	}
	if !tool.Optional[0].Installed || tool.Optional[0].Binary != "/usr/bin/thin_check" {
		t.Fatalf("thin_check: %+v", tool.Optional[0])
	}
}

func TestProbeCapability(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "proc"), 0o755); err != nil {
		t.Fatal(err)
	}
	proc := filepath.Join(dir, "proc", "devices")
	if err := os.WriteFile(proc, []byte("253 device-mapper\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	none := func(string) bool { return false }
	ok, reason := probeCapability("debian", none, dir)
	if ok || reason != "Required tooling is not installed: lvm." {
		t.Fatalf("missing binary, kernel listed: ok=%v reason=%q", ok, reason)
	}
	ok, reason = probeCapability("debian", none, t.TempDir())
	if ok || !strings.Contains(reason, "Required tooling is not installed: lvm.") || !strings.Contains(reason, reasonDMUnread) {
		t.Fatalf("missing both: ok=%v reason=%q", ok, reason)
	}
	lvmOnly := func(path string) bool { return filepath.Base(path) == "lvm" }
	ok, reason = probeCapability("debian", lvmOnly, t.TempDir())
	if ok || reason != reasonDMUnread {
		t.Fatalf("kernel unread: ok=%v reason=%q", ok, reason)
	}
	if err := os.WriteFile(proc, []byte("1 mem\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, reason = probeCapability("debian", lvmOnly, dir)
	if ok || reason != reasonDMMissing {
		t.Fatalf("kernel absent: ok=%v reason=%q", ok, reason)
	}
	if err := os.WriteFile(proc, []byte("253 device-mapper\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, reason = probeCapability("debian", lvmOnly, dir)
	if !ok || reason != "" {
		t.Fatalf("lvm plus device-mapper must be available even without thin_check: ok=%v reason=%q", ok, reason)
	}
}

func TestClipCapabilityReasonRuneSafe(t *testing.T) {
	prefix := strings.Repeat("a", capabilityReasonMax-1)
	got := clipCapabilityReason(prefix + "é")
	if !utf8.ValidString(got) {
		t.Fatalf("invalid utf8: %q", got)
	}
}

func fixtureRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sys", "block"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "proc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proc", "devices"), []byte("253 device-mapper\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeLV(t *testing.T, root, name, uuid, mapper string) {
	t.Helper()
	d := filepath.Join(root, "sys", "block", name, "dm")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if uuid != "" {
		if err := os.WriteFile(filepath.Join(d, "uuid"), []byte(uuid), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if mapper != "" {
		if err := os.WriteFile(filepath.Join(d, "name"), []byte(mapper), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCollect(t *testing.T) {
	snap, err := Collect(context.Background(), t.TempDir(), "debian")
	if err != nil || snap.Tooling.Family != "debian" || snap.KernelDM.Present {
		t.Fatalf("%+v %v", snap, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Collect(ctx, t.TempDir(), "debian"); err == nil {
		t.Fatal("cancelled collect returned no error")
	}
}
