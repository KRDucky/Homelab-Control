// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package lvminfo

import (
	"context"
	"encoding/json"
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

func TestCollectKeepsLVMPrefixOnly(t *testing.T) {
	dir := fixtureRoot(t)
	writeLV(t, dir, "dm-0", "LVM-SECRETUUID\n", "vg-lv\n")
	writeLV(t, dir, "dm-1", "CRYPT-not-lvm\n", "crypt-keep")
	writeLV(t, dir, "dm-2", "LVM-OTHER", "bad/name")
	writeLV(t, dir, "dm-3", "LVM-DOTS", "pool/../escape")
	writeLV(t, dir, "dm-8", "LVM-SLASH", "vg/lv")
	writeLV(t, dir, "dm-4", "LVM-THINMETA", "vg-pool_tmeta")
	writeLV(t, dir, "dm-5", "LVM-CACHE", "vg-cache_corig")
	writeLV(t, dir, "sda", "LVM-DISK", "not-dm")
	writeLV(t, dir, "dm-x", "LVM-LETTERS", "letters")
	snap, err := Collect(context.Background(), dir, "debian")
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Lvm.Present || snap.Lvm.DevicesTruncated || snap.Lvm.Reason != "" {
		t.Fatalf("%+v", snap.Lvm)
	}
	if !snap.KernelDM.Present {
		t.Fatalf("kernel: %+v", snap.KernelDM)
	}
	got := map[string]string{}
	for _, d := range snap.Lvm.Devices {
		got[d.Name] = d.Mapper
	}
	if len(got) != 4 || got["dm-0"] != "vg-lv" || got["dm-2"] != "" || got["dm-3"] != "" || got["dm-8"] != "" {
		t.Fatalf("devices: %+v", snap.Lvm.Devices)
	}
	if _, ok := got["dm-4"]; ok {
		t.Fatal("thin metadata must not be listed")
	}
	if _, ok := got["dm-5"]; ok {
		t.Fatal("cache origin must not be listed")
	}
	if snap.Tooling.Optional[0].Package != "thin-provisioning-tools" {
		t.Fatalf("thin pool observed, package must stay: %+v", snap.Tooling.Optional[0])
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "LVM-") || strings.Contains(string(raw), "SECRET") || strings.Contains(string(raw), "CRYPT-") || strings.Contains(string(raw), "_tmeta") || strings.Contains(string(raw), "_corig") {
		t.Fatalf("uuid or internal name leaked: %s", raw)
	}
}

func TestCollectTruncatesDevices(t *testing.T) {
	dir := fixtureRoot(t)
	for i := 0; i < deviceMax+1; i++ {
		writeLV(t, dir, "dm-"+itoa(i), "LVM-ROW", "vg-lv"+itoa(i))
	}
	snap, err := Collect(context.Background(), dir, "debian")
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Lvm.Present || !snap.Lvm.DevicesTruncated || len(snap.Lvm.Devices) != deviceMax {
		t.Fatalf("n=%d truncated=%v present=%v", len(snap.Lvm.Devices), snap.Lvm.DevicesTruncated, snap.Lvm.Present)
	}
	if snap.Lvm.Devices[0].Name != "dm-0" || snap.Lvm.Devices[deviceMax-1].Name != "dm-"+itoa(deviceMax-1) {
		t.Fatalf("keep the lowest dm numbers: first=%s last=%s", snap.Lvm.Devices[0].Name, snap.Lvm.Devices[deviceMax-1].Name)
	}
	if snap.Lvm.Reason != "" {
		t.Fatalf("truncated inventory must stay present: %q", snap.Lvm.Reason)
	}
}

func TestCollectScanCapTruncates(t *testing.T) {
	dir := fixtureRoot(t)
	for i := 0; i < dirReadMax+1; i++ {
		if err := os.Mkdir(filepath.Join(dir, "sys", "block", "pad-"+itoa(i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	snap, err := Collect(context.Background(), dir, "unknown")
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Lvm.DevicesTruncated || len(snap.Lvm.Devices) != 0 {
		t.Fatalf("n=%d truncated=%v", len(snap.Lvm.Devices), snap.Lvm.DevicesTruncated)
	}
}

func TestCollectUnreadableInventory(t *testing.T) {
	dir := t.TempDir()
	snap, err := Collect(context.Background(), dir, "debian")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Lvm.Present || snap.Lvm.Reason != "device inventory could not be read" || len(snap.Lvm.Devices) != 0 || snap.Lvm.DevicesTruncated {
		t.Fatalf("%+v", snap.Lvm)
	}
}

func TestCollectEmptyIsHonest(t *testing.T) {
	dir := fixtureRoot(t)
	snap, err := Collect(context.Background(), dir, "debian")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Lvm.Devices) != 0 || snap.Lvm.DevicesTruncated {
		t.Fatalf("%+v", snap.Lvm)
	}
	if snap.Tooling.Optional[0].ID != "thin_check" || snap.Tooling.Optional[0].Package != "" {
		t.Fatalf("no thin pool, package must be cleared: %+v", snap.Tooling.Optional[0])
	}
	if snap.Tooling.RequiredInstalled("lvm") {
		if !snap.Lvm.Present || snap.Lvm.Reason != "" {
			t.Fatalf("installed lvm with no devices: %+v", snap.Lvm)
		}
		return
	}
	if snap.Lvm.Present || snap.Lvm.Reason != "lvm binary was not found" {
		t.Fatalf("missing lvm with no devices: %+v", snap.Lvm)
	}
}

func TestCollectSortsLeadingZeros(t *testing.T) {
	dir := fixtureRoot(t)
	writeLV(t, dir, "dm-10", "LVM-TEN", "vg-ten")
	writeLV(t, dir, "dm-2", "LVM-TWO", "vg-two")
	writeLV(t, dir, "dm-01", "LVM-ONE", "vg-one")
	snap, err := Collect(context.Background(), dir, "debian")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Lvm.Devices) != 3 {
		t.Fatalf("%+v", snap.Lvm.Devices)
	}
	if snap.Lvm.Devices[0].Name != "dm-01" || snap.Lvm.Devices[1].Name != "dm-2" || snap.Lvm.Devices[2].Name != "dm-10" {
		t.Fatalf("numeric order: %+v", snap.Lvm.Devices)
	}
}

func TestCollectSkipsOversizedUUIDAndEscapingSymlink(t *testing.T) {
	dir := fixtureRoot(t)
	writeLV(t, dir, "dm-1", "LVM-KEEP", "vg-lv")
	big := "LVM-" + strings.Repeat("A", uuidReadMax)
	writeLV(t, dir, "dm-2", big, "vg-big")
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("LVM-LEAKED-SECRET\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(dir, "sys", "block", "dm-4", "dm")
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(linkDir, "uuid")); err != nil {
		t.Fatal(err)
	}
	snap, err := Collect(context.Background(), dir, "debian")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, d := range snap.Lvm.Devices {
		got[d.Name] = d.Mapper
	}
	if len(got) != 1 || got["dm-1"] != "vg-lv" {
		t.Fatalf("devices: %+v", snap.Lvm.Devices)
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "LEAKED") || strings.Contains(string(raw), "LVM-") {
		t.Fatalf("leaked: %s", raw)
	}
}

func TestCollectDevicesContextStopsEvery64(t *testing.T) {
	dir := fixtureRoot(t)
	const n = ctxCheckEvery + 16
	for i := 0; i < n; i++ {
		writeLV(t, dir, "dm-"+itoa(i), "LVM-ROW", "vg-lv"+itoa(i))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	devices, truncated, seen, thin, err := collectDevices(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || !seen || thin || len(devices) != ctxCheckEvery {
		t.Fatalf("n=%d truncated=%v seen=%v thin=%v", len(devices), truncated, seen, thin)
	}
	if devices[0].Name != "dm-0" || devices[ctxCheckEvery-1].Name != "dm-"+itoa(ctxCheckEvery-1) {
		t.Fatalf("stopped group: first=%s last=%s", devices[0].Name, devices[len(devices)-1].Name)
	}
	live, liveTrunc, _, _, err := collectDevices(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if liveTrunc || len(live) != n {
		t.Fatalf("live walk n=%d truncated=%v", len(live), liveTrunc)
	}
}

func TestCollectHiddenInternalsDoNotConsumeCap(t *testing.T) {
	dir := fixtureRoot(t)
	internals := []string{
		"vg-pool_tdata",
		"vg-pool_tmeta",
		"vg-cache_cmeta",
		"vg-cache_cdata",
		"vg-cache_corig",
		"vg-lv_mimage_0",
		"vg-lv_mlog",
		"vg-lv_rimage_1",
		"vg-lv_rmeta_2",
		"vg-pool_pmspare",
		"vg-origin_vorigin",
		"vg-snap-real",
		"vg-snap-cow",
	}
	for i, mapper := range internals {
		writeLV(t, dir, "dm-"+itoa(i), "LVM-INT", mapper)
	}
	// Lookalikes sit in the kept window: user LV "real" and user LV "foo-cow".
	writeLV(t, dir, "dm-13", "LVM-REAL", "vg-real")
	writeLV(t, dir, "dm-14", "LVM-COW", "vg-foo--cow")
	const publicN = 255
	for i := 0; i < publicN; i++ {
		writeLV(t, dir, "dm-"+itoa(15+i), "LVM-PUB", "vg-lv"+itoa(i))
	}
	snap, err := Collect(context.Background(), dir, "debian")
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Lvm.DevicesTruncated || len(snap.Lvm.Devices) != deviceMax {
		t.Fatalf("n=%d truncated=%v", len(snap.Lvm.Devices), snap.Lvm.DevicesTruncated)
	}
	if snap.Lvm.Devices[0].Name != "dm-13" || snap.Lvm.Devices[0].Mapper != "vg-real" {
		t.Fatalf("first kept: %+v", snap.Lvm.Devices[0])
	}
	if snap.Lvm.Devices[1].Name != "dm-14" || snap.Lvm.Devices[1].Mapper != "vg-foo--cow" {
		t.Fatalf("escaped cow name must stay: %+v", snap.Lvm.Devices[1])
	}
	last := snap.Lvm.Devices[deviceMax-1].Name
	if last != "dm-"+itoa(15+deviceMax-3) {
		t.Fatalf("last kept=%s", last)
	}
	if snap.Tooling.Optional[0].ID != "thin_check" || snap.Tooling.Optional[0].Package != "thin-provisioning-tools" {
		t.Fatalf("thin component among hidden lows must keep the package: %+v", snap.Tooling.Optional[0])
	}
	raw, err := json.Marshal(snap.Lvm.Devices)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"_tdata", "_tmeta", "_mimage", "_mlog", "_rimage", "_rmeta", "_pmspare", "_vorigin", "snap-real", "snap-cow"} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("internal leaked %s: %s", leak, raw)
		}
	}
}

func TestCollectThinAboveCap(t *testing.T) {
	dir := fixtureRoot(t)
	for i := 0; i < deviceMax+1; i++ {
		writeLV(t, dir, "dm-"+itoa(i), "LVM-ROW", "vg-lv"+itoa(i))
	}
	writeLV(t, dir, "dm-400", "LVM-THIN", "vg-pool_tdata")
	snap, err := Collect(context.Background(), dir, "debian")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Lvm.Devices) != deviceMax || !snap.Lvm.DevicesTruncated {
		t.Fatalf("n=%d truncated=%v", len(snap.Lvm.Devices), snap.Lvm.DevicesTruncated)
	}
	if snap.Tooling.Optional[0].Package != "thin-provisioning-tools" {
		t.Fatalf("thin above the cap must still name the package: %+v", snap.Tooling.Optional[0])
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "_tdata") {
		t.Fatalf("thin internal listed: %s", raw)
	}
}

func TestCollectInvalidMapperStillMarksThin(t *testing.T) {
	dir := fixtureRoot(t)
	writeLV(t, dir, "dm-1", "LVM-THIN", "bad/pool_tdata")
	writeLV(t, dir, "dm-2", "LVM-PUB", "vg/lv")
	snap, err := Collect(context.Background(), dir, "debian")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Lvm.Devices) != 1 || snap.Lvm.Devices[0].Name != "dm-2" || snap.Lvm.Devices[0].Mapper != "" {
		t.Fatalf("devices: %+v", snap.Lvm.Devices)
	}
	if snap.Tooling.Optional[0].ID != "thin_check" || snap.Tooling.Optional[0].Package != "thin-provisioning-tools" {
		t.Fatalf("invalid thin mapper must still keep the package: %+v", snap.Tooling.Optional[0])
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "_tdata") {
		t.Fatalf("internal leaked: %s", raw)
	}
}

func TestCollectUnreadableNameIsNotThin(t *testing.T) {
	dir := fixtureRoot(t)
	writeLV(t, dir, "dm-1", "LVM-NONAME", "")
	snap, err := Collect(context.Background(), dir, "debian")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Lvm.Devices) != 1 || snap.Lvm.Devices[0].Mapper != "" {
		t.Fatalf("devices: %+v", snap.Lvm.Devices)
	}
	if snap.Tooling.Optional[0].ID != "thin_check" || snap.Tooling.Optional[0].Package != "" {
		t.Fatalf("unreadable name must not mark thin: %+v", snap.Tooling.Optional[0])
	}
}

func TestCollectCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Collect(ctx, t.TempDir(), "debian"); err == nil {
		t.Fatal("cancelled collect must error")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
