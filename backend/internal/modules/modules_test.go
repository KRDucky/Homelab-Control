// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package modules

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryIsClosed(t *testing.T) {
	all := All()
	if len(all) != 15 || all[0].ID != "services" || all[0].APIPrefix != "/api/v1/services" || all[0].OpPrefix != "services." {
		t.Fatalf("registry: %+v (new modules need review)", all)
	}
	// M11 read-only networking: the broker-backed modules own their op
	// prefix so a disabled module also refuses its op; bonds has no op.
	for i, want := range []Module{
		{ID: "ports", APIPrefix: "/api/v1/ports", OpPrefix: "network.ports.", Path: "/network/ports", Scopes: []string{"networking.ports.read"}},
		{ID: "bonds", APIPrefix: "/api/v1/bonds", OpPrefix: "", Path: "/network/bonds", Scopes: []string{"networking.bonds.read"}},
		{ID: "firewall", APIPrefix: "/api/v1/firewall", OpPrefix: "network.firewall.", Path: "/network/firewall", Scopes: []string{"networking.firewall.read"}},
	} {
		got := all[12+i]
		if got.ID != want.ID || got.APIPrefix != want.APIPrefix || got.OpPrefix != want.OpPrefix || got.Path != want.Path || len(got.Scopes) != 1 || got.Scopes[0] != want.Scopes[0] {
			t.Fatalf("M11 module %d: %+v (new modules need review)", i, got)
		}
	}
	if ForOp("network.ports.list") != "ports" || ForOp("network.firewall.config") != "firewall" || ForRoute("/api/v1/networking/x") != "networking" {
		t.Fatal("M11 op/route ownership")
	}
	if all[1].ID != "storage" || all[1].APIPrefix != "/api/v1/storage" || all[1].OpPrefix != "storage." || all[1].Path != "/storage" || len(all[1].Scopes) != 4 || all[1].Scopes[0] != "storage.read" || all[1].Scopes[1] != "storage.probe" || all[1].Scopes[2] != "layout.manage" || all[1].Scopes[3] != "storage.partition" {
		t.Fatalf("storage module: %+v (new modules need review)", all[1])
	}
	if all[2].ID != "packages" || all[2].APIPrefix != "/api/v1/packages" || all[2].OpPrefix != "packages." || all[2].Path != "/software" || len(all[2].Scopes) != 1 || all[2].Scopes[0] != "packages.read" {
		t.Fatalf("packages module: %+v (new modules need review)", all[2])
	}
	if all[3].ID != "networking" || all[3].OpPrefix != "" {
		t.Fatalf("networking registry: %+v", all[3])
	}
	if all[4].ID != "mergerfs" || all[4].APIPrefix != "/api/v1/mergerfs" || all[4].OpPrefix != "mergerfs." || all[4].Path != "/storage/mergerfs" || len(all[4].Scopes) != 3 || all[4].Scopes[0] != "mergerfs.read" || all[4].Scopes[1] != "mergerfs.configure" || all[4].Scopes[2] != "mergerfs.maintain" {
		t.Fatalf("mergerfs module: %+v (new modules need review)", all[4])
	}
	if all[5].ID != "zfs" || all[5].APIPrefix != "/api/v1/zfs" || all[5].OpPrefix != "" || all[5].Path != "/storage/zfs" || len(all[5].Scopes) != 1 || all[5].Scopes[0] != "zfs.read" {
		t.Fatalf("zfs module: %+v (new modules need review)", all[5])
	}
	if all[6].ID != "lvm" || all[6].APIPrefix != "/api/v1/lvm" || all[6].OpPrefix != "" || all[6].Path != "/storage/lvm" || len(all[6].Scopes) != 1 || all[6].Scopes[0] != "lvm.read" {
		t.Fatalf("lvm module: %+v (new modules need review)", all[6])
	}
	if all[7].ID != "btrfs" || all[7].APIPrefix != "/api/v1/btrfs" || all[7].OpPrefix != "" || all[7].Path != "/storage/btrfs" || len(all[7].Scopes) != 1 || all[7].Scopes[0] != "btrfs.read" {
		t.Fatalf("btrfs module: %+v (new modules need review)", all[7])
	}
	if all[8].ID != "ext4" || all[8].APIPrefix != "/api/v1/ext4" || all[8].OpPrefix != "ext4." || all[8].Path != "/storage/ext4" || len(all[8].Scopes) != 2 || all[8].Scopes[0] != "ext4.read" || all[8].Scopes[1] != "ext4.format" {
		t.Fatalf("ext4 module: %+v (new modules need review)", all[8])
	}
	if all[9].ID != "mdadm" || all[9].APIPrefix != "/api/v1/mdadm" || all[9].OpPrefix != "" || all[9].Path != "/storage/mdadm" || len(all[9].Scopes) != 1 || all[9].Scopes[0] != "mdadm.read" {
		t.Fatalf("mdadm module: %+v (new modules need review)", all[9])
	}
	if all[10].ID != "f2fs" || all[10].APIPrefix != "/api/v1/f2fs" || all[10].OpPrefix != "" || all[10].Path != "/storage/f2fs" || len(all[10].Scopes) != 1 || all[10].Scopes[0] != "f2fs.read" {
		t.Fatalf("f2fs module: %+v (new modules need review)", all[10])
	}
	if all[11].ID != "xfs" || all[11].APIPrefix != "/api/v1/xfs" || all[11].OpPrefix != "" || all[11].Path != "/storage/xfs" || len(all[11].Scopes) != 1 || all[11].Scopes[0] != "xfs.read" {
		t.Fatalf("xfs module: %+v (new modules need review)", all[11])
	}
	for path, want := range map[string]string{"/api/v1/services": "services", "/api/v1/services/units/{unit}/plan": "services",
		"/api/v1/servicesx": "", "/api/v1/audit": "", "/api/v1/operations": "", "/api/v1/capabilities": "",
		"/api/v1/files": "", "/api/v1/files/roots": "", "/api/v1/files/entries": "", "/": "",
		"/api/v1/storage": "storage", "/api/v1/storage/probe": "storage", "/api/v1/storage/plan/dry-run": "storage", "/api/v1/storage/partition/plan": "storage", "/api/v1/storage/partition/apply": "storage", "/api/v1/storage/layout/chassis": "storage", "/api/v1/storagex": "",
		"/api/v1/networking/reverse-proxy/traefik/inventory": "networking", "/api/v1/networkingx": "", "/api/v1/packages": "packages", "/api/v1/packages/search": "packages", "/api/v1/packagesx": "", "/api/v1/mergerfs": "mergerfs", "/api/v1/mergerfs/snapraid/status": "mergerfs", "/api/v1/mergerfs/pool/plan": "mergerfs", "/api/v1/mergerfs/maintain/apply": "mergerfs", "/api/v1/mergerfsx": "", "/api/v1/zfs": "zfs", "/api/v1/zfsx": "", "/api/v1/lvm": "lvm", "/api/v1/lvmx": "", "/api/v1/btrfs": "btrfs", "/api/v1/btrfsx": "", "/api/v1/ext4": "ext4", "/api/v1/ext4x": "", "/api/v1/mdadm": "mdadm", "/api/v1/mdadmx": "", "/api/v1/f2fs": "f2fs", "/api/v1/f2fsx": "", "/api/v1/xfs": "xfs", "/api/v1/xfsx": ""} {
		if got := ForRoute(path); got != want {
			t.Errorf("ForRoute(%q) = %q, want %q", path, got, want)
		}
	}
	for op, want := range map[string]string{"services.unit.apply": "services", "operations.journal.list": "", "broker.info": "", "servicesx": "", "storage.probe": "storage",
		"host.settings.plan": "", "host.settings.apply": "", "packages.installed": "packages", "packages.search": "packages", "packagesx": "",
		"mergerfs.snapraid.status": "mergerfs", "mergerfs.pool.apply": "mergerfs", "mergerfs.maintain.apply": "mergerfs", "mergerfsx.status": "",
		"ext4.format.plan": "ext4", "ext4.format.apply": "ext4", "storage.partition.plan": "storage", "storage.partition.apply": "storage"} {
		if got := ForOp(op); got != want {
			t.Errorf("ForOp(%q) = %q", op, got)
		}
	}
}

func TestParseIsStrict(t *testing.T) {
	if s, err := Parse([]byte(`{"version":1,"disabled":["services"]}`)); err != nil || s.Enabled("services") || !s.Enabled("") {
		t.Fatalf("%v %v", s, err)
	}
	if s, err := Parse([]byte(`{"version":1,"disabled":[]}`)); err != nil || !s.Enabled("services") {
		t.Fatalf("%v %v", s, err)
	}
	for _, bad := range []string{``, `{}`, `{"version":2,"disabled":[]}`, `{"version":1,"disabled":["does-not-exist"]}`,
		`{"version":1,"disabled":[],"enabled":["x"]}`, `{"version":1,"disabled":"services"}`, `{"version":1}{"version":1}`, `[1]`} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestLoadFailsClosed(t *testing.T) {
	dir := t.TempDir()
	if s := LoadOrFailClosed(filepath.Join(dir, "missing.json"), true); !s.Enabled("services") || s.Reason != "" {
		t.Fatal("missing file must mean defaults")
	}
	p := filepath.Join(dir, "m.json")
	if err := os.WriteFile(p, []byte(`{"version":1,"disabled":["services"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if s, err := Load(p, false); err != nil || s.Enabled("services") {
		t.Fatalf("%v %v", s, err)
	}
	// Not root-owned (tests run as non-root): refused in production mode.
	if os.Getuid() != 0 {
		if s := LoadOrFailClosed(p, true); s.Enabled("services") || s.Reason == "" {
			t.Fatal("non-root-owned setting must fail closed")
		}
	}
	_ = os.Chmod(p, 0o666)
	if _, err := Load(p, false); err == nil {
		t.Fatal("world-writable setting accepted")
	}
	_ = os.Chmod(p, 0o644)
	ln := filepath.Join(dir, "link.json")
	_ = os.Symlink(p, ln)
	if s := LoadOrFailClosed(ln, false); s.Enabled("services") {
		t.Fatal("symlinked setting must fail closed")
	}
	_ = os.WriteFile(p, []byte(`{"version":1,"disabled":["nope"]}`), 0o644)
	if s := LoadOrFailClosed(p, false); s.Enabled("services") || len(s.Disabled()) != 15 {
		t.Fatal("invalid setting must disable every module")
	}
}
