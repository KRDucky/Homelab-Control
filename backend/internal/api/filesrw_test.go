// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/filesinfo"
)

func filesRWHarness(t *testing.T, scopes ...string) (*harness, string) {
	t.Helper()
	all := append([]string{auth.ScopeFilesRead}, scopes...)
	h, d := filesHarness(t, all)
	return h, d
}

func stepUpFull(h *harness) {
	h.t.Helper()
	b, _ := json.Marshal(map[string]string{"password": "full-password-1234"})
	rr := h.do("POST", "/api/v1/auth/step-up", string(b), map[string]string{
		"Content-Type": "application/json", "Origin": testOrigin, "X-CSRF-Token": h.csrf,
	})
	if rr.Code != http.StatusOK {
		h.t.Fatalf("step-up failed: %d %s", rr.Code, rr.Body)
	}
}

func deletePlan(t *testing.T, h *harness, root, path string) filesDeletePlanResponse {
	t.Helper()
	rr := h.do("POST", "/api/v1/files/delete/plan?root="+root+"&path="+path, "", filesHeaders(h))
	if rr.Code != http.StatusOK {
		t.Fatalf("plan %s/%s: %d %s", root, path, rr.Code, rr.Body)
	}
	var p filesDeletePlanResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Token == "" {
		t.Fatal("empty plan token")
	}
	return p
}

func deleteApply(t *testing.T, h *harness, token string) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"token": token})
	return h.do("POST", "/api/v1/files/delete/apply", string(b), map[string]string{
		"Content-Type": "application/json", "Origin": testOrigin, "X-CSRF-Token": h.csrf,
	})
}

func TestFilesRWScopeDenial(t *testing.T) {
	// files.read alone must not authorize write, upload, create, rename or delete.
	h, _ := filesHarness(t, []string{auth.ScopeFilesRead})
	writeHdrs := map[string]string{"Origin": testOrigin, "X-CSRF-Token": h.csrf, "Content-Type": "application/json"}
	cases := []struct {
		method, path, body string
		hdr                map[string]string
		wantScope          string
	}{
		{"PUT", "/api/v1/files/content?root=lab&path=readme.txt", "x", writeHdrs, "files.write"},
		{"POST", "/api/v1/files/upload?root=lab&path=", "x", map[string]string{"Origin": testOrigin, "X-CSRF-Token": h.csrf, "Content-Type": "multipart/form-data; boundary=B"}, "files.write"},
		{"POST", "/api/v1/files/entries?root=lab&path=", `{"name":"n","type":"file"}`, writeHdrs, "files.write"},
		{"PUT", "/api/v1/files/entries?root=lab&path=readme.txt", `{"to":"x.txt"}`, writeHdrs, "files.write"},
		{"POST", "/api/v1/files/delete/plan?root=lab&path=readme.txt", "", filesHeaders(h), "files.destroy"},
		{"POST", "/api/v1/files/delete/apply", `{"token":"hcfp1.a.b"}`, writeHdrs, "files.destroy"},
	}
	for _, c := range cases {
		rr := h.do(c.method, c.path, c.body, c.hdr)
		if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), c.wantScope) {
			t.Fatalf("%s %s: %d %s (want scope %s)", c.method, c.path, rr.Code, rr.Body, c.wantScope)
		}
	}
	// files.write must not imply files.destroy.
	h2, _ := filesHarness(t, []string{auth.ScopeFilesRead, auth.ScopeFilesWrite})
	if rr := h2.do("POST", "/api/v1/files/delete/plan?root=lab&path=readme.txt", "", filesHeaders(h2)); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "files.destroy") {
		t.Fatalf("files.write implied files.destroy: %d %s", rr.Code, rr.Body)
	}
}

func TestFilesRWCSRF(t *testing.T) {
	h, _ := filesRWHarness(t, auth.ScopeFilesWrite, auth.ScopeFilesDestroy)
	for name, hdr := range map[string]map[string]string{
		"cookie only": nil,
		"wrong token": {"Origin": testOrigin, "X-CSRF-Token": "nope"},
	} {
		for _, path := range []string{"/api/v1/files/content?root=lab&path=readme.txt", "/api/v1/files/download?root=lab&path=readme.txt"} {
			rr := h.do("GET", path, "", hdr)
			if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "csrf_rejected") {
				t.Fatalf("%s %s: %d %s", name, path, rr.Code, rr.Body)
			}
		}
		rr := h.do("PUT", "/api/v1/files/content?root=lab&path=readme.txt", "x", hdr)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("%s PUT: %d %s", name, rr.Code, rr.Body)
		}
	}
}

func TestFilesContentReadDownload(t *testing.T) {
	h, jail := filesRWHarness(t)
	rr := h.do("GET", "/api/v1/files/content?root=lab&path=readme.txt", "", filesHeaders(h))
	if rr.Code != http.StatusOK || rr.Body.String() != "secret-bytes" {
		t.Fatalf("content: %d %q", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("X-File-Encoding") != "utf8" || rr.Header().Get("X-Content-Size") != "12" {
		t.Fatalf("content headers: %+v", rr.Header())
	}
	rr = h.do("GET", "/api/v1/files/download?root=lab&path=sub/inner.txt", "", filesHeaders(h))
	if rr.Code != http.StatusOK || rr.Body.String() != "inner" {
		t.Fatalf("download: %d %q", rr.Code, rr.Body.String())
	}
	cd := rr.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "filename=inner.txt") || strings.Contains(cd, jail) || strings.Contains(cd, "sub/") {
		t.Fatalf("Content-Disposition leaked a path: %q", cd)
	}
	// Binary (invalid UTF-8) content is flagged binary.
	if err := os.WriteFile(filepath.Join(jail, "bin.dat"), []byte{0xff, 0xfe, 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}
	rr = h.do("GET", "/api/v1/files/content?root=lab&path=bin.dat", "", filesHeaders(h))
	if rr.Code != http.StatusOK || rr.Header().Get("X-File-Encoding") != "binary" {
		t.Fatalf("binary encoding: %d %+v", rr.Code, rr.Header())
	}
}

func TestFilesContentWriteReplace(t *testing.T) {
	h, jail := filesRWHarness(t, auth.ScopeFilesWrite)
	hdr := map[string]string{"Origin": testOrigin, "X-CSRF-Token": h.csrf, "Content-Type": "application/octet-stream"}
	rr := h.do("PUT", "/api/v1/files/content?root=lab&path=readme.txt", "replaced", hdr)
	if rr.Code != http.StatusOK {
		t.Fatalf("write: %d %s", rr.Code, rr.Body)
	}
	if got, _ := os.ReadFile(filepath.Join(jail, "readme.txt")); string(got) != "replaced" {
		t.Fatalf("file = %q", got)
	}
	// Symlink, directory and missing targets are refused without following.
	if rr := h.do("PUT", "/api/v1/files/content?root=lab&path=escape", "x", hdr); rr.Code != http.StatusBadRequest {
		t.Fatalf("symlink write: %d %s", rr.Code, rr.Body)
	}
	if rr := h.do("PUT", "/api/v1/files/content?root=lab&path=sub", "x", hdr); rr.Code != http.StatusBadRequest {
		t.Fatalf("dir write: %d %s", rr.Code, rr.Body)
	}
	if rr := h.do("PUT", "/api/v1/files/content?root=lab&path=missing", "x", hdr); rr.Code != http.StatusNotFound {
		t.Fatalf("missing write: %d %s", rr.Code, rr.Body)
	}
}

func TestFilesCreateAndRename(t *testing.T) {
	h, jail := filesRWHarness(t, auth.ScopeFilesWrite)
	hdr := map[string]string{"Origin": testOrigin, "X-CSRF-Token": h.csrf, "Content-Type": "application/json"}
	rr := h.do("POST", "/api/v1/files/entries?root=lab&path=", `{"name":"new.txt","type":"file"}`, hdr)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create file: %d %s", rr.Code, rr.Body)
	}
	rr = h.do("POST", "/api/v1/files/entries?root=lab&path=", `{"name":"newdir","type":"dir"}`, hdr)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create dir: %d %s", rr.Code, rr.Body)
	}
	if _, err := os.Stat(filepath.Join(jail, "new.txt")); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(jail, "newdir")); err != nil || !fi.IsDir() {
		t.Fatalf("newdir: %v %v", fi, err)
	}
	// Duplicate create → 409.
	if rr := h.do("POST", "/api/v1/files/entries?root=lab&path=", `{"name":"new.txt","type":"file"}`, hdr); rr.Code != http.StatusConflict {
		t.Fatalf("duplicate create: %d %s", rr.Code, rr.Body)
	}
	// Rename within the same root.
	rr = h.do("PUT", "/api/v1/files/entries?root=lab&path=new.txt", `{"to":"renamed.txt"}`, hdr)
	if rr.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", rr.Code, rr.Body)
	}
	if _, err := os.Stat(filepath.Join(jail, "renamed.txt")); err != nil {
		t.Fatal(err)
	}
	// Rename out of the jail is refused.
	if rr := h.do("PUT", "/api/v1/files/entries?root=lab&path=renamed.txt", `{"to":"../escape"}`, hdr); rr.Code != http.StatusForbidden {
		t.Fatalf("rename escape: %d %s", rr.Code, rr.Body)
	}
}

func TestFilesUpload(t *testing.T) {
	h, jail := filesRWHarness(t, auth.ScopeFilesWrite)
	boundary := "BOUNDARY123"
	body := "--" + boundary + "\r\n" +
		"Content-Disposition: form-data; name=\"file\"; filename=\"up.txt\"\r\n" +
		"Content-Type: application/octet-stream\r\n\r\n" +
		"hello-upload\r\n" +
		"--" + boundary + "--\r\n"
	hdr := map[string]string{"Origin": testOrigin, "X-CSRF-Token": h.csrf, "Content-Type": "multipart/form-data; boundary=" + boundary}
	rr := h.do("POST", "/api/v1/files/upload?root=lab&path=", body, hdr)
	if rr.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rr.Code, rr.Body)
	}
	if got, _ := os.ReadFile(filepath.Join(jail, "up.txt")); string(got) != "hello-upload" {
		t.Fatalf("uploaded = %q", got)
	}
	// Backslash filename is a path escape on some clients and is refused.
	bad := "--" + boundary + "\r\n" +
		"Content-Disposition: form-data; name=\"file\"; filename=\"..\\evil.txt\"\r\n\r\n" +
		"x\r\n" +
		"--" + boundary + "--\r\n"
	if rr := h.do("POST", "/api/v1/files/upload?root=lab&path=", bad, hdr); rr.Code != http.StatusBadRequest {
		t.Fatalf("backslash upload: %d %s", rr.Code, rr.Body)
	}
}

func TestFilesDeletePlanApply(t *testing.T) {
	h, jail := filesRWHarness(t, auth.ScopeFilesDestroy)
	p := deletePlan(t, h, "lab", "readme.txt")
	if p.Kind != "file" || p.Size != 12 || p.Path != "readme.txt" {
		t.Fatalf("plan: %+v", p)
	}
	// Missing step-up does not consume the token.
	if rr := deleteApply(t, h, p.Token); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "step_up_required") {
		t.Fatalf("apply without step-up: %d %s", rr.Code, rr.Body)
	}
	stepUpFull(h)
	rr := deleteApply(t, h, p.Token)
	if rr.Code != http.StatusOK {
		t.Fatalf("apply: %d %s", rr.Code, rr.Body)
	}
	if _, err := os.Lstat(filepath.Join(jail, "readme.txt")); !os.IsNotExist(err) {
		t.Fatalf("file still exists: %v", err)
	}
	// Replay is refused (single use).
	if rr := deleteApply(t, h, p.Token); rr.Code != http.StatusForbidden {
		t.Fatalf("replay: %d %s", rr.Code, rr.Body)
	}
}

func TestFilesDeleteConflictAndNotEmpty(t *testing.T) {
	h, _ := filesRWHarness(t, auth.ScopeFilesDestroy, auth.ScopeFilesWrite)
	stepUpFull(h)
	// Plan, then change the target, then apply → conflict, nothing deleted.
	p := deletePlan(t, h, "lab", "readme.txt")
	hdr := map[string]string{"Origin": testOrigin, "X-CSRF-Token": h.csrf, "Content-Type": "application/octet-stream"}
	if rr := h.do("PUT", "/api/v1/files/content?root=lab&path=readme.txt", "changed-bytes", hdr); rr.Code != http.StatusOK {
		t.Fatalf("mutate: %d %s", rr.Code, rr.Body)
	}
	if rr := deleteApply(t, h, p.Token); rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "delete_conflict") {
		t.Fatalf("conflict: %d %s", rr.Code, rr.Body)
	}
	// Non-empty directory is refused.
	p2 := deletePlan(t, h, "lab", "sub")
	if rr := deleteApply(t, h, p2.Token); rr.Code != http.StatusConflict {
		t.Fatalf("non-empty dir: %d %s", rr.Code, rr.Body)
	}
}

func TestFilesOversizeContentWriteAndReadTruncation(t *testing.T) {
	h, jail := filesRWHarness(t, auth.ScopeFilesWrite)
	hdr := map[string]string{"Origin": testOrigin, "X-CSRF-Token": h.csrf, "Content-Type": "application/octet-stream"}
	big := strings.Repeat("a", filesinfo.MaxContentBytes+1)
	if rr := h.do("PUT", "/api/v1/files/content?root=lab&path=readme.txt", big, hdr); rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize write: %d", rr.Code)
	}
	// Oversize read is capped and flagged truncated.
	large := strings.Repeat("b", filesinfo.MaxContentBytes+100)
	if err := os.WriteFile(filepath.Join(jail, "big.txt"), []byte(large), 0o644); err != nil {
		t.Fatal(err)
	}
	rr := h.do("GET", "/api/v1/files/content?root=lab&path=big.txt", "", filesHeaders(h))
	if rr.Code != http.StatusOK || rr.Header().Get("X-Content-Truncated") != "true" || rr.Body.Len() != filesinfo.MaxContentBytes {
		t.Fatalf("truncated read: %d len=%d trunc=%s", rr.Code, rr.Body.Len(), rr.Header().Get("X-Content-Truncated"))
	}
}

func TestFilesJailEscapeEveryVerb(t *testing.T) {
	h, jail := filesRWHarness(t, auth.ScopeFilesWrite, auth.ScopeFilesDestroy)
	hdrJSON := map[string]string{"Origin": testOrigin, "X-CSRF-Token": h.csrf, "Content-Type": "application/json"}
	hdrOctet := map[string]string{"Origin": testOrigin, "X-CSRF-Token": h.csrf, "Content-Type": "application/octet-stream"}
	cases := []struct {
		method, path, body string
		hdr                map[string]string
		want               int
	}{
		{"GET", "/api/v1/files/content?root=lab&path=..", "", filesHeaders(h), http.StatusForbidden},
		{"GET", "/api/v1/files/download?root=lab&path=..", "", filesHeaders(h), http.StatusForbidden},
		{"GET", "/api/v1/files/content?root=lab&path=sub//inner.txt", "", filesHeaders(h), http.StatusBadRequest},
		{"PUT", "/api/v1/files/content?root=lab&path=..", "x", hdrOctet, http.StatusForbidden},
		{"PUT", "/api/v1/files/content?root=lab&path=sub//inner.txt", "x", hdrOctet, http.StatusBadRequest},
		{"POST", "/api/v1/files/entries?root=lab&path=", `{"name":"../evil","type":"file"}`, hdrJSON, http.StatusBadRequest},
		{"PUT", "/api/v1/files/entries?root=lab&path=readme.txt", `{"to":"../../evil"}`, hdrJSON, http.StatusForbidden},
		{"POST", "/api/v1/files/delete/plan?root=lab&path=..", "", filesHeaders(h), http.StatusForbidden},
	}
	for _, c := range cases {
		rr := h.do(c.method, c.path, c.body, c.hdr)
		if rr.Code != c.want {
			t.Fatalf("%s %s: %d (want %d) %s", c.method, c.path, rr.Code, c.want, rr.Body)
		}
	}
	// A symlink target is never followed for read or write.
	if rr := h.do("GET", "/api/v1/files/content?root=lab&path=escape", "", filesHeaders(h)); rr.Code != http.StatusBadRequest {
		t.Fatalf("symlink read: %d %s", rr.Code, rr.Body)
	}
	if rr := h.do("PUT", "/api/v1/files/content?root=lab&path=escape", "x", hdrOctet); rr.Code != http.StatusBadRequest {
		t.Fatalf("symlink write: %d %s", rr.Code, rr.Body)
	}
	// The host jail path never appears anywhere.
	if strings.Contains(h.audit.String(), jail) {
		t.Fatalf("audit leaked jail path: %s", h.audit.String())
	}
}

func TestFilesRWAuditNeverLeaksContentOrHostPath(t *testing.T) {
	h, jail := filesRWHarness(t, auth.ScopeFilesWrite, auth.ScopeFilesDestroy)
	hdrOctet := map[string]string{"Origin": testOrigin, "X-CSRF-Token": h.csrf, "Content-Type": "application/octet-stream"}
	_ = h.do("PUT", "/api/v1/files/content?root=lab&path=readme.txt", "super-secret", hdrOctet)
	_ = h.do("GET", "/api/v1/files/content?root=lab&path=readme.txt", "", filesHeaders(h))
	a := h.audit.String()
	if strings.Contains(a, "super-secret") || strings.Contains(a, "secret-bytes") || strings.Contains(a, jail) {
		t.Fatalf("audit leaked content or host path: %s", a)
	}
	if !strings.Contains(a, "files.content.write") || !strings.Contains(a, "files.content.read") || !strings.Contains(a, "root=lab") {
		t.Fatalf("audit missing events: %s", a)
	}
}

func TestFilesUploadOversize(t *testing.T) {
	h, _ := filesRWHarness(t, auth.ScopeFilesWrite)
	boundary := "BOUNDARY123"
	big := strings.Repeat("z", filesinfo.MaxUploadBytes+1)
	body := "--" + boundary + "\r\n" +
		"Content-Disposition: form-data; name=\"file\"; filename=\"big.bin\"\r\n\r\n" +
		big + "\r\n" +
		"--" + boundary + "--\r\n"
	hdr := map[string]string{"Origin": testOrigin, "X-CSRF-Token": h.csrf, "Content-Type": "multipart/form-data; boundary=" + boundary}
	if rr := h.do("POST", "/api/v1/files/upload?root=lab&path=", body, hdr); rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize upload: %d %s", rr.Code, rr.Body)
	}
}
