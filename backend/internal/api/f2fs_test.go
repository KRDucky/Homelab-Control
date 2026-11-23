// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/f2fsinfo"
	"homelab-control/backend/internal/modules"
)

func f2fsFake() func(context.Context) (f2fsinfo.Snapshot, error) {
	return func(context.Context) (f2fsinfo.Snapshot, error) {
		return f2fsinfo.Snapshot{
			Tooling: f2fsinfo.Tooling{
				Family: "debian",
				Required: []f2fsinfo.ToolItem{
					{ID: "dump.f2fs", Package: "f2fs-tools", Installed: true, Binary: "/usr/sbin/dump.f2fs"},
				},
				Optional: []f2fsinfo.ToolItem{},
			},
			F2fs: f2fsinfo.Inventory{Present: true, Mounts: []f2fsinfo.Mount{
				{Mountpoint: "/mnt/data", Device: "/dev/sda1"},
			}},
			KernelF2fs: f2fsinfo.KernelF2fs{Present: true},
		}, nil
	}
}

func TestF2fsRequiresSessionAndScope(t *testing.T) {
	h := newHarness(t)
	if rr := h.do("GET", "/api/v1/f2fs", "", nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("anon: %d %s", rr.Code, rr.Body)
	}
	h.login("reader", "reader-password-123")
	if rr := h.do("GET", "/api/v1/f2fs", "", packagesHeaders(h)); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "f2fs.read") {
		t.Fatalf("wrong scope: %d %s", rr.Code, rr.Body)
	}
}

func TestF2fsRequiresCSRFAndOrigin(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeF2fsRead})
	if rr := h.do("GET", "/api/v1/f2fs", "", map[string]string{"Origin": testOrigin}); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "csrf_rejected") {
		t.Fatalf("csrf: %d %s", rr.Code, rr.Body)
	}
	if rr := h.do("GET", "/api/v1/f2fs", "", map[string]string{"Origin": "http://evil.test", "X-CSRF-Token": h.csrf}); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "origin_rejected") {
		t.Fatalf("origin: %d %s", rr.Code, rr.Body)
	}
}

func TestF2fsReadsAndAuditsCountsOnly(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeF2fsRead})
	h.srv.f2fsCollect = f2fsFake()
	rr := h.do("GET", "/api/v1/f2fs", "", packagesHeaders(h))
	if rr.Code != http.StatusOK {
		t.Fatalf("code %d %s", rr.Code, rr.Body)
	}
	if !strings.Contains(rr.Body.String(), `"f2fs-tools"`) || !strings.Contains(rr.Body.String(), `"/dev/sda1"`) {
		t.Fatalf("body: %s", rr.Body)
	}
	auditLine := h.audit.String()
	if !strings.Contains(auditLine, "f2fs.inventory.read") || !strings.Contains(auditLine, "mounts=1") {
		t.Fatalf("audit must record counts only: %s", auditLine)
	}
	if strings.Contains(auditLine, "/dev/sda1") || strings.Contains(auditLine, "/mnt/data") {
		t.Fatalf("audit leaked a path: %s", auditLine)
	}
}

func TestF2fsDisabledModule404(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeF2fsRead})
	set, err := modules.Parse([]byte(`{"version":1,"disabled":["f2fs"]}`))
	if err != nil {
		t.Fatal(err)
	}
	h.srv.WithModules(set)
	unknown := h.do("GET", "/api/v1/no-such-route", "", nil)
	rr := h.do("GET", "/api/v1/f2fs", "", nil)
	if rr.Code != http.StatusNotFound || rr.Body.String() != unknown.Body.String() {
		t.Fatalf("disabled module: %d %s", rr.Code, rr.Body)
	}
}
