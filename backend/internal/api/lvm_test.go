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
	"homelab-control/backend/internal/lvminfo"
	"homelab-control/backend/internal/modules"
)

func lvmFake() func(context.Context) (lvminfo.Snapshot, error) {
	return func(context.Context) (lvminfo.Snapshot, error) {
		return lvminfo.Snapshot{
			Tooling: lvminfo.Tooling{
				Family: "debian",
				Required: []lvminfo.ToolItem{
					{ID: "lvm", Package: "lvm2", Installed: true, Binary: "/usr/sbin/lvm"},
				},
				Optional: []lvminfo.ToolItem{
					{ID: "thin_check", Package: "thin-provisioning-tools", Installed: false},
				},
			},
			Lvm: lvminfo.Inventory{Present: true, Devices: []lvminfo.Device{
				{Name: "dm-0", Mapper: "vg-lv"},
			}},
			KernelDM: lvminfo.KernelDM{Present: true},
		}, nil
	}
}

func TestLVMRequiresSessionAndScope(t *testing.T) {
	h := newHarness(t)
	if rr := h.do("GET", "/api/v1/lvm", "", nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("anon: %d %s", rr.Code, rr.Body)
	}
	h.login("reader", "reader-password-123")
	if rr := h.do("GET", "/api/v1/lvm", "", packagesHeaders(h)); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "lvm.read") {
		t.Fatalf("wrong scope: %d %s", rr.Code, rr.Body)
	}
}

func TestLVMRequiresCSRFAndOrigin(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeLVMRead})
	if rr := h.do("GET", "/api/v1/lvm", "", map[string]string{"Origin": testOrigin}); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "csrf_rejected") {
		t.Fatalf("csrf: %d %s", rr.Code, rr.Body)
	}
	if rr := h.do("GET", "/api/v1/lvm", "", map[string]string{"Origin": "http://evil.test", "X-CSRF-Token": h.csrf}); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "origin_rejected") {
		t.Fatalf("origin: %d %s", rr.Code, rr.Body)
	}
}

func TestLVMReadsAndAuditsCountsOnly(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeLVMRead})
	h.srv.lvmCollect = lvmFake()
	rr := h.do("GET", "/api/v1/lvm", "", packagesHeaders(h))
	if rr.Code != http.StatusOK {
		t.Fatalf("code %d %s", rr.Code, rr.Body)
	}
	if !strings.Contains(rr.Body.String(), `"lvm2"`) || !strings.Contains(rr.Body.String(), `"vg-lv"`) {
		t.Fatalf("body: %s", rr.Body)
	}
	auditLine := h.audit.String()
	if !strings.Contains(auditLine, "lvm.inventory.read") || !strings.Contains(auditLine, "devices=1") {
		t.Fatalf("audit must record counts only: %s", auditLine)
	}
	if strings.Contains(auditLine, "vg-lv") || strings.Contains(auditLine, "dm-0") || strings.Contains(auditLine, "LVM-") {
		t.Fatalf("audit leaked a name: %s", auditLine)
	}
}

func TestLVMDisabledModule404(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeLVMRead})
	set, err := modules.Parse([]byte(`{"version":1,"disabled":["lvm"]}`))
	if err != nil {
		t.Fatal(err)
	}
	h.srv.WithModules(set)
	unknown := h.do("GET", "/api/v1/no-such-route", "", nil)
	rr := h.do("GET", "/api/v1/lvm", "", nil)
	if rr.Code != http.StatusNotFound || rr.Body.String() != unknown.Body.String() {
		t.Fatalf("disabled module: %d %s", rr.Code, rr.Body)
	}
}
