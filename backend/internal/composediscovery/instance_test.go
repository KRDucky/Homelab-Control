// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package composediscovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfficialTraefikImage(t *testing.T) {
	yes := []string{
		"traefik",
		"traefik:v3.3",
		"docker.io/library/traefik@sha256:f4bb143d1135a579b630ed491e17ec50f6f7c9c6a30585ff3d385471d2a86f5a",
		"library/traefik:latest",
		"ghcr.io/traefik/traefik:v3.6",
	}
	no := []string{
		"",
		"homecontrol/traefik-core:compose-apply",
		"homecontrol/traefik-plugin:file-preview",
		"homecontrol/traefik-browser:phase4-r1",
		"traefik/whoami",
		"nginx",
		"myorg/traefik-custom",
	}
	for _, image := range yes {
		if !OfficialTraefikImage(image) {
			t.Fatal(image)
		}
	}
	for _, image := range no {
		if OfficialTraefikImage(image) {
			t.Fatal(image)
		}
	}
}

func TestDetectInstancePrefersRunningOfficialTraefik(t *testing.T) {
	if got := DetectInstance(nil); got.Present {
		t.Fatal(got)
	}
	apps := []Service{
		{Container: "notes-web", Compose: true, Project: "hc-notes", Service: "notes", State: "running", Labels: map[string]string{"traefik.enable": "true"}},
		{Container: "hc-traefik-core-test", Compose: true, Project: "hc-traefik-live", Service: "core", State: "running"},
		{Container: "hc-traefik-fixture", Compose: true, Project: "hc-traefik-live", Service: "traefik", State: "running"},
	}
	got := DetectInstance(apps)
	if !got.Present || !got.Running || got.Container != "hc-traefik-fixture" || got.Service != "traefik" {
		t.Fatal(got)
	}
	imageOnly := []Service{{Container: "proxy", State: "exited", OfficialTraefik: true}}
	got = DetectInstance(imageOnly)
	if !got.Present || got.Running || got.Container != "proxy" {
		t.Fatal(got)
	}
}

func TestWriteOfficialSetupLeavesEightyFree(t *testing.T) {
	dir := t.TempDir()
	wrote, err := WriteOfficialSetup(dir)
	if err != nil || len(wrote) != 2 {
		t.Fatal(wrote, err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{OfficialImage, "8880:8080", "network_mode: bridge"} {
		if !strings.Contains(text, want) {
			t.Fatal(text)
		}
	}
	for _, ban := range []string{"'80:80'", "\"80:80\"", "'443:443'", "\"443:443\"", "'80:8080'", "\"80:8080\""} {
		if strings.Contains(text, ban) {
			t.Fatal("host 80/443 must stay free", text)
		}
	}
	static, err := os.ReadFile(filepath.Join(dir, "traefik.yml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(static)
	if !strings.Contains(cfg, "exposedByDefault: false") || !strings.Contains(cfg, "unix:///var/run/docker.sock") {
		t.Fatal(cfg)
	}
	info, err := os.Stat(filepath.Join(dir, "dynamic"))
	if err != nil || !info.IsDir() {
		t.Fatal(err)
	}
}

func TestWriteOfficialSetupRejectsMissingDir(t *testing.T) {
	if _, err := WriteOfficialSetup(filepath.Join(t.TempDir(), "missing")); err != ErrSetup {
		t.Fatal(err)
	}
}
