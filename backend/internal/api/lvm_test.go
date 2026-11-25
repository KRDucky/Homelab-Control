// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"context"
	"encoding/json"
	"homelab-control/backend/internal/brokerclient"
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

const lvmTestVGS = `{"report":[{"vg":[{"vg_uuid":"` + probeVG + `","vg_name":"vg1","vg_size":"1073741824","vg_free":"536870912","vg_attr":"wz--n-","pv_count":"1","lv_count":"1","vg_missing_pv_count":"0"}]}]}`

func lvmBroker() *fakeBroker {
	fb := newFake()
	fb.results["lvm.status"] = map[string]any{"pvs": json.RawMessage(probePVS), "lvs": json.RawMessage(probeLVS), "vgs": json.RawMessage(lvmTestVGS)}
	return fb
}

func TestLVMReportsFromBrokerAndAuditsCountsOnly(t *testing.T) {
	fb := lvmBroker()
	h := harnessWith(t, fb, []string{auth.ScopeLVMRead})
	h.srv.lvmCollect = lvmFake()
	rr := h.do("GET", "/api/v1/lvm", "", packagesHeaders(h))
	if rr.Code != http.StatusOK {
		t.Fatalf("code %d %s", rr.Code, rr.Body)
	}
	body := rr.Body.String()
	for _, want := range []string{`"lvm2"`, `"status":{"available":true`, `"volume_groups":[{"uuid":"` + probeVG + `","name":"vg1"`, `"free_bytes":536870912`, `"name":"lv1"`, `"physical_volumes":[{`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s: %s", want, body)
		}
	}
	auditLine := h.audit.String()
	if !strings.Contains(auditLine, "lvm.inventory.read") || !strings.Contains(auditLine, "vgs=1 lvs=1 pvs=1") {
		t.Fatalf("audit must record counts only: %s", auditLine)
	}
	if strings.Contains(auditLine, "vg1") || strings.Contains(auditLine, "lv1") || strings.Contains(auditLine, "/dev/") {
		t.Fatalf("audit leaked a name: %s", auditLine)
	}
	if left, ok := fb.deadlines["lvm.status"]; ok && left < brokerclient.DefaultDeadline("lvm.status") {
		t.Fatalf("hc-api gave lvm.status %v, shorter than the broker client", left)
	}
}

func TestLVMUnparsableReportIsAReason(t *testing.T) {
	fb := lvmBroker()
	fb.results["lvm.status"] = map[string]any{"pvs": json.RawMessage(probePVS), "lvs": json.RawMessage(probeLVS), "vgs": json.RawMessage(`{"report":[{"vg":[{"vg_uuid":"` + probeVG + `","vg_size":"lots"}]}]}`)}
	h := harnessWith(t, fb, []string{auth.ScopeLVMRead})
	h.srv.lvmCollect = lvmFake()
	rr := h.do("GET", "/api/v1/lvm", "", packagesHeaders(h))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"available":false,"reason":"LVM status is unavailable: the LVM report could not be read`) {
		t.Fatalf("code %d %s", rr.Code, rr.Body)
	}
}

func TestLVMWithoutLvm2SkipsBroker(t *testing.T) {
	fb := lvmBroker()
	h := harnessWith(t, fb, []string{auth.ScopeLVMRead})
	h.srv.lvmCollect = func(ctx context.Context) (lvminfo.Snapshot, error) {
		s, _ := lvmFake()(ctx)
		s.Tooling.Required[0].Installed = false
		return s, nil
	}
	rr := h.do("GET", "/api/v1/lvm", "", packagesHeaders(h))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "lvm2 is not installed") {
		t.Fatalf("code %d %s", rr.Code, rr.Body)
	}
	for _, op := range fb.ops {
		if op == "lvm.status" {
			t.Fatal("broker asked although lvm2 is not installed")
		}
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
