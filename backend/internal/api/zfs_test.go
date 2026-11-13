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
	"homelab-control/backend/internal/zfsinfo"
)

func zfsFake() func(context.Context) (zfsinfo.Snapshot, error) {
	return func(context.Context) (zfsinfo.Snapshot, error) {
		return zfsinfo.Snapshot{
			Tooling: zfsinfo.Tooling{
				Family: "debian",
				Required: []zfsinfo.ToolItem{
					{ID: "zfs", Package: "zfsutils-linux", Installed: true, Binary: "/usr/sbin/zfs"},
					{ID: "zpool", Package: "zfsutils-linux", Installed: true, Binary: "/usr/sbin/zpool"},
				},
				Optional: []zfsinfo.ToolItem{},
			},
			Zfs: zfsinfo.Inventory{Present: true, Mounts: []zfsinfo.Mount{
				{Mountpoint: "/tank/media", Dataset: "tank/media"},
			}},
			KernelZfs: zfsinfo.KernelZfs{Present: true},
		}, nil
	}
}

func TestZFSRequiresSessionAndScope(t *testing.T) {
	h := newHarness(t)
	if rr := h.do("GET", "/api/v1/zfs", "", nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("anon: %d %s", rr.Code, rr.Body)
	}
	h.login("reader", "reader-password-123")
	if rr := h.do("GET", "/api/v1/zfs", "", packagesHeaders(h)); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "zfs.read") {
		t.Fatalf("wrong scope: %d %s", rr.Code, rr.Body)
	}
}

func TestZFSRequiresCSRFAndOrigin(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeZFSRead})
	if rr := h.do("GET", "/api/v1/zfs", "", map[string]string{"Origin": testOrigin}); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "csrf_rejected") {
		t.Fatalf("csrf: %d %s", rr.Code, rr.Body)
	}
	if rr := h.do("GET", "/api/v1/zfs", "", map[string]string{"Origin": "http://evil.test", "X-CSRF-Token": h.csrf}); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "origin_rejected") {
		t.Fatalf("origin: %d %s", rr.Code, rr.Body)
	}
}

func TestZFSReadsAndAuditsCountsOnly(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeZFSRead})
	h.srv.zfsCollect = zfsFake()
	rr := h.do("GET", "/api/v1/zfs", "", packagesHeaders(h))
	if rr.Code != http.StatusOK {
		t.Fatalf("code %d %s", rr.Code, rr.Body)
	}
	if !strings.Contains(rr.Body.String(), `"zfsutils-linux"`) || !strings.Contains(rr.Body.String(), `"tank/media"`) {
		t.Fatalf("body: %s", rr.Body)
	}
	auditLine := h.audit.String()
	if !strings.Contains(auditLine, "zfs.inventory.read") || !strings.Contains(auditLine, "mounts=1") {
		t.Fatalf("audit must record counts only: %s", auditLine)
	}
	if strings.Contains(auditLine, "tank/media") || strings.Contains(auditLine, "/tank") {
		t.Fatalf("audit leaked a path: %s", auditLine)
	}
}

func TestZFSDisabledModule404(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeZFSRead})
	set, err := modules.Parse([]byte(`{"version":1,"disabled":["zfs"]}`))
	if err != nil {
		t.Fatal(err)
	}
	h.srv.WithModules(set)
	unknown := h.do("GET", "/api/v1/no-such-route", "", nil)
	rr := h.do("GET", "/api/v1/zfs", "", nil)
	if rr.Code != http.StatusNotFound || rr.Body.String() != unknown.Body.String() {
		t.Fatalf("disabled module: %d %s", rr.Code, rr.Body)
	}
}
