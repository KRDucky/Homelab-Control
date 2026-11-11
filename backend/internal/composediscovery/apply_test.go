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
		t.Fatal("port_missing")
	}
}

func TestLabelApplyReadyRequiresDockerProvider(t *testing.T) {
	on := true
	off := false
	inv := traefikclient.Inventory{Routers: []traefikclient.Resource{{
		Name: "notes@docker", Provider: "docker", Rule: "Host(`notes.isolated.test`)",
	}}}
	if LabelApplyReady(nil, "notes", "notes.isolated.test", inv, nil) {
		t.Fatal("nil provider")
	}
	if LabelApplyReady(nil, "notes", "notes.isolated.test", inv, &off) {
		t.Fatal("disabled")
	}
	if !LabelApplyReady(nil, "notes", "notes.isolated.test", inv, &on) {
		t.Fatal("enabled empty blockers")
	}
	if !LabelApplyReady([]Issue{{Code: "hostname_duplicate"}, {Code: "router_duplicate"}}, "notes", "notes.isolated.test", inv, &on) {
		t.Fatal("same docker router")
	}
	if LabelApplyReady([]Issue{{Code: "port_missing"}}, "notes", "notes.isolated.test", inv, &on) {
		t.Fatal("port_missing")
	}
	if LabelApplyReady([]Issue{{Code: "provider_disabled"}}, "notes", "notes.isolated.test", inv, &on) {
		t.Fatal("other blockers remain")
	}
}
