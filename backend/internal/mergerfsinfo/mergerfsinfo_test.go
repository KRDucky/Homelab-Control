// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsinfo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const mountFixture = `36 29 0:28 / /mnt/media rw,nosuid,nodev,relatime shared:123 - fuse.mergerfs /mnt/disk1:/mnt/disk2 rw,default_permissions,allow_other,category.create=epmfs,minfreespace=20G,user=secret,password=hunter2
37 29 0:29 / / rw - ext4 /dev/sda1 rw
38 29 0:30 / / rw - fuseblk /mnt/disk3:/mnt/disk4 rw,fsname=pool,use_ino
39 29 0:31 / / rw - cifs //user:pass@host/share rw,password=secret
`

func TestParseMountInfoKeepsOnlyMergerFS(t *testing.T) {
	pools, err := ParseMountInfo(strings.NewReader(mountFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(pools) != 2 {
		t.Fatalf("pools: %+v", pools)
	}
	if pools[0].Mountpoint != "/mnt/media" || len(pools[0].Branches) != 2 ||
		pools[0].Branches[0] != "/mnt/disk1" || pools[0].Branches[1] != "/mnt/disk2" {
		t.Fatalf("pool 0: %+v", pools[0])
	}
	opts := strings.Join(pools[0].Options, ",")
	for _, want := range []string{"allow_other", "category.create=epmfs", "minfreespace=20G"} {
		if !strings.Contains(opts, want) {
			t.Fatalf("options missing %q: %q", want, opts)
		}
	}
	for _, secret := range []string{"user=secret", "password=hunter2", "hunter2", "user:pass"} {
		if strings.Contains(opts, secret) || strings.Contains(strings.Join(pools[0].Branches, ":"), secret) {
			t.Fatalf("credential retained: %q in %q", secret, opts)
		}
	}
	// fuseblk accepted only because its source is clearly mergerfs branches.
	if pools[1].Mountpoint != "/" || pools[1].Branches[0] != "/mnt/disk3" || pools[1].Branches[1] != "/mnt/disk4" {
		t.Fatalf("pool 1 (fuseblk): %+v", pools[1])
	}
}

func TestParseMountInfoRejectsUnsafeBranches(t *testing.T) {
	for _, in := range []string{
		"1 0 0:1 / /mnt rw - fuse.mergerfs relative:/mnt/disk2 rw\n",
		"1 0 0:1 / /mnt rw - fuse.mergerfs /mnt/disk1: rw\n",
		"1 0 0:1 / /mnt rw - fuse.mergerfs /mnt/../etc:/mnt/disk2 rw\n",
		"1 0 0:1 / /mnt rw - fuse.mergerfs /mnt/disk1:/mnt/disk2\n", // missing super-options
		"1 0 0:1 / /mnt rw - fuseblk /mnt/disk1 rw\n",               // single-branch fuseblk not mergerfs
	} {
		pools, err := ParseMountInfo(strings.NewReader(in))
		if err == nil && len(pools) != 0 {
			t.Fatalf("accepted %q -> %+v", in, pools)
		}
	}
}

func TestCollectNoMountsNoConfig(t *testing.T) {
	dir := t.TempDir()
	snap, err := Collect(context.Background(), dir, filepath.Join(dir, "missing.conf"), "debian")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Tooling.Family != "debian" {
		t.Fatalf("tooling: %+v", snap.Tooling)
	}
	if len(snap.MergerFS.Pools) != 0 {
		t.Fatalf("mergerfs pools: %+v", snap.MergerFS.Pools)
	}
	// With no mounts, mergerfs.present mirrors the binary gate.
	if got, want := snap.MergerFS.Present, snap.Tooling.RequiredInstalled("mergerfs"); got != want {
		t.Fatalf("mergerfs present %v, binary gate %v", got, want)
	}
	if snap.SnapRAID.Present || snap.SnapRAID.Reason != "optional: no snapraid.conf" {
		t.Fatalf("snapraid: %+v", snap.SnapRAID)
	}
	if snap.KernelFuse.Present || snap.KernelFuse.Reason != reasonFuseUnread {
		t.Fatalf("kernel fuse without proc: %+v", snap.KernelFuse)
	}
	if snap.Warnings == nil || len(snap.Warnings) != 0 || snap.WarningsTruncated {
		t.Fatalf("warnings must be an empty list: %+v trunc=%v", snap.Warnings, snap.WarningsTruncated)
	}
}

func TestCollectParseFailureFailsClosed(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "proc", "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proc", "self", "mountinfo"), []byte("garbage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := Collect(context.Background(), dir, filepath.Join(dir, "missing.conf"), "debian")
	if err != nil {
		t.Fatal(err)
	}
	if snap.MergerFS.Present || snap.MergerFS.Reason != "mount inventory could not be read" {
		t.Fatalf("mergerfs must fail closed: %+v", snap.MergerFS)
	}
	if len(snap.MergerFS.Pools) != 0 {
		t.Fatalf("mergerfs pools: %+v", snap.MergerFS.Pools)
	}
}

func TestProbeToolingPresent(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"mergerfs", "fusermount3", "snapraid"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fixture"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stat := func(path string) bool {
		fi, err := os.Stat(filepath.Join(dir, filepath.Base(path)))
		return err == nil && fi.Mode().IsRegular()
	}
	tool := probeTooling("debian", stat)
	if tool.Family != "debian" || len(tool.Required) != 2 || len(tool.Optional) != 1 {
		t.Fatalf("%+v", tool)
	}
	req := tool.Required[0]
	if req.ID != "mergerfs" || req.Package != "mergerfs" || !req.Installed || req.Binary != "/usr/bin/mergerfs" {
		t.Fatalf("required mergerfs: %+v", req)
	}
	fuse := tool.Required[1]
	if fuse.ID != "fusermount3" || fuse.Package != "fuse3" || !fuse.Installed || fuse.Binary != "/usr/bin/fusermount3" {
		t.Fatalf("required fusermount3: %+v", fuse)
	}
	opt := tool.Optional[0]
	if opt.ID != "snapraid" || opt.Package != "snapraid" || !opt.Installed || opt.Binary != "/usr/bin/snapraid" {
		t.Fatalf("optional: %+v", opt)
	}
}

func TestProbeToolingMissingAndPackageNames(t *testing.T) {
	stat := func(string) bool { return false }
	for _, family := range []string{"debian", "fedora", "suse", "arch"} {
		tool := probeTooling(family, stat)
		if len(tool.Required) != 2 || tool.Required[0].Installed || tool.Required[1].Installed {
			t.Fatalf("%s required installed: %+v", family, tool.Required)
		}
		if tool.Required[0].Package != "mergerfs" || tool.Required[1].Package != "fuse3" || tool.Optional[0].Package != "snapraid" {
			t.Fatalf("%s package names: %+v", family, tool)
		}
	}
	unknown := probeTooling("unknown", stat)
	if unknown.Required[0].Package != "" || unknown.Required[1].Package != "" || unknown.Optional[0].Package != "" {
		t.Fatalf("unknown family must not name a package: %+v", unknown)
	}
	if unknown.Required[0].Installed || unknown.Required[1].Installed || unknown.Optional[0].Installed {
		t.Fatalf("unknown family probe: %+v", unknown)
	}
}

func TestParseSnapRAIDConfig(t *testing.T) {
	cfg := `# Example data
data d1 /mnt/disk1
data d2 /mnt/disk2
parity /mnt/parity1,/mnt/parity2
3-parity /mnt/parity3
content /var/snapraid/content
autosave 500
nohidden
`
	out, err := ParseSnapRAIDConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Data) != 2 || len(out.Parity) != 3 || len(out.Content) != 1 {
		t.Fatalf("%+v", out)
	}
	if out.Data[1] != "/mnt/disk2" || out.Parity[0] != "/mnt/parity1" || out.Parity[1] != "/mnt/parity2" || out.Parity[2] != "/mnt/parity3" || out.Content[0] != "/var/snapraid/content" {
		t.Fatalf("%+v", out)
	}
}

func TestParseSnapRAIDConfigRejectsGarbage(t *testing.T) {
	for _, bad := range []string{
		"data d1\n",                  // missing path
		"data d1 relative/path\n",    // non-absolute path
		"data d1 /mnt/disk1 extra\n", // trailing garbage
		"data bad/name /mnt/disk1\n", // invalid name charset
		"parity relative\n",          // non-absolute
		"parity /mnt/p1 extra\n",     // trailing garbage
	} {
		if _, err := ParseSnapRAIDConfig(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestReadSnapRAIDRefusesSymlinkAndOversize(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.conf")
	if err := os.WriteFile(real, []byte("data d1 /mnt/disk1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.conf")
	_ = os.Symlink(real, link)
	if out := ReadSnapRAID(link); out.Present {
		t.Fatalf("followed symlink: %+v", out)
	}
	big := filepath.Join(dir, "big.conf")
	if err := os.WriteFile(big, make([]byte, snapraidMaxBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := ReadSnapRAID(big); out.Present {
		t.Fatalf("accepted oversize config: %+v", out)
	}
}
