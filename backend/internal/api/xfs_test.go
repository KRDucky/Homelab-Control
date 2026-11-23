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
	"homelab-control/backend/internal/modules"
	"homelab-control/backend/internal/xfsinfo"
)

func xfsFake() func(context.Context) (xfsinfo.Snapshot, error) {
	return func(context.Context) (xfsinfo.Snapshot, error) {
		return xfsinfo.Snapshot{
			Tooling: xfsinfo.Tooling{
				Family: "debian",
				Required: []xfsinfo.ToolItem{
					{ID: "xfs_info", Package: "xfsprogs", Installed: true, Binary: "/usr/sbin/xfs_info"},
				},
				Optional: []xfsinfo.ToolItem{},
			},
			Xfs: xfsinfo.Inventory{Present: true, Mounts: []xfsinfo.Mount{
				{Mountpoint: "/mnt/data", Device: "/dev/sda1"},
			}},
			KernelXfs: xfsinfo.KernelXfs{Present: true},
		}, nil
	}
}

func TestXFSRequiresSessionAndScope(t *testing.T) {
	h := newHarness(t)
	if rr := h.do("GET", "/api/v1/xfs", "", nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("anon: %d %s", rr.Code, rr.Body)
	}
	h.login("reader", "reader-password-123")
	if rr := h.do("GET", "/api/v1/xfs", "", packagesHeaders(h)); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "xfs.read") {
		t.Fatalf("wrong scope: %d %s", rr.Code, rr.Body)
	}
}

func TestXFSRequiresCSRFAndOrigin(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeXFSRead})
	if rr := h.do("GET", "/api/v1/xfs", "", map[string]string{"Origin": testOrigin}); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "csrf_rejected") {
		t.Fatalf("csrf: %d %s", rr.Code, rr.Body)
	}
	if rr := h.do("GET", "/api/v1/xfs", "", map[string]string{"Origin": "http://evil.test", "X-CSRF-Token": h.csrf}); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "origin_rejected") {
		t.Fatalf("origin: %d %s", rr.Code, rr.Body)
	}
}

func TestXFSReadsAndAuditsCountsOnly(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeXFSRead})
	h.srv.xfsCollect = xfsFake()
	rr := h.do("GET", "/api/v1/xfs", "", packagesHeaders(h))
	if rr.Code != http.StatusOK {
		t.Fatalf("code %d %s", rr.Code, rr.Body)
	}
	if !strings.Contains(rr.Body.String(), `"xfsprogs"`) || !strings.Contains(rr.Body.String(), `"/dev/sda1"`) {
		t.Fatalf("body: %s", rr.Body)
	}
	auditLine := h.audit.String()
	if !strings.Contains(auditLine, "xfs.inventory.read") || !strings.Contains(auditLine, "mounts=1") {
		t.Fatalf("audit must record counts only: %s", auditLine)
	}
	if strings.Contains(auditLine, "/dev/sda1") || strings.Contains(auditLine, "/mnt/data") {
		t.Fatalf("audit leaked a path: %s", auditLine)
	}
}

func TestXFSDisabledModule404(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeXFSRead})
	set, err := modules.Parse([]byte(`{"version":1,"disabled":["xfs"]}`))
	if err != nil {
		t.Fatal(err)
	}
	h.srv.WithModules(set)
	unknown := h.do("GET", "/api/v1/no-such-route", "", nil)
	rr := h.do("GET", "/api/v1/xfs", "", nil)
	if rr.Code != http.StatusNotFound || rr.Body.String() != unknown.Body.String() {
		t.Fatalf("disabled module: %d %s", rr.Code, rr.Body)
	}
}
