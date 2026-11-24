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
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/modules"
	"homelab-control/backend/internal/netinfo"
)

var networkScopes = []string{auth.ScopeNetworkingPortsRead, auth.ScopeNetworkingBondsRead, auth.ScopeNetworkingFirewallRead}

func networkFake() *fakeBroker {
	fb := newFake()
	info := fb.results["broker.info"].(map[string]any)
	info["ops"] = append(info["ops"].([]map[string]string), map[string]string{"name": "network.ports.list"}, map[string]string{"name": "network.firewall.config"})
	fb.results["network.ports.list"] = netinfo.PortMap{
		Namespace: "host (network namespace of PID 1)", Sources: []string{"tcp"}, ProcessesScanned: 9, ProcessesHidden: 2,
		Sockets: []netinfo.Socket{{Proto: "tcp", Address: "192.168.7.7", Port: 22, Exposure: "address", UID: 0, Account: "root",
			Owners: []netinfo.Owner{{PID: 812, Command: "sshd", Unit: "ssh.service"}}}},
	}
	fb.results["network.firewall.config"] = netinfo.FirewallSnapshot{
		Active: netinfo.FirewallUfw, Source: "saved-configuration",
		Ufw: &netinfo.UfwView{Enabled: true, Rules: []netinfo.UfwRule{{Action: "allow", Proto: "tcp", ToPort: "22", From: "10.9.9.0/24", Direction: "in"}}},
	}
	return fb
}

func TestNetworkRoutesRequireSessionAndScope(t *testing.T) {
	for _, path := range []string{"/api/v1/ports", "/api/v1/bonds", "/api/v1/firewall"} {
		h := newHarness(t)
		if rr := h.do("GET", path, "", nil); rr.Code != http.StatusUnauthorized {
			t.Fatalf("%s anon: %d", path, rr.Code)
		}
		h.login("reader", "reader-password-123")
		if rr := h.do("GET", path, "", packagesHeaders(h)); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "networking.") {
			t.Fatalf("%s wrong scope: %d %s", path, rr.Code, rr.Body)
		}
	}
	// Each scope grants its own route only.
	h := harnessWith(t, networkFake(), []string{auth.ScopeNetworkingPortsRead})
	if rr := h.do("GET", "/api/v1/firewall", "", packagesHeaders(h)); rr.Code != http.StatusForbidden {
		t.Fatalf("ports scope must not read the firewall: %d", rr.Code)
	}
	if rr := h.do("GET", "/api/v1/bonds", "", packagesHeaders(h)); rr.Code != http.StatusForbidden {
		t.Fatalf("ports scope must not read bonds: %d", rr.Code)
	}
}

func TestNetworkRoutesRequireCSRFAndOrigin(t *testing.T) {
	h := harnessWith(t, networkFake(), networkScopes)
	for _, path := range []string{"/api/v1/ports", "/api/v1/bonds", "/api/v1/firewall"} {
		if rr := h.do("GET", path, "", map[string]string{"Origin": testOrigin}); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "csrf_rejected") {
			t.Fatalf("%s csrf: %d %s", path, rr.Code, rr.Body)
		}
		if rr := h.do("GET", path, "", map[string]string{"Origin": "http://evil.test", "X-CSRF-Token": h.csrf}); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "origin_rejected") {
			t.Fatalf("%s origin: %d %s", path, rr.Code, rr.Body)
		}
	}
}

func TestPortsReadsBrokerAndAuditsCountsOnly(t *testing.T) {
	fb := networkFake()
	h := harnessWith(t, fb, networkScopes)
	rr := h.do("GET", "/api/v1/ports", "", packagesHeaders(h))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"ssh.service"`) || !strings.Contains(rr.Body.String(), `"processesHidden":2`) {
		t.Fatalf("%d %s", rr.Code, rr.Body)
	}
	if strings.Contains(rr.Body.String(), "inodes") {
		t.Fatalf("internal inode list leaked: %s", rr.Body)
	}
	a := h.audit.String()
	if !strings.Contains(a, "network.ports.read") || !strings.Contains(a, "sockets=1 hidden=2") || strings.Contains(a, "192.168.7.7") || strings.Contains(a, "sshd") {
		t.Fatalf("audit must carry counts only: %s", a)
	}
	if last := fb.actors[len(fb.actors)-1]; last.User != "full" {
		t.Fatalf("broker actor: %+v", last)
	}
}

func TestFirewallReadsBrokerAndAuditsBackendOnly(t *testing.T) {
	h := harnessWith(t, networkFake(), networkScopes)
	rr := h.do("GET", "/api/v1/firewall", "", packagesHeaders(h))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"active":"ufw"`) || !strings.Contains(rr.Body.String(), `"saved-configuration"`) {
		t.Fatalf("%d %s", rr.Code, rr.Body)
	}
	a := h.audit.String()
	if !strings.Contains(a, "network.firewall.read") || !strings.Contains(a, "active=ufw") || strings.Contains(a, "10.9.9.0") {
		t.Fatalf("audit must carry the backend only: %s", a)
	}
}

func TestFirewallRejectsUnknownBackendFromBroker(t *testing.T) {
	fb := networkFake()
	fb.results["network.firewall.config"] = map[string]any{"active": "<script>"}
	h := harnessWith(t, fb, networkScopes)
	if rr := h.do("GET", "/api/v1/firewall", "", packagesHeaders(h)); rr.Code != http.StatusBadGateway {
		t.Fatalf("unknown backend accepted: %d %s", rr.Code, rr.Body)
	}
}

func TestNetworkBrokerFailuresMapAndAudit(t *testing.T) {
	fb := networkFake()
	fb.errs["network.ports.list"] = &proto.Error{Code: proto.CodeUnavailable, Message: "the host socket tables could not be read"}
	h := harnessWith(t, fb, networkScopes)
	if rr := h.do("GET", "/api/v1/ports", "", packagesHeaders(h)); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable: %d %s", rr.Code, rr.Body)
	}
	if !strings.Contains(h.audit.String(), `"unavailable"`) {
		t.Fatalf("failure not audited: %s", h.audit.String())
	}
	fb.down = true
	if rr := h.do("GET", "/api/v1/firewall", "", packagesHeaders(h)); rr.Code != http.StatusServiceUnavailable || !strings.Contains(rr.Body.String(), "broker_unavailable") {
		t.Fatalf("broker down: %d %s", rr.Code, rr.Body)
	}
}

func TestBondsReadsCollectorAndAuditsCount(t *testing.T) {
	h := harnessWith(t, networkFake(), networkScopes)
	h.srv.bondsCollect = func(context.Context) (netinfo.BondSnapshot, error) {
		return netinfo.BondSnapshot{DriverLoaded: true, Stacks: []string{"systemd-networkd"}, Bonds: []netinfo.Bond{{Name: "bond0", Mode: "802.3ad", LACP: true, Slaves: []netinfo.BondSlave{{Name: "enp1s0"}}, Renderer: netinfo.Renderer{ID: "systemd-networkd", Confidence: "observed"}}}}, nil
	}
	rr := h.do("GET", "/api/v1/bonds", "", packagesHeaders(h))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"802.3ad"`) {
		t.Fatalf("%d %s", rr.Code, rr.Body)
	}
	if a := h.audit.String(); !strings.Contains(a, "network.bonds.read") || !strings.Contains(a, "bonds=1") || strings.Contains(a, "enp1s0") {
		t.Fatalf("audit: %s", a)
	}
	h.srv.bondsCollect = func(context.Context) (netinfo.BondSnapshot, error) { return netinfo.BondSnapshot{}, errors.New("boom") }
	if rr := h.do("GET", "/api/v1/bonds", "", packagesHeaders(h)); rr.Code != http.StatusServiceUnavailable || strings.Contains(rr.Body.String(), "boom") {
		t.Fatalf("collector error: %d %s", rr.Code, rr.Body)
	}
}

func TestNetworkDisabledModules404AndDropCapability(t *testing.T) {
	h := harnessWith(t, networkFake(), networkScopes)
	set, err := modules.Parse([]byte(`{"version":1,"disabled":["ports","bonds","firewall"]}`))
	if err != nil {
		t.Fatal(err)
	}
	h.srv.WithModules(set)
	unknown := h.do("GET", "/api/v1/no-such-route", "", nil)
	for _, path := range []string{"/api/v1/ports", "/api/v1/bonds", "/api/v1/firewall"} {
		if rr := h.do("GET", path, "", nil); rr.Code != http.StatusNotFound || rr.Body.String() != unknown.Body.String() {
			t.Fatalf("%s disabled: %d %s", path, rr.Code, rr.Body)
		}
	}
	c := caps(t, h)
	for _, id := range []string{"ports", "bonds", "firewall"} {
		if _, ok := c[id]; ok {
			t.Errorf("capability %s of a disabled module listed", id)
		}
	}
}

func TestNetworkCapabilities(t *testing.T) {
	h := harnessWith(t, networkFake(), networkScopes)
	c := caps(t, h)
	if c["ports"].Status != CapAvailable || c["firewall"].Status != CapAvailable || c["ports"].Path != "/network/ports" {
		t.Fatalf("caps: %+v %+v", c["ports"], c["firewall"])
	}
	if _, ok := c["bonds"]; !ok {
		t.Fatal("bonds capability missing")
	}
	// A broker without the ops reports them unavailable rather than broken pages.
	old := newFake()
	h2 := harnessWith(t, old, networkScopes)
	c2 := caps(t, h2)
	if c2["ports"].Status != CapUnavailable || c2["firewall"].Status != CapUnavailable {
		t.Fatalf("old broker: %+v %+v", c2["ports"], c2["firewall"])
	}
	// Without the scope the capability is forbidden, not hidden.
	h3 := harnessWith(t, networkFake(), []string{auth.ScopeSystemRead})
	if c3 := caps(t, h3); c3["firewall"].Status != CapForbidden {
		t.Fatalf("no scope: %+v", c3["firewall"])
	}
}
