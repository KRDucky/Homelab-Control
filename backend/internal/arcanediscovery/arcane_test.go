// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package arcanediscovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"homelab-control/backend/internal/composediscovery"
)

func TestOfficialArcaneImage(t *testing.T) {
	for _, good := range []string{"arcane", "arcane:latest", "getarcaneapp/arcane", "getarcaneapp/arcane:2.17.3", "getarcaneapp/manager", "ghcr.io/getarcaneapp/arcane:latest", "docker.io/library/arcane@sha256:abc"} {
		if !OfficialArcaneImage(good) {
			t.Errorf("should detect %q", good)
		}
	}
	for _, bad := range []string{"", "traefik", "arcade", "getarcaneapp/other", "notarcane", "whoami"} {
		if OfficialArcaneImage(bad) {
			t.Errorf("should not detect %q", bad)
		}
	}
}

func TestDetectFromSnapshot(t *testing.T) {
	snap := composediscovery.Snapshot{Services: []composediscovery.Service{
		{ID: "a", Container: "traefik", State: "running", Image: "traefik:latest"},
		{ID: "b", Container: "arcane", State: "running", Image: "ghcr.io/getarcaneapp/arcane:latest", PublishedPorts: []int{3000, 3552}},
	}}
	d := Detect(snap)
	if !d.Present || d.Container != "arcane" || d.URL != "http://127.0.0.1:3552" {
		t.Fatalf("detect: %+v", d)
	}
	if Detect(composediscovery.Snapshot{Services: snap.Services[:1]}).Present {
		t.Fatal("traefik-only snapshot detected as Arcane")
	}
}

func TestNewRejectsInvalidURL(t *testing.T) {
	if _, err := New("", "k", "0"); err == nil {
		t.Fatal("empty URL accepted")
	}
	if _, err := New("ftp://x", "k", "0"); err == nil {
		t.Fatal("non-http URL accepted")
	}
	// The key is optional (managers without auth, e.g. LAN-only Dockhand).
	if _, err := New("http://192.168.1.44:3552", "", "0"); err != nil {
		t.Fatalf("empty key should be allowed: %v", err)
	}
}

// fakeArcane serves the slice-1 endpoints and records the last PUT body.
type fakeArcane struct {
	mu         sync.Mutex
	compose    string
	putBody    map[string]string
	putCalls   int
	unauth     bool
	projectIDs []string
}

func (f *fakeArcane) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Api-Key") != "test-key" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/environments/0/projects":
		data := make([]map[string]string, 0, len(f.projectIDs))
		for _, id := range f.projectIDs {
			data = append(data, map[string]string{"id": id, "name": "example-notes", "path": "/x", "relativePath": "x"})
		}
		writeEnv(w, data)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/environments/0/projects/"):
		writeEnv(w, map[string]any{
			"id": "p1", "name": "example-notes", "composeFileName": "compose.yaml", "composeContent": f.compose,
		})
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/environments/0/projects/"):
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.putCalls++
		f.putBody = body
		f.mu.Unlock()
		writeEnv(w, map[string]any{"id": "p1", "name": "example-notes", "composeFileName": "compose.yaml", "composeContent": body["composeContent"]})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func writeEnv(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
}

func TestApplyLabelsPatchesAndPuts(t *testing.T) {
	f := &fakeArcane{compose: "name: example-notes\nservices:\n  web:\n    image: x\n", projectIDs: []string{"p1"}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	c, err := New(srv.URL, "test-key", "0")
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.ApplyLabels(context.Background(), "example-notes", "web", map[string]string{"traefik.enable": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 1 || res.Wrote != "compose.yaml" {
		t.Fatalf("result: %+v", res)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.putCalls != 1 || f.putBody == nil || !strings.Contains(f.putBody["composeContent"], "traefik.enable: 'true'") {
		t.Fatalf("put body: %+v calls=%d", f.putBody, f.putCalls)
	}
}

func TestApplyLabelsFallsThroughOnMissingProject(t *testing.T) {
	f := &fakeArcane{compose: "name: other\nservices:\n  api:\n    image: y\n", projectIDs: []string{"p1"}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	c, _ := New(srv.URL, "test-key", "0")
	if _, err := c.ApplyLabels(context.Background(), "example-notes", "web", map[string]string{"traefik.enable": "true"}); err != ErrProjectNotFound {
		t.Fatalf("want ErrProjectNotFound, got %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.putCalls != 0 {
		t.Fatal("no PUT expected for a non-matching project")
	}
}

func TestApplyLabelsUnauthorizedDoesNotPanic(t *testing.T) {
	f := &fakeArcane{compose: "name: example-notes\nservices:\n  web:\n    image: x\n", projectIDs: []string{"p1"}, unauth: true}
	srv := httptest.NewServer(f)
	defer srv.Close()
	c, _ := New(srv.URL, "wrong-key", "0")
	if _, err := c.ApplyLabels(context.Background(), "example-notes", "web", map[string]string{"traefik.enable": "true"}); err == nil {
		t.Fatal("expected error on unauthorized")
	}
}

func TestApplyLabelsConflictIsReturned(t *testing.T) {
	// The compose already has a conflicting traefik.enable value.
	f := &fakeArcane{compose: "name: example-notes\nservices:\n  web:\n    image: x\n    labels:\n      traefik.enable: \"false\"\n", projectIDs: []string{"p1"}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	c, _ := New(srv.URL, "test-key", "0")
	_, err := c.ApplyLabels(context.Background(), "example-notes", "web", map[string]string{"traefik.enable": "true"})
	if err != composediscovery.ErrComposeLabelConflict {
		t.Fatalf("want ErrComposeLabelConflict, got %v", err)
	}
}
