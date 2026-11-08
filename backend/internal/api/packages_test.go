// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"net/http"
	"strings"
	"testing"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/broker/proto"
)

func packagesFake() *fakeBroker {
	fb := newFake()
	fb.results["packages.installed"] = map[string]any{
		"packages":  []map[string]any{{"name": "nginx", "version": "1.2.3", "arch": "amd64", "installed": true}},
		"truncated": false,
		"source":    "fixture",
	}
	fb.results["packages.updates"] = map[string]any{
		"updates":   []map[string]any{{"name": "nginx", "arch": "amd64", "current": "1.2.3", "new": "1.2.4"}},
		"truncated": false,
		"source":    "fixture",
		"as_of":     "2026-11-07T00:00:00Z",
	}
	fb.results["packages.sources"] = map[string]any{
		"sources":   []map[string]any{{"name": "ubuntu", "origin": "http://archive.ubuntu.com/ubuntu", "enabled": true, "signed": true, "third_party": false}},
		"truncated": false,
		"source":    "fixture",
	}
	fb.results["packages.search"] = map[string]any{
		"packages":  []map[string]any{{"name": "nginx", "summary": "web server", "installed": false}},
		"truncated": false,
		"source":    "fixture",
	}
	return fb
}

func packagesHeaders(h *harness) map[string]string {
	return map[string]string{"Origin": testOrigin, "X-CSRF-Token": h.csrf}
}

func TestPackagesRequiresSessionAndScope(t *testing.T) {
	h := newHarness(t)
	if rr := h.do("GET", "/api/v1/packages/installed", "", nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("anon installed: %d", rr.Code)
	}
	h.login("reader", "reader-password-123")
	if rr := h.do("GET", "/api/v1/packages/installed", "", packagesHeaders(h)); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "packages.read") {
		t.Fatalf("system.read installed: %d %s", rr.Code, rr.Body)
	}
}

func TestPackagesRequiresCSRFAndOrigin(t *testing.T) {
	h := harnessWith(t, packagesFake(), []string{auth.ScopePackagesRead})
	// Missing CSRF token is refused before any read.
	if rr := h.do("GET", "/api/v1/packages/installed", "", map[string]string{"Origin": testOrigin}); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "csrf_rejected") {
		t.Fatalf("missing csrf: %d %s", rr.Code, rr.Body)
	}
	// Cross-origin GET with a valid CSRF token is refused.
	if rr := h.do("GET", "/api/v1/packages/installed", "", map[string]string{"Origin": "http://evil.test", "X-CSRF-Token": h.csrf}); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "origin_rejected") {
		t.Fatalf("cross-origin: %d %s", rr.Code, rr.Body)
	}
}

func TestPackagesInstalledReadsAndAuditsCountsOnly(t *testing.T) {
	h := harnessWith(t, packagesFake(), []string{auth.ScopePackagesRead})
	rr := h.do("GET", "/api/v1/packages/installed", "", packagesHeaders(h))
	if rr.Code != http.StatusOK {
		t.Fatalf("installed: %d %s", rr.Code, rr.Body)
	}
	if !strings.Contains(rr.Body.String(), "nginx") || strings.Contains(rr.Body.String(), "1.2.3") == false {
		t.Fatalf("installed body: %s", rr.Body)
	}
	auditLine := h.audit.String()
	if !strings.Contains(auditLine, "packages.installed.read") || !strings.Contains(auditLine, "packages=1") {
		t.Fatalf("audit must record counts only: %s", auditLine)
	}
	if strings.Contains(auditLine, "nginx") {
		t.Fatalf("audit leaked a package name: %s", auditLine)
	}
}

func TestPackagesSearchAuditsQueryLengthOnly(t *testing.T) {
	h := harnessWith(t, packagesFake(), []string{auth.ScopePackagesRead})
	rr := h.do("GET", "/api/v1/packages/search?q=nginx", "", packagesHeaders(h))
	if rr.Code != http.StatusOK {
		t.Fatalf("search: %d %s", rr.Code, rr.Body)
	}
	auditLine := h.audit.String()
	if !strings.Contains(auditLine, "packages.search.read") || !strings.Contains(auditLine, "q_len=5") {
		t.Fatalf("audit must record query length only: %s", auditLine)
	}
}

func TestPackagesUnsupportedFamilyIsNotEmptySuccess(t *testing.T) {
	fb := packagesFake()
	fb.errs["packages.installed"] = &proto.Error{Code: proto.CodeUnavailable, Message: "unsupported package family: unknown"}
	h := harnessWith(t, fb, []string{auth.ScopePackagesRead})
	rr := h.do("GET", "/api/v1/packages/installed", "", packagesHeaders(h))
	if rr.Code != http.StatusServiceUnavailable || !strings.Contains(rr.Body.String(), "unsupported") {
		t.Fatalf("unsupported: %d %s", rr.Code, rr.Body)
	}
}
