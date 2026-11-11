// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package composediscovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleCompose = `# SPDX-FileCopyrightText: 2026 Homelab Control Extensions contributors
name: hc-notes
services:
  notes:
    image: docker.io/library/node:22.23.3-bookworm
    labels:
      traefik.enable: 'true'
      traefik.docker.network: hc-traefik-test
    networks: [proxy]
networks:
  proxy:
    external: true
`

func TestFindComposeFileMatchesProjectAndService(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "compose.yaml")
	if err := os.WriteFile(path, []byte(sampleCompose), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := FindComposeFile(root, "hc-notes", "notes")
	if err != nil || got != path && !strings.HasSuffix(got, "compose.yaml") {
		t.Fatal(got, err)
	}
	if _, err = FindComposeFile(root, "hc-notes", "missing"); err != ErrComposeNotFound {
		t.Fatal(err)
	}
}

func TestPatchServiceLabelsMergesAndRejectsConflict(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(path, []byte(sampleCompose), 0644); err != nil {
		t.Fatal(err)
	}
	labels := map[string]string{
		"traefik.enable":                                             "true",
		"traefik.docker.network":                                     "hc-traefik-test",
		"traefik.http.routers.notes-gui.rule":                        "Host(`notes-gui.isolated.test`)",
		"traefik.http.routers.notes-gui.entrypoints":                 "web",
		"traefik.http.routers.notes-gui.service":                     "notes-gui",
		"traefik.http.services.notes-gui.loadbalancer.server.port":   "8080",
		"traefik.http.services.notes-gui.loadbalancer.server.scheme": "http",
	}
	n, err := PatchServiceLabels(path, "notes", labels)
	if err != nil || n != 5 {
		t.Fatal(n, err)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "Host(`notes-gui.isolated.test`)") || !strings.Contains(string(body), "image: docker.io/library/node") {
		t.Fatal(string(body))
	}
	n, err = PatchServiceLabels(path, "notes", labels)
	if err != nil || n != 0 {
		t.Fatal("idempotent", n, err)
	}
	labels["traefik.enable"] = "false"
	if _, err = PatchServiceLabels(path, "notes", labels); err != ErrComposeLabelConflict {
		t.Fatal(err)
	}
}

func TestPatchServiceLabelsInsertsMissingMap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yaml")
	src := "name: app\nservices:\n  web:\n    image: x\n"
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	n, err := PatchServiceLabels(path, "web", map[string]string{"traefik.enable": "true"})
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "labels:") || !strings.Contains(string(body), "traefik.enable") || !strings.Contains(string(body), "image: x") {
		t.Fatal(string(body))
	}
}

func TestPatchServiceLabelsRejectsListForm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yaml")
	src := "name: app\nservices:\n  web:\n    labels:\n      - traefik.enable=true\n"
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := PatchServiceLabels(path, "web", map[string]string{"traefik.enable": "true"}); err != ErrComposeLabelsForm {
		t.Fatal(err)
	}
}

func TestFindComposeFileRejectsEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "compose.yaml"), []byte(sampleCompose), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "compose.yaml"), filepath.Join(root, "compose.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := FindComposeFile(root, "hc-notes", "notes"); err != ErrComposeNotFound {
		t.Fatal(err)
	}
}
