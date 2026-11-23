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
	"homelab-control/backend/internal/ext4info"
	"homelab-control/backend/internal/modules"
)

func ext4Fake() func(context.Context) (ext4info.Snapshot, error) {
	return func(context.Context) (ext4info.Snapshot, error) {
		return ext4info.Snapshot{
			Tooling: ext4info.Tooling{
				Family: "debian",
				Required: []ext4info.ToolItem{
					{ID: "dumpe2fs", Package: "e2fsprogs", Installed: true, Binary: "/usr/sbin/dumpe2fs"},
				},
				Optional: []ext4info.ToolItem{},
			},
			Ext4: ext4info.Inventory{Present: true, Mounts: []ext4info.Mount{
				{Mountpoint: "/mnt/data", Device: "/dev/sda1"},
			}},
			KernelExt4: ext4info.KernelExt4{Present: true},
		}, nil
	}
}

func TestExt4RequiresSessionAndScope(t *testing.T) {
	h := newHarness(t)
	if rr := h.do("GET", "/api/v1/ext4", "", nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("anon: %d %s", rr.Code, rr.Body)
	}
	h.login("reader", "reader-password-123")
	if rr := h.do("GET", "/api/v1/ext4", "", packagesHeaders(h)); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "ext4.read") {
		t.Fatalf("wrong scope: %d %s", rr.Code, rr.Body)
	}
}

func TestExt4RequiresCSRFAndOrigin(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeExt4Read})
	if rr := h.do("GET", "/api/v1/ext4", "", map[string]string{"Origin": testOrigin}); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "csrf_rejected") {
		t.Fatalf("csrf: %d %s", rr.Code, rr.Body)
	}
	if rr := h.do("GET", "/api/v1/ext4", "", map[string]string{"Origin": "http://evil.test", "X-CSRF-Token": h.csrf}); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "origin_rejected") {
		t.Fatalf("origin: %d %s", rr.Code, rr.Body)
	}
}

func TestExt4ReadsAndAuditsCountsOnly(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeExt4Read})
	h.srv.ext4Collect = ext4Fake()
	rr := h.do("GET", "/api/v1/ext4", "", packagesHeaders(h))
	if rr.Code != http.StatusOK {
		t.Fatalf("code %d %s", rr.Code, rr.Body)
	}
	if !strings.Contains(rr.Body.String(), `"e2fsprogs"`) || !strings.Contains(rr.Body.String(), `"/dev/sda1"`) {
		t.Fatalf("body: %s", rr.Body)
	}
	auditLine := h.audit.String()
	if !strings.Contains(auditLine, "ext4.inventory.read") || !strings.Contains(auditLine, "mounts=1") {
		t.Fatalf("audit must record counts only: %s", auditLine)
	}
	if strings.Contains(auditLine, "/dev/sda1") || strings.Contains(auditLine, "/mnt/data") {
		t.Fatalf("audit leaked a path: %s", auditLine)
	}
}

func TestExt4DisabledModule404(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeExt4Read})
	set, err := modules.Parse([]byte(`{"version":1,"disabled":["ext4"]}`))
	if err != nil {
		t.Fatal(err)
	}
	h.srv.WithModules(set)
	unknown := h.do("GET", "/api/v1/no-such-route", "", nil)
	rr := h.do("GET", "/api/v1/ext4", "", nil)
	if rr.Code != http.StatusNotFound || rr.Body.String() != unknown.Body.String() {
		t.Fatalf("disabled module: %d %s", rr.Code, rr.Body)
	}
}
