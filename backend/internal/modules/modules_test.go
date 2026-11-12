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
	if len(all) != 4 || all[0].ID != "services" || all[0].APIPrefix != "/api/v1/services" || all[0].OpPrefix != "services." {
		t.Fatalf("registry: %+v (new modules need review)", all)
	}
	if all[1].ID != "packages" || all[1].APIPrefix != "/api/v1/packages" || all[1].OpPrefix != "packages." || all[1].Path != "/software" || len(all[1].Scopes) != 1 || all[1].Scopes[0] != "packages.read" {
		t.Fatalf("packages module: %+v (new modules need review)", all[1])
	}
	if all[2].ID != "networking" || all[2].OpPrefix != "" {
		t.Fatalf("networking registry: %+v", all[2])
	}
	if all[3].ID != "mergerfs" || all[3].APIPrefix != "/api/v1/mergerfs" || all[3].OpPrefix != "" || all[3].Path != "/storage/mergerfs" || len(all[3].Scopes) != 1 || all[3].Scopes[0] != "mergerfs.read" {
		t.Fatalf("mergerfs module: %+v (new modules need review)", all[3])
	}
	for path, want := range map[string]string{"/api/v1/services": "services", "/api/v1/services/units/{unit}/plan": "services",
		"/api/v1/servicesx": "", "/api/v1/audit": "", "/api/v1/operations": "", "/api/v1/capabilities": "",
		"/api/v1/files": "", "/api/v1/files/roots": "", "/api/v1/files/entries": "", "/": "",
		"/api/v1/networking/reverse-proxy/traefik/inventory": "networking", "/api/v1/networkingx": "", "/api/v1/packages": "packages", "/api/v1/packages/search": "packages", "/api/v1/packagesx": "", "/api/v1/mergerfs": "mergerfs", "/api/v1/mergerfsx": ""} {
		if got := ForRoute(path); got != want {
			t.Errorf("ForRoute(%q) = %q, want %q", path, got, want)
		}
	}
	for op, want := range map[string]string{"services.unit.apply": "services", "operations.journal.list": "", "broker.info": "", "servicesx": "",
		"host.settings.plan": "", "host.settings.apply": "", "packages.installed": "packages", "packages.search": "packages", "packagesx": ""} {
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
	for _, bad := range []string{``, `{}`, `{"version":2,"disabled":[]}`, `{"version":1,"disabled":["storage"]}`,
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
	if s := LoadOrFailClosed(p, false); s.Enabled("services") || len(s.Disabled()) != 4 {
		t.Fatal("invalid setting must disable every module")
	}
}
