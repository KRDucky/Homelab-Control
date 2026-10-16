package api

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/brokerclient"
	"homelab-control/backend/internal/hwinfo"
)

// fakeBroker answers ops from a table and records actors.
type fakeBroker struct {
	results map[string]any
	errs    map[string]error
	down    bool
	actors  []proto.Actor
}

func (f *fakeBroker) Call(_ context.Context, op string, actor proto.Actor, out any) error {
	f.actors = append(f.actors, actor)
	if f.down {
		return brokerclient.ErrUnavailable
	}
	if err, ok := f.errs[op]; ok {
		return err
	}
	b, _ := json.Marshal(f.results[op])
	return json.Unmarshal(b, out)
}

func newFake() *fakeBroker {
	return &fakeBroker{errs: map[string]error{}, results: map[string]any{
		"broker.info": map[string]any{"version": "t", "systemd_booted": true, "exec_available": []string{"systemctl"}, "ops": []map[string]string{
			{"name": "broker.info"}, {"name": "services.systemd.list"}, {"name": "hardware.dmi.identity"}, {"name": "legacy.webmin.status"}}},
		"services.systemd.list": map[string]any{"units": []map[string]string{{"name": "ssh.service", "type": "service", "load": "loaded", "active": "active", "sub": "running"}}, "counts": map[string]int{"active": 1}},
		"hardware.dmi.identity": map[string]any{"available": true, "fields": map[string]any{"product_serial": map[string]string{"value": "S1", "status": "present"}}},
		"legacy.webmin.status":  map[string]any{"installed": true, "version": "2.670", "checks": []any{}, "rpc_capable_users": []string{}},
	}}
}

func harnessWith(t *testing.T, fb *fakeBroker, scopes []string) *harness {
	h := newHarness(t)
	h1, _ := auth.HashPassword("full-password-1234", auth.MinIterations)
	h.users.Users = append(h.users.Users, auth.User{Username: "full", PasswordHash: h1, Scopes: scopes})
	h.srv.users = auth.NewStore(h.users)
	root, _ := filepath.Abs("../hwinfo/testdata/host")
	h.srv.WithHardware(&hwinfo.Collector{Roots: hwinfo.Roots{Root: root}})
	if fb != nil {
		h.srv.WithBroker(fb)
	}
	if rr := h.login("full", "full-password-1234"); rr.Code != 200 {
		t.Fatal(rr.Body)
	}
	return h
}

var allScopes = []string{auth.ScopeSystemRead, auth.ScopeServicesRead, auth.ScopeHardwareRead}

func caps(t *testing.T, h *harness) map[string]Capability {
	rr := h.do("GET", "/api/v1/capabilities", "", nil)
	if rr.Code != 200 {
		t.Fatalf("%d %s", rr.Code, rr.Body)
	}
	var resp capabilitiesResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	m := map[string]Capability{}
	for _, c := range resp.Capabilities {
		m[c.ID] = c
	}
	return m
}

func TestCapabilitiesWithBroker(t *testing.T) {
	h := harnessWith(t, newFake(), allScopes)
	c := caps(t, h)
	for _, id := range []string{"overview", "services", "hardware", "legacy"} {
		if c[id].Status != CapAvailable {
			t.Errorf("%s: %+v", id, c[id])
		}
	}
}

func TestCapabilitiesBrokerDown(t *testing.T) {
	fb := newFake()
	fb.down = true
	h := harnessWith(t, fb, allScopes)
	c := caps(t, h)
	if c["services"].Status != CapUnavailable || !strings.Contains(c["services"].Reason, "homelab-control-broker") {
		t.Errorf("services: %+v", c["services"])
	}
	if c["hardware"].Status != CapDegraded || c["legacy"].Status != CapDegraded || c["overview"].Status != CapAvailable {
		t.Errorf("degradation wrong: %+v", c)
	}
}

func TestCapabilitiesNoSystemd(t *testing.T) {
	for name, mut := range map[string]func(m map[string]any){
		"no systemctl":       func(m map[string]any) { m["exec_available"] = []string{} },
		"systemd not booted": func(m map[string]any) { m["systemd_booted"] = false },
	} {
		fb := newFake()
		mut(fb.results["broker.info"].(map[string]any))
		h := harnessWith(t, fb, allScopes)
		if c := caps(t, h); c["services"].Status != CapUnsupported {
			t.Errorf("%s: %+v", name, c["services"])
		}
	}
}

func TestCapabilitiesRespectScopes(t *testing.T) {
	h := harnessWith(t, newFake(), []string{auth.ScopeSystemRead})
	c := caps(t, h)
	if c["services"].Status != CapForbidden || c["hardware"].Status != CapForbidden || c["overview"].Status != CapAvailable {
		t.Fatalf("%+v", c)
	}
	if rr := h.do("GET", "/api/v1/services", "", nil); rr.Code != http.StatusForbidden {
		t.Fatalf("services without scope: %d", rr.Code)
	}
	if rr := h.do("GET", "/api/v1/hardware", "", nil); rr.Code != http.StatusForbidden {
		t.Fatalf("hardware without scope: %d", rr.Code)
	}
}

func TestServicesEndpoint(t *testing.T) {
	fb := newFake()
	h := harnessWith(t, fb, allScopes)
	rr := h.do("GET", "/api/v1/services", "", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "ssh.service") {
		t.Fatalf("%d %s", rr.Code, rr.Body)
	}
	last := fb.actors[len(fb.actors)-1]
	if last.User != "full" || len(last.Scopes) != 3 || last.RequestID == "" {
		t.Fatalf("actor not forwarded: %+v", last)
	}
	fb.down = true
	if rr := h.do("GET", "/api/v1/services", "", nil); rr.Code != http.StatusServiceUnavailable || !strings.Contains(rr.Body.String(), "broker_unavailable") {
		t.Fatalf("broker down: %d %s", rr.Code, rr.Body)
	}
	fb.down = false
	fb.errs["services.systemd.list"] = &proto.Error{Code: proto.CodeTimeout, Message: "slow"}
	if rr := h.do("GET", "/api/v1/services", "", nil); rr.Code != http.StatusGatewayTimeout {
		t.Fatalf("timeout mapping: %d", rr.Code)
	}
	fb.errs["services.systemd.list"] = &proto.Error{Code: proto.CodeInternal, Message: "secret internal detail"}
	if rr := h.do("GET", "/api/v1/services", "", nil); rr.Code != http.StatusBadGateway || strings.Contains(rr.Body.String(), "secret internal detail") {
		t.Fatalf("internal errors must not leak: %d %s", rr.Code, rr.Body)
	}
}

func TestServicesWithoutBrokerConfigured(t *testing.T) {
	h := harnessWith(t, nil, allScopes)
	if rr := h.do("GET", "/api/v1/services", "", nil); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("%d", rr.Code)
	}
}

func TestHardwareEndpoint(t *testing.T) {
	fb := newFake()
	h := harnessWith(t, fb, allScopes)
	rr := h.do("GET", "/api/v1/hardware", "", nil)
	var resp hardwareResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil || rr.Code != 200 {
		t.Fatalf("%d %v", rr.Code, err)
	}
	if resp.Identity.Status != "ok" || resp.Identity.Data.Fields["product_serial"].Value != "S1" {
		t.Fatalf("%+v", resp.Identity)
	}
	if len(resp.Inventory.Disks.Devices) == 0 {
		t.Fatal("fixture disks missing")
	}
	fb.down = true
	rr = h.do("GET", "/api/v1/hardware", "", nil)
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if rr.Code != 200 || resp.Identity.Status != "unavailable" || len(resp.Inventory.Disks.Devices) == 0 {
		t.Fatalf("hardware must degrade, not fail: %d %+v", rr.Code, resp.Identity)
	}
}

func TestLegacyEndpointFallsBack(t *testing.T) {
	fb := newFake()
	h := harnessWith(t, fb, allScopes)
	rr := h.do("GET", "/api/v1/legacy/webmin", "", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"source":"broker"`) {
		t.Fatalf("%s", rr.Body)
	}
	fb.down = true
	rr = h.do("GET", "/api/v1/legacy/webmin", "", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"source":"unprivileged"`) || !strings.Contains(rr.Body.String(), `"fallback"`) {
		t.Fatalf("%s", rr.Body)
	}
}

func TestCapabilitiesRequireSession(t *testing.T) {
	h := newHarness(t)
	if rr := h.do("GET", "/api/v1/capabilities", "", nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("%d", rr.Code)
	}
}
