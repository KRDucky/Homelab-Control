// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/mdadminfo"
	"homelab-control/backend/internal/modules"
)

func mdadmFake() func(context.Context) (mdadminfo.Snapshot, error) {
	two := 2
	return func(context.Context) (mdadminfo.Snapshot, error) {
		return mdadminfo.Snapshot{
			Tooling: mdadminfo.Tooling{
				Family: "debian",
				Required: []mdadminfo.ToolItem{
					{ID: "mdadm", Package: "mdadm", Installed: true, Binary: "/usr/sbin/mdadm"},
				},
				Optional: []mdadminfo.ToolItem{},
			},
			MD: mdadminfo.Inventory{Present: true, Personalities: []string{"raid1"}, Arrays: []mdadminfo.Array{
				{Name: "md0", Level: "raid1", State: "active", MemberCount: &two},
			}},
		}, nil
	}
}

func TestMdadmRequiresSessionAndScope(t *testing.T) {
	h := newHarness(t)
	if rr := h.do("GET", "/api/v1/mdadm", "", nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("anon: %d %s", rr.Code, rr.Body)
	}
	h.login("reader", "reader-password-123")
	if rr := h.do("GET", "/api/v1/mdadm", "", packagesHeaders(h)); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "mdadm.read") {
		t.Fatalf("wrong scope: %d %s", rr.Code, rr.Body)
	}
}

func TestMdadmStorageReadDoesNotGrantAccess(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeStorageRead, auth.ScopeStorageProbe})
	if rr := h.do("GET", "/api/v1/mdadm", "", packagesHeaders(h)); rr.Code != http.StatusForbidden {
		t.Fatalf("storage.read must not imply mdadm.read: %d %s", rr.Code, rr.Body)
	}
}

func TestMdadmRequiresCSRFAndOrigin(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeMdadmRead})
	if rr := h.do("GET", "/api/v1/mdadm", "", map[string]string{"Origin": testOrigin}); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "csrf_rejected") {
		t.Fatalf("csrf: %d %s", rr.Code, rr.Body)
	}
	if rr := h.do("GET", "/api/v1/mdadm", "", map[string]string{"Origin": "http://evil.test", "X-CSRF-Token": h.csrf}); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "origin_rejected") {
		t.Fatalf("origin: %d %s", rr.Code, rr.Body)
	}
}

func TestMdadmReadsAndAuditsCountsOnly(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeMdadmRead})
	h.srv.mdadmCollect = mdadmFake()
	rr := h.do("GET", "/api/v1/mdadm", "", packagesHeaders(h))
	if rr.Code != http.StatusOK {
		t.Fatalf("code %d %s", rr.Code, rr.Body)
	}
	body := rr.Body.String()
	for _, want := range []string{`"name":"md0"`, `"level":"raid1"`, `"state":"active"`, `"memberCount":2`, `"package":"mdadm"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body lacks %s: %s", want, body)
		}
	}
	auditLine := h.audit.String()
	if !strings.Contains(auditLine, "mdadm.inventory.read") || !strings.Contains(auditLine, "arrays=1") {
		t.Fatalf("audit must record counts only: %s", auditLine)
	}
	if strings.Contains(auditLine, "md0") || strings.Contains(auditLine, "raid1") {
		t.Fatalf("audit leaked an array detail: %s", auditLine)
	}
}

func TestMdadmCollectFailureIs503(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeMdadmRead})
	h.srv.mdadmCollect = func(context.Context) (mdadminfo.Snapshot, error) {
		return mdadminfo.Snapshot{}, errors.New("cancelled")
	}
	if rr := h.do("GET", "/api/v1/mdadm", "", packagesHeaders(h)); rr.Code != http.StatusServiceUnavailable || !strings.Contains(rr.Body.String(), "mdadm_unavailable") {
		t.Fatalf("collect failure: %d %s", rr.Code, rr.Body)
	}
}

func TestMdadmDisabledModule404(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeMdadmRead})
	set, err := modules.Parse([]byte(`{"version":1,"disabled":["mdadm"]}`))
	if err != nil {
		t.Fatal(err)
	}
	h.srv.WithModules(set)
	unknown := h.do("GET", "/api/v1/no-such-route", "", nil)
	rr := h.do("GET", "/api/v1/mdadm", "", nil)
	if rr.Code != http.StatusNotFound || rr.Body.String() != unknown.Body.String() {
		t.Fatalf("disabled module: %d %s", rr.Code, rr.Body)
	}
}
