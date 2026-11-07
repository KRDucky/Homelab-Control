// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/filesinfo"
)

func filesJail(t *testing.T) string {
	t.Helper()
	d := filepath.Join(t.TempDir(), "jail")
	if err := os.MkdirAll(filepath.Join(d, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "readme.txt"), []byte("secret-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "sub", "inner.txt"), []byte("inner"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/", filepath.Join(d, "escape")); err != nil {
		t.Fatal(err)
	}
	return d
}

func filesHarness(t *testing.T, scopes []string) (*harness, string) {
	t.Helper()
	h := harnessWith(t, nil, scopes)
	d := filesJail(t)
	set := filesinfo.LoadOrFailClosed(writeFilesJSON(t, d), false)
	if !set.BrowseAvailable() {
		t.Fatal(set.Reason)
	}
	h.srv.WithFiles(set)
	return h, d
}

func writeFilesJSON(t *testing.T, jail string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "files.json")
	body := `{"version":1,"roots":[{"id":"lab","label":"Lab files","path":"` + jail + `"}]}`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func filesHeaders(h *harness) map[string]string {
	return map[string]string{"Origin": testOrigin, "X-CSRF-Token": h.csrf}
}

func TestFilesRequiresSessionAndFilesRead(t *testing.T) {
	h := newHarness(t)
	if rr := h.do("GET", "/api/v1/files/roots", "", nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("anon roots: %d", rr.Code)
	}
	if rr := h.do("GET", "/api/v1/files/entries?root=lab&path=", "", nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("anon entries: %d", rr.Code)
	}
	if rr := h.login("reader", "reader-password-123"); rr.Code != 200 {
		t.Fatal(rr.Body)
	}
	if rr := h.do("GET", "/api/v1/files/roots", "", filesHeaders(h)); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "files.read") {
		t.Fatalf("system.read roots: %d %s", rr.Code, rr.Body)
	}
}

func TestFilesOtherScopesDoNotAuthorize(t *testing.T) {
	others := []string{
		auth.ScopeSystemRead, auth.ScopeServicesRead, auth.ScopeHardwareRead,
		auth.ScopeServicesFiles, auth.ScopeStorageRead, auth.ScopeLayoutManage,
		auth.ScopeHostSettingsPreview, auth.ScopeHostSettingsApply,
	}
	h, _ := filesHarness(t, others)
	for _, path := range []string{"/api/v1/files/roots", "/api/v1/files/entries?root=lab&path="} {
		if rr := h.do("GET", path, "", filesHeaders(h)); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "files.read") {
			t.Fatalf("%s: %d %s", path, rr.Code, rr.Body)
		}
	}
}

func TestFilesCSRFAndOrigin(t *testing.T) {
	h, _ := filesHarness(t, []string{auth.ScopeFilesRead})
	before := h.audit.Len()
	for name, hdr := range map[string]map[string]string{
		"cookie only":  nil,
		"wrong token":  {"Origin": testOrigin, "X-CSRF-Token": "nope"},
		"no origin":    {"X-CSRF-Token": h.csrf},
		"cross origin": {"Origin": "http://evil.test", "X-CSRF-Token": h.csrf},
	} {
		for _, path := range []string{"/api/v1/files/roots", "/api/v1/files/entries?root=lab&path=sub"} {
			rr := h.do("GET", path, "", hdr)
			if rr.Code != http.StatusForbidden {
				t.Fatalf("%s %s: %d %s", name, path, rr.Code, rr.Body)
			}
			if name == "cookie only" || name == "wrong token" {
				if !strings.Contains(rr.Body.String(), "csrf_rejected") {
					t.Fatalf("%s: %s", name, rr.Body)
				}
			} else if !strings.Contains(rr.Body.String(), "origin_rejected") {
				t.Fatalf("%s: %s", name, rr.Body)
			}
		}
	}
	if h.audit.Len() != before {
		t.Fatalf("refused reads were audited: %s", h.audit.String()[before:])
	}
}

func TestFilesRootsAndEntriesHappyPath(t *testing.T) {
	h, jail := filesHarness(t, []string{auth.ScopeFilesRead})
	rr := h.do("GET", "/api/v1/files/roots", "", filesHeaders(h))
	if rr.Code != http.StatusOK {
		t.Fatal(rr.Code, rr.Body)
	}
	var roots filesRootsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &roots); err != nil {
		t.Fatal(err)
	}
	if !roots.BrowseAvailable || len(roots.Roots) != 1 || roots.Roots[0].ID != "lab" || roots.Roots[0].Label != "Lab files" {
		t.Fatalf("%+v", roots)
	}
	if strings.Contains(rr.Body.String(), jail) {
		t.Fatal("roots leaked host path")
	}
	rr = h.do("GET", "/api/v1/files/entries?root=lab&path=", "", filesHeaders(h))
	if rr.Code != http.StatusOK {
		t.Fatal(rr.Code, rr.Body)
	}
	var ents filesEntriesResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &ents); err != nil {
		t.Fatal(err)
	}
	if ents.Root != "lab" || ents.Path != "." {
		t.Fatalf("%+v", ents)
	}
	names := map[string]filesinfo.Entry{}
	for _, e := range ents.Entries {
		names[e.Name] = e
	}
	if !names["sub"].Dir || names["readme.txt"].Dir || names["escape"].Dir {
		t.Fatalf("%+v", names)
	}
	body := rr.Body.String()
	if strings.Contains(body, "secret-bytes") || strings.Contains(body, jail) {
		t.Fatalf("listing leaked contents or jail path: %s", body)
	}
	a := h.audit.String()
	if !strings.Contains(a, `"event":"files.entries.read"`) || !strings.Contains(a, "root=lab") || strings.Contains(a, "secret-bytes") || strings.Contains(a, jail) {
		t.Fatalf("audit: %s", a)
	}
}

func TestFilesJailEscapeAPI(t *testing.T) {
	h, jail := filesHarness(t, []string{auth.ScopeFilesRead})
	hdr := filesHeaders(h)
	before := h.audit.Len()
	if rr := h.do("GET", "/api/v1/files/entries?root=lab&path=..", "", hdr); rr.Code != http.StatusForbidden {
		t.Fatalf(".. : %d %s", rr.Code, rr.Body)
	}
	if rr := h.do("GET", "/api/v1/files/entries?root=lab&path=sub//inner.txt", "", hdr); rr.Code != http.StatusBadRequest {
		t.Fatalf("slashes: %d %s", rr.Code, rr.Body)
	}
	if rr := h.do("GET", "/api/v1/files/entries?root=lab&path=escape", "", hdr); rr.Code != http.StatusBadRequest {
		t.Fatalf("symlink out: %d %s", rr.Code, rr.Body)
	}
	if rr := h.do("GET", "/api/v1/files/entries?root=nope&path=", "", hdr); rr.Code != http.StatusNotFound {
		t.Fatalf("bad root: %d %s", rr.Code, rr.Body)
	}
	if rr := h.do("GET", "/api/v1/files/entries?root=lab&path=readme.txt", "", hdr); rr.Code != http.StatusBadRequest {
		t.Fatalf("file: %d %s", rr.Code, rr.Body)
	}
	if h.audit.Len() != before {
		t.Fatalf("jail-escape was audited: %s", h.audit.String()[before:])
	}
	if strings.Contains(h.audit.String(), jail) {
		t.Fatalf("audit leaked jail path: %s", h.audit.String())
	}
}

func TestFilesMissingConfig(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeFilesRead})
	rr := h.do("GET", "/api/v1/files/roots", "", filesHeaders(h))
	if rr.Code != http.StatusOK {
		t.Fatal(rr.Code, rr.Body)
	}
	var roots filesRootsResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &roots)
	if roots.BrowseAvailable || len(roots.Roots) != 0 {
		t.Fatalf("%+v", roots)
	}
	if rr := h.do("GET", "/api/v1/files/entries?root=lab&path=", "", filesHeaders(h)); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("entries: %d %s", rr.Code, rr.Body)
	}
}

func TestFilesCapabilityDegradedWhenBrowsable(t *testing.T) {
	h, _ := filesHarness(t, []string{auth.ScopeSystemRead, auth.ScopeFilesRead})
	c := caps(t, h)["files"]
	if c.Status != CapDegraded || c.Path != "/files" || c.Scope != auth.ScopeFilesRead {
		t.Fatalf("%+v", c)
	}
	h2 := harnessWith(t, nil, []string{auth.ScopeSystemRead})
	c2 := caps(t, h2)["files"]
	if c2.Status != CapForbidden {
		t.Fatalf("%+v", c2)
	}
}
