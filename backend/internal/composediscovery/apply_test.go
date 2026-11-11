// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package composediscovery

import (
	"homelab-control/backend/internal/traefikclient"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteYAMLConfined(t *testing.T) {
	dir := t.TempDir()
	name, err := WriteYAML(dir, "notes", "http:\n  routers:\n    notes: {}\n")
	if err != nil || name != "notes.yml" {
		t.Fatal(err, name)
	}
	b, err := os.ReadFile(filepath.Join(dir, "notes.yml"))
	if err != nil || !strings.Contains(string(b), "routers:") {
		t.Fatal(string(b), err)
	}
	if _, err = WriteYAML(dir, "dynamic", "x"); err == nil {
		t.Fatal("reserved overlay filename")
	}
	if _, err = WriteYAML(dir, "../etc", "x"); err == nil {
		t.Fatal("path escape")
	}
	if _, err = WriteYAML(dir, "Notes", "x"); err == nil {
		t.Fatal("uppercase name")
	}
}

func TestFileApplyReadyIgnoresProviderDisabledAndSameFileRouter(t *testing.T) {
	inv := traefikclient.Inventory{Routers: []traefikclient.Resource{{
		Name: "notes@file", Provider: "file", Status: "enabled", Rule: "Host(`notes.isolated.test`)",
	}}}
	if !FileApplyReady([]Issue{{Code: "provider_disabled"}}, "notes", "notes.isolated.test", traefikclient.Inventory{}) {
		t.Fatal("provider_disabled must not block file apply")
	}
	if !FileApplyReady([]Issue{{Code: "router_duplicate"}, {Code: "hostname_duplicate"}, {Code: "provider_disabled"}}, "notes", "notes.isolated.test", inv) {
		t.Fatal("updating the same file router must be ready")
	}
	if FileApplyReady([]Issue{{Code: "hostname_duplicate"}}, "notes", "notes.isolated.test", traefikclient.Inventory{Routers: []traefikclient.Resource{{
		Name: "other@file", Provider: "file", Rule: "Host(`notes.isolated.test`)",
	}}}) {
		t.Fatal("foreign hostname must block")
	}
	if FileApplyReady([]Issue{{Code: "port_missing"}}, "notes", "notes.isolated.test", inv) {
		t.Fatal("other blockers remain")
	}
}
