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
	"homelab-control/backend/internal/mergerfsinfo"
	"homelab-control/backend/internal/modules"
)

func mergerfsFake() func(context.Context) (mergerfsinfo.Snapshot, error) {
	return func(context.Context) (mergerfsinfo.Snapshot, error) {
		return mergerfsinfo.Snapshot{
			Tooling: mergerfsinfo.Tooling{
				Family: "debian",
				Required: []mergerfsinfo.ToolItem{
					{ID: "mergerfs", Package: "mergerfs", Installed: true, Binary: "/usr/bin/mergerfs"},
					{ID: "fusermount3", Package: "fuse3", Installed: true, Binary: "/usr/bin/fusermount3"},
				},
				Optional: []mergerfsinfo.ToolItem{{ID: "snapraid", Package: "snapraid", Installed: false}},
			},
			MergerFS: mergerfsinfo.MergerFS{Present: true, Pools: []mergerfsinfo.Pool{
				{Mountpoint: "/mnt/media", Branches: []string{"/mnt/disk1", "/mnt/disk2"}, Options: []string{"category.create=epmfs", "minfreespace=20G"}, Usage: []mergerfsinfo.BranchUsage{}},
			}, Configured: []mergerfsinfo.ConfiguredMount{
				{Origin: "fstab", Path: "/etc/fstab", Mountpoint: "/mnt/media", Branches: []string{"/mnt/disk1", "/mnt/disk2"}, Mounted: true},
			}},
			SnapRAID:          mergerfsinfo.SnapRAID{Present: true, ConfigPath: "/etc/snapraid.conf", Data: []string{"/mnt/disk1"}, Parity: []string{"/mnt/parity1"}, Content: []string{"/var/snapraid/content"}},
			KernelFuse:        mergerfsinfo.KernelFuse{Present: true},
			Warnings:          []mergerfsinfo.Warning{},
			WarningsTruncated: false,
		}, nil
	}
}

func TestMergerFSRequiresSessionAndScope(t *testing.T) {
	h := newHarness(t)
	if rr := h.do("GET", "/api/v1/mergerfs", "", nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("anon: %d", rr.Code)
	}
	h.login("reader", "reader-password-123")
	if rr := h.do("GET", "/api/v1/mergerfs", "", packagesHeaders(h)); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "mergerfs.read") {
		t.Fatalf("system.read: %d %s", rr.Code, rr.Body)
	}
}

func TestMergerFSRequiresCSRFAndOrigin(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeMergerFSRead})
	if rr := h.do("GET", "/api/v1/mergerfs", "", map[string]string{"Origin": testOrigin}); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "csrf_rejected") {
		t.Fatalf("missing csrf: %d %s", rr.Code, rr.Body)
	}
	if rr := h.do("GET", "/api/v1/mergerfs", "", map[string]string{"Origin": "http://evil.test", "X-CSRF-Token": h.csrf}); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "origin_rejected") {
		t.Fatalf("cross-origin: %d %s", rr.Code, rr.Body)
	}
}

func TestMergerFSReadsAndAuditsCountsOnly(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeMergerFSRead})
	h.srv.mergerfsCollect = mergerfsFake()
	rr := h.do("GET", "/api/v1/mergerfs", "", packagesHeaders(h))
	if rr.Code != http.StatusOK {
		t.Fatalf("mergerfs: %d %s", rr.Code, rr.Body)
	}
	if !strings.Contains(rr.Body.String(), "/mnt/media") {
		t.Fatalf("body: %s", rr.Body)
	}
	if !strings.Contains(rr.Body.String(), `"snapraid"`) || !strings.Contains(rr.Body.String(), `"present":true`) {
		t.Fatalf("body missing snapraid: %s", rr.Body)
	}
	auditLine := h.audit.String()
	if !strings.Contains(auditLine, "mergerfs.inventory.read") || !strings.Contains(auditLine, "pools=1") || !strings.Contains(auditLine, "configured=1") || !strings.Contains(auditLine, "snapraid=true") {
		t.Fatalf("audit must record counts only: %s", auditLine)
	}
	for _, secret := range []string{"/mnt/disk1", "/mnt/parity1", "/var/snapraid/content"} {
		if strings.Contains(auditLine, secret) {
			t.Fatalf("audit leaked a path %q: %s", secret, auditLine)
		}
	}
}

func TestMergerFSDisabledModule404(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeMergerFSRead})
	set, err := modules.Parse([]byte(`{"version":1,"disabled":["mergerfs"]}`))
	if err != nil {
		t.Fatal(err)
	}
	h.srv.WithModules(set)
	unknown := h.do("GET", "/api/v1/no-such-route", "", nil)
	rr := h.do("GET", "/api/v1/mergerfs", "", nil)
	if rr.Code != http.StatusNotFound || rr.Body.String() != unknown.Body.String() {
		t.Fatalf("disabled module: %d %s", rr.Code, rr.Body)
	}
}
