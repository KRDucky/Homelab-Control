// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsinfo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestParseFstabLineKeepsMergerFSOnly(t *testing.T) {
	c, ok := parseFstabLine("/mnt/disk1:/mnt/disk2 /mnt/media fuse.mergerfs allow_other,category.create=epmfs,user=secret,password=hunter2 0 0")
	if !ok {
		t.Fatal("expected mergerfs fstab line")
	}
	if c.Origin != originFstab || c.Path != fstabDisplayPath || c.Mountpoint != "/mnt/media" {
		t.Fatalf("meta: %+v", c)
	}
	if len(c.Branches) != 2 || c.Branches[0] != "/mnt/disk1" || c.Branches[1] != "/mnt/disk2" {
		t.Fatalf("branches: %+v", c.Branches)
	}
	opts := strings.Join(c.Options, ",")
	if !strings.Contains(opts, "allow_other") || !strings.Contains(opts, "category.create=epmfs") {
		t.Fatalf("options: %q", opts)
	}
	for _, secret := range []string{"user=secret", "password=hunter2", "hunter2"} {
		if strings.Contains(opts, secret) {
			t.Fatalf("credential retained: %q in %q", secret, opts)
		}
	}

	for _, line := range []string{
		"# /mnt/disk1:/mnt/disk2 /mnt/media fuse.mergerfs defaults 0 0",
		"UUID=abcd / ext4 defaults 0 1",
		"/mnt/disk1:/mnt/disk2 /mnt/media ext4 defaults 0 0",
		"relative:/mnt/disk2 /mnt/media fuse.mergerfs defaults 0 0",
		"/mnt/disk1:/mnt/disk2 media fuse.mergerfs defaults 0 0",
		"/mnt/disk1:/mnt/../etc /mnt/media fuse.mergerfs defaults 0 0",
		"",
	} {
		if _, ok := parseFstabLine(line); ok {
			t.Fatalf("accepted %q", line)
		}
	}
}

func TestParseFstabLineUnescapesAndTrimsSlash(t *testing.T) {
	c, ok := parseFstabLine("/mnt/disk1:/mnt/disk2 /mnt/my\\040pool/ fuse.mergerfs defaults 0 0")
	if !ok || c.Mountpoint != "/mnt/my pool" {
		t.Fatalf("escaped mountpoint: ok=%v %+v", ok, c)
	}
}

func TestParseSystemdMountMergerFS(t *testing.T) {
	text := `[Unit]
Description=media pool
[Mount]
What=/mnt/disk1:/mnt/disk2
Where=/mnt/media/
Type=fuse.mergerfs
Options=allow_other,use_ino,password=hunter2
[Install]
WantedBy=multi-user.target
`
	what, where, typ, options, ok := parseSystemdMount(text)
	if !ok || what != "/mnt/disk1:/mnt/disk2" || where != "/mnt/media/" || typ != "fuse.mergerfs" {
		t.Fatalf("parse: ok=%v %q %q %q", ok, what, where, typ)
	}
	if !strings.Contains(options, "allow_other") {
		t.Fatalf("options: %q", options)
	}

	_, _, extType, _, ok := parseSystemdMount("[Mount]\nWhat=/a:/b\nWhere=/mnt\nType=ext4\n")
	if !ok || extType != "ext4" {
		t.Fatalf("ext4 type should parse for the caller to reject: ok=%v %q", ok, extType)
	}
	if _, _, _, _, ok := parseSystemdMount("[Mount]\n.include /etc/secret\nWhat=/a:/b\nWhere=/mnt\nType=fuse.mergerfs\n"); ok {
		t.Fatal(".include must refuse the unit")
	}
	quoted := "[Mount]\nWhat=\"/a:/b\"\nWhere=\"/mnt/media\"\nType=mergerfs\n"
	what, where, typ, _, ok = parseSystemdMount(quoted)
	if !ok || what != "/a:/b" || where != "/mnt/media" || typ != "mergerfs" {
		t.Fatalf("quoted: ok=%v %q %q %q", ok, what, where, typ)
	}
}

func TestCollectConfiguredFstabAndSystemd(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "etc", "systemd", "system"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "proc", "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	fstab := "/mnt/disk1:/mnt/disk2 /mnt/media fuse.mergerfs allow_other,category.create=epmfs 0 0\n" +
		"/dev/sda1 / ext4 defaults 0 1\n" +
		"/mnt/a:/mnt/b /mnt/unmounted fuse.mergerfs defaults 0 0\n"
	if err := os.WriteFile(filepath.Join(dir, "etc", "fstab"), []byte(fstab), 0o644); err != nil {
		t.Fatal(err)
	}
	unit := "[Mount]\nWhat=/mnt/x:/mnt/y\nWhere=/mnt/other\nType=fuse.mergerfs\nOptions=use_ino\n"
	if err := os.WriteFile(filepath.Join(dir, "etc", "systemd", "system", "mnt-other.mount"), []byte(unit), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "etc", "systemd", "system", "unrelated.service"), []byte("[Service]\nExecStart=/bin/true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proc", "self", "mountinfo"), []byte(
		"36 29 0:28 / /mnt/media rw - fuse.mergerfs /mnt/disk1:/mnt/disk2 rw,allow_other,category.create=epmfs\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}

	snap, err := Collect(context.Background(), dir, filepath.Join(dir, "missing.conf"), "debian")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.MergerFS.Pools) != 1 || snap.MergerFS.Pools[0].Mountpoint != "/mnt/media" {
		t.Fatalf("live pools: %+v", snap.MergerFS.Pools)
	}
	if len(snap.MergerFS.Configured) != 3 {
		t.Fatalf("configured: %+v", snap.MergerFS.Configured)
	}
	byPoint := map[string]ConfiguredMount{}
	for _, c := range snap.MergerFS.Configured {
		byPoint[c.Mountpoint] = c
	}
	if c := byPoint["/mnt/media"]; c.Origin != originFstab || !c.Mounted {
		t.Fatalf("fstab live: %+v", c)
	}
	if c := byPoint["/mnt/unmounted"]; c.Origin != originFstab || c.Mounted {
		t.Fatalf("fstab idle: %+v", c)
	}
	if c := byPoint["/mnt/other"]; c.Origin != originSystemd || c.Unit != "mnt-other.mount" || c.Mounted || c.Path != "/etc/systemd/system/mnt-other.mount" {
		t.Fatalf("systemd: %+v", c)
	}
}

func TestCollectConfiguredSkipsSymlinkFstabAndUnit(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "etc", "systemd", "system"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "outside-fstab")
	if err := os.WriteFile(target, []byte("/mnt/a:/mnt/b /mnt/evil fuse.mergerfs defaults 0 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "etc", "fstab")); err != nil {
		t.Skip("filesystem does not support symlinks")
	}
	unitTarget := filepath.Join(dir, "outside.mount")
	if err := os.WriteFile(unitTarget, []byte("[Mount]\nWhat=/mnt/a:/mnt/b\nWhere=/mnt/evil2\nType=fuse.mergerfs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(unitTarget, filepath.Join(dir, "etc", "systemd", "system", "mnt-evil2.mount")); err != nil {
		t.Skip("filesystem does not support symlinks")
	}
	cfg, truncated := collectConfigured(context.Background(), dir, nil)
	if truncated || len(cfg) != 0 {
		t.Fatalf("symlinks must be skipped: truncated=%v %+v", truncated, cfg)
	}
}

func TestCollectConfiguredCapAndFalsePrefix(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i := 0; i < configuredMax+2; i++ {
		fmt.Fprintf(&b, "/mnt/d1:/mnt/d2 /mnt/p%d fuse.mergerfs defaults 0 0\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "etc", "fstab"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, truncated := collectConfigured(context.Background(), dir, []Pool{{Mountpoint: "/mnt/pool"}})
	if !truncated || len(cfg) != configuredMax {
		t.Fatalf("cap: n=%d truncated=%v", len(cfg), truncated)
	}
}

func TestConfiguredMountedExactMountpoint(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "etc", "fstab"), []byte("/mnt/d1:/mnt/d2 /mnt/pool2 fuse.mergerfs defaults 0 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, truncated := collectConfigured(context.Background(), dir, []Pool{{Mountpoint: "/mnt/pool"}})
	if truncated || len(cfg) != 1 || cfg[0].Mountpoint != "/mnt/pool2" || cfg[0].Mounted {
		t.Fatalf("/mnt/pool must not match /mnt/pool2: %+v truncated=%v", cfg, truncated)
	}
}

func TestCollectConfiguredSkipsTemplatedUnit(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "etc", "systemd", "system"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "etc", "systemd", "system", "mnt-pool@.mount"), []byte("[Mount]\nWhat=/a:/b\nWhere=/mnt/%i\nType=fuse.mergerfs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _ := collectConfigured(context.Background(), dir, nil)
	if len(cfg) != 0 {
		t.Fatalf("templated unit: %+v", cfg)
	}
}

func TestCollectStillImportsConfiguredWhenMountinfoFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "proc", "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proc", "self", "mountinfo"), []byte("garbage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "etc", "fstab"), []byte("/mnt/a:/mnt/b /mnt/media fuse.mergerfs defaults 0 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := Collect(context.Background(), dir, filepath.Join(dir, "missing.conf"), "debian")
	if err != nil {
		t.Fatal(err)
	}
	if snap.MergerFS.Present || snap.MergerFS.Reason != "mount inventory could not be read" {
		t.Fatalf("must fail closed on mounts: %+v", snap.MergerFS)
	}
	if len(snap.MergerFS.Configured) != 1 || snap.MergerFS.Configured[0].Mountpoint != "/mnt/media" || snap.MergerFS.Configured[0].Mounted {
		t.Fatalf("configured still imported: %+v", snap.MergerFS.Configured)
	}
}

func TestValidMountUnitName(t *testing.T) {
	if !validMountUnitName("mnt-media.mount") || !validMountUnitName(`mnt-foo\x2dbar.mount`) {
		t.Fatal("valid names rejected")
	}
	for _, name := range []string{"mnt-media.service", "mnt-pool@.mount", "../etc.mount", ".mount", "foo/bar.mount"} {
		if validMountUnitName(name) {
			t.Fatalf("accepted %q", name)
		}
	}
}

func TestCollectConfiguredCapsBranchesAndDropsSecrets(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	var spec strings.Builder
	for i := 0; i < configuredBranchesMax+8; i++ {
		if i > 0 {
			spec.WriteByte(':')
		}
		fmt.Fprintf(&spec, "/d%d", i)
	}
	line := spec.String() + " /mnt/many fuse.mergerfs defaults,password=hunter2,user=secret 0 0\n"
	if err := os.WriteFile(filepath.Join(dir, "etc", "fstab"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _ := collectConfigured(context.Background(), dir, nil)
	if len(cfg) != 1 || len(cfg[0].Branches) != configuredBranchesMax || !cfg[0].BranchesTruncated {
		t.Fatalf("branch cap: %+v", cfg)
	}
	opts := strings.Join(cfg[0].Options, ",")
	if strings.Contains(opts, "password") || strings.Contains(opts, "hunter2") || strings.Contains(opts, "user=secret") {
		t.Fatalf("credentials retained: %q", opts)
	}
}

func TestCollectConfiguredSkipsIncludeOversizedFIFOAndBadUTF8(t *testing.T) {
	dir := t.TempDir()
	sysd := filepath.Join(dir, "etc", "systemd", "system")
	if err := os.MkdirAll(sysd, 0o755); err != nil {
		t.Fatal(err)
	}
	includeUnit := "[Mount]\n.include /etc/secret\nWhat=/a:/b\nWhere=/mnt/inc\nType=fuse.mergerfs\n"
	if err := os.WriteFile(filepath.Join(sysd, "mnt-inc.mount"), []byte(includeUnit), 0o644); err != nil {
		t.Fatal(err)
	}
	big := "[Mount]\nWhat=/a:/b\nWhere=/mnt/big\nType=fuse.mergerfs\nOptions=" + strings.Repeat("x", unitMaxBytes) + "\n"
	if err := os.WriteFile(filepath.Join(sysd, "mnt-big.mount"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(sysd, "fifo.mount"), 0o600); err != nil {
		t.Skip("filesystem does not support FIFOs")
	}
	fstab := "/mnt/a:/mnt/b /mnt/\xff fuse.mergerfs defaults 0 0\n"
	if err := os.WriteFile(filepath.Join(dir, "etc", "fstab"), []byte(fstab), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _ := collectConfigured(context.Background(), dir, nil)
	if len(cfg) != 0 {
		t.Fatalf("include/oversize/fifo/utf8 must skip: %+v", cfg)
	}
}

func TestCollectConfiguredFindsMountAfterNonMountNames(t *testing.T) {
	dir := t.TempDir()
	sysd := filepath.Join(dir, "etc", "systemd", "system")
	if err := os.MkdirAll(sysd, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < systemdDirBatch+8; i++ {
		name := fmt.Sprintf("dummy-%d.wants", i)
		if err := os.WriteFile(filepath.Join(sysd, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	unit := "[Mount]\nWhat=/mnt/a:/mnt/b\nWhere=/mnt/late\nType=fuse.mergerfs\n"
	if err := os.WriteFile(filepath.Join(sysd, "mnt-late.mount"), []byte(unit), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, truncated := collectConfigured(context.Background(), dir, nil)
	if truncated || len(cfg) != 1 || cfg[0].Mountpoint != "/mnt/late" {
		t.Fatalf("late .mount dropped: truncated=%v %+v", truncated, cfg)
	}
}
