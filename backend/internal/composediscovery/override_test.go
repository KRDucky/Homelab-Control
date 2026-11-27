// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package composediscovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func composeDir(t *testing.T, name, body string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return dir, path
}

func TestWriteLabelsOverrideLeavesTheComposeFileUnchanged(t *testing.T) {
	dir, path := composeDir(t, "compose.yaml", sampleCompose)
	wrote, n, err := WriteLabelsOverride(path, "notes", notesLabels())
	if err != nil || wrote != "compose.override.yaml" || n != 7 {
		t.Fatal(wrote, n, err)
	}
	if b, _ := os.ReadFile(path); string(b) != sampleCompose {
		t.Fatalf("compose file changed:\n%s", b)
	}
	b, err := os.ReadFile(filepath.Join(dir, wrote))
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	for _, want := range []string{overrideHeader, "services:\n  notes:\n    labels:\n",
		"      traefik.http.routers.notes-gui.rule: 'Host(`notes-gui.isolated.test`)'\n",
		"      traefik.enable: 'true'\n"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in\n%s", want, body)
		}
	}
	// Same labels again: nothing new, file unchanged.
	if _, n, err = WriteLabelsOverride(path, "notes", notesLabels()); err != nil || n != 0 {
		t.Fatal("idempotent", n, err)
	}
	if b2, _ := os.ReadFile(filepath.Join(dir, wrote)); string(b2) != body {
		t.Fatal("rewritten without a change")
	}
}

func TestWriteLabelsOverrideKeepsOtherServicesAndUpdatesItsOwn(t *testing.T) {
	src := "name: app\nservices:\n  web:\n    image: x\n  api:\n    image: y\n"
	dir, path := composeDir(t, "docker-compose.yml", src)
	if _, _, err := WriteLabelsOverride(path, "web", map[string]string{"traefik.enable": "true"}); err != nil {
		t.Fatal(err)
	}
	wrote, n, err := WriteLabelsOverride(path, "api", map[string]string{"traefik.enable": "true", "traefik.http.services.api.loadbalancer.server.port": "9000"})
	if err != nil || wrote != "docker-compose.override.yml" || n != 2 {
		t.Fatal(wrote, n, err)
	}
	if _, n, err = WriteLabelsOverride(path, "api", map[string]string{"traefik.http.services.api.loadbalancer.server.port": "9001"}); err != nil || n != 1 {
		t.Fatal("update own label", n, err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, wrote))
	body := string(b)
	if !strings.Contains(body, "  web:\n") || !strings.Contains(body, "port: '9001'") || strings.Contains(body, "'9000'") {
		t.Fatal(body)
	}
}

func TestWriteLabelsOverrideRefusesWhatItDoesNotOwn(t *testing.T) {
	// A hand-written override that Compose would load first.
	dir, path := composeDir(t, "compose.yaml", sampleCompose)
	mine := "services:\n  notes:\n    environment: [A=1]\n"
	if err := os.WriteFile(filepath.Join(dir, "compose.override.yml"), []byte(mine), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := WriteLabelsOverride(path, "notes", notesLabels()); err != ErrComposeOverrideUnmanaged {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "compose.override.yml")); string(b) != mine {
		t.Fatal("unmanaged override changed")
	}
	if _, err := os.Stat(filepath.Join(dir, "compose.override.yaml")); err == nil {
		t.Fatal("wrote a second override that Compose would not load")
	}

	// A state line that would inject YAML is not ours either.
	dir2, path2 := composeDir(t, "compose.yaml", sampleCompose)
	bad := overrideHeader + overrideState + `{"notes":{"traefik.enable\n  evil: x":"true"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir2, "compose.override.yaml"), []byte(bad), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := WriteLabelsOverride(path2, "notes", notesLabels()); err != ErrComposeOverrideUnmanaged {
		t.Fatal(err)
	}

	// A symlink in the override's place is refused.
	dir3, path3 := composeDir(t, "compose.yaml", sampleCompose)
	if err := os.Symlink(filepath.Join(dir3, "compose.yaml"), filepath.Join(dir3, "compose.override.yml")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := WriteLabelsOverride(path3, "notes", notesLabels()); err != ErrComposeOverrideUnmanaged {
		t.Fatal(err)
	}
}

func TestWriteLabelsOverrideKeepsTheComposeFileConflictCheck(t *testing.T) {
	_, path := composeDir(t, "compose.yaml", sampleCompose)
	labels := notesLabels()
	labels["traefik.docker.network"] = "other"
	if _, _, err := WriteLabelsOverride(path, "notes", labels); err != ErrComposeLabelConflict {
		t.Fatal(err)
	}
}
