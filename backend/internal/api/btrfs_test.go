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
	"homelab-control/backend/internal/btrfsinfo"
	"homelab-control/backend/internal/modules"
)

func btrfsFake() func(context.Context) (btrfsinfo.Snapshot, error) {
	return func(context.Context) (btrfsinfo.Snapshot, error) {
		return btrfsinfo.Snapshot{
			Tooling: btrfsinfo.Tooling{
				Family: "debian",
				Required: []btrfsinfo.ToolItem{
					{ID: "btrfs", Package: "btrfs-progs", Installed: true, Binary: "/usr/bin/btrfs"},
				},
				Optional: []btrfsinfo.ToolItem{},
			},
			Btrfs: btrfsinfo.Inventory{Present: true, Mounts: []btrfsinfo.Mount{
				{Mountpoint: "/mnt/data", Device: "/dev/sda1", Subvol: "/", SubvolID: "5"},
			}},
			KernelBtrfs: btrfsinfo.KernelBtrfs{Present: true},
		}, nil
	}
}

func TestBtrfsRequiresSessionAndScope(t *testing.T) {
	h := newHarness(t)
	if rr := h.do("GET", "/api/v1/btrfs", "", nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("anon: %d %s", rr.Code, rr.Body)
	}
	h.login("reader", "reader-password-123")
	if rr := h.do("GET", "/api/v1/btrfs", "", packagesHeaders(h)); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "btrfs.read") {
		t.Fatalf("wrong scope: %d %s", rr.Code, rr.Body)
	}
}

func TestBtrfsRequiresCSRFAndOrigin(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeBtrfsRead})
	if rr := h.do("GET", "/api/v1/btrfs", "", map[string]string{"Origin": testOrigin}); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "csrf_rejected") {
		t.Fatalf("csrf: %d %s", rr.Code, rr.Body)
	}
	if rr := h.do("GET", "/api/v1/btrfs", "", map[string]string{"Origin": "http://evil.test", "X-CSRF-Token": h.csrf}); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "origin_rejected") {
		t.Fatalf("origin: %d %s", rr.Code, rr.Body)
	}
}

func TestBtrfsReadsAndAuditsCountsOnly(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeBtrfsRead})
	h.srv.btrfsCollect = btrfsFake()
	rr := h.do("GET", "/api/v1/btrfs", "", packagesHeaders(h))
	if rr.Code != http.StatusOK {
		t.Fatalf("code %d %s", rr.Code, rr.Body)
	}
	if !strings.Contains(rr.Body.String(), `"btrfs-progs"`) || !strings.Contains(rr.Body.String(), `"/dev/sda1"`) {
		t.Fatalf("body: %s", rr.Body)
	}
	auditLine := h.audit.String()
	if !strings.Contains(auditLine, "btrfs.inventory.read") || !strings.Contains(auditLine, "mounts=1") {
		t.Fatalf("audit must record counts only: %s", auditLine)
	}
	if strings.Contains(auditLine, "/dev/sda1") || strings.Contains(auditLine, "/mnt/data") {
		t.Fatalf("audit leaked a path: %s", auditLine)
	}
}

func TestBtrfsDisabledModule404(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeBtrfsRead})
	set, err := modules.Parse([]byte(`{"version":1,"disabled":["btrfs"]}`))
	if err != nil {
		t.Fatal(err)
	}
	h.srv.WithModules(set)
	unknown := h.do("GET", "/api/v1/no-such-route", "", nil)
	rr := h.do("GET", "/api/v1/btrfs", "", nil)
	if rr.Code != http.StatusNotFound || rr.Body.String() != unknown.Body.String() {
		t.Fatalf("disabled module: %d %s", rr.Code, rr.Body)
	}
}
