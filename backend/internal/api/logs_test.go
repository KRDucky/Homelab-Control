// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/broker/proto"
)

func logsFake() *fakeBroker {
	fb := newFake()
	info := fb.results["broker.info"].(map[string]any)
	info["exec_available"] = []string{"systemctl", "journalctl"}
	info["ops"] = append(info["ops"].([]map[string]string), map[string]string{"name": "services.unit.logs"})
	fb.results["services.unit.logs"] = map[string]any{"unit": "ssh.service", "requested": 50, "truncated": false, "source": "t",
		"entries": []map[string]any{{"message": "Accepted publickey for alice", "priority": 6}}}
	return fb
}

func TestUnitLogsRequiresScope(t *testing.T) {
	h := harnessWith(t, logsFake(), allScopes) // no services.logs
	if rr := h.do("GET", "/api/v1/services/units/ssh.service/logs", "", nil); rr.Code != http.StatusForbidden {
		t.Fatalf("without services.logs: %d", rr.Code)
	}
	if c := caps(t, h); c["service_logs"].Status != CapForbidden {
		t.Fatalf("capability: %+v", c["service_logs"])
	}
	h.cookie = nil
	if rr := h.do("GET", "/api/v1/services/units/ssh.service/logs", "", nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("without session: %d", rr.Code)
	}
}

func TestUnitLogsValidatesBeforeBroker(t *testing.T) {
	fb := logsFake()
	h := harnessWith(t, fb, append(allScopes, auth.ScopeServicesLogs))
	for _, p := range []string{
		"/api/v1/services/units/" + url.PathEscape("-x.service") + "/logs",
		"/api/v1/services/units/" + url.PathEscape("ssh.service;id") + "/logs",
		"/api/v1/services/units/" + url.PathEscape("a b.service") + "/logs",
		"/api/v1/services/units/foo.device/logs",
		"/api/v1/services/units/ssh/logs",
		"/api/v1/services/units/ssh.service/logs?lines=0",
		"/api/v1/services/units/ssh.service/logs?lines=501",
		"/api/v1/services/units/ssh.service/logs?lines=ten",
		"/api/v1/services/units/..%2F..%2Fetc%2Fpasswd.service/logs",
		"/api/v1/services/units/%2Fetc%2Fpasswd.service/logs",
	} {
		if rr := h.do("GET", p, "", nil); rr.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", p, rr.Code, rr.Body)
		}
	}
	if len(fb.args) != 0 {
		t.Fatalf("invalid requests reached the broker: %v", fb.args)
	}
}

func TestUnitLogsOK(t *testing.T) {
	fb := logsFake()
	h := harnessWith(t, fb, append(allScopes, auth.ScopeServicesLogs))
	if c := caps(t, h); c["service_logs"].Status != CapAvailable {
		t.Fatalf("capability: %+v", c["service_logs"])
	}
	rr := h.do("GET", "/api/v1/services/units/ssh.service/logs?lines=50", "", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "Accepted publickey") {
		t.Fatalf("%d %s", rr.Code, rr.Body)
	}
	b, _ := json.Marshal(fb.args[len(fb.args)-1])
	if string(b) != `{"lines":50,"unit":"ssh.service"}` {
		t.Fatalf("args sent to broker: %s", b)
	}
	audit := h.audit.String()
	if !strings.Contains(audit, `"event":"services.logs.read"`) || !strings.Contains(audit, "ssh.service lines=50") ||
		strings.Contains(audit, "Accepted publickey") {
		t.Fatalf("audit must record the read but not the content: %s", audit)
	}
	// Default line count.
	_ = h.do("GET", "/api/v1/services/units/ssh.service/logs", "", nil)
	if b, _ := json.Marshal(fb.args[len(fb.args)-1]); !strings.Contains(string(b), `"lines":100`) {
		t.Fatalf("default lines: %s", b)
	}
}

func TestUnitLogsBrokerErrors(t *testing.T) {
	for code, want := range map[string]int{proto.CodeNotFound: 404, proto.CodeBadRequest: 400, proto.CodeUnavailable: 503,
		proto.CodeForbidden: 403, proto.CodeTimeout: 504, proto.CodeInternal: 502} {
		fb := logsFake()
		fb.errs["services.unit.logs"] = &proto.Error{Code: code, Message: "m"}
		h := harnessWith(t, fb, append(allScopes, auth.ScopeServicesLogs))
		if rr := h.do("GET", "/api/v1/services/units/ssh.service/logs", "", nil); rr.Code != want {
			t.Errorf("%s: got %d want %d", code, rr.Code, want)
		}
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// Log content is withheld when the read cannot be audited.
func TestUnitLogsFailClosedOnAudit(t *testing.T) {
	h := harnessWith(t, logsFake(), append(allScopes, auth.ScopeServicesLogs))
	h.srv.audit = audit.New(brokenWriter{})
	rr := h.do("GET", "/api/v1/services/units/ssh.service/logs", "", nil)
	if rr.Code != http.StatusServiceUnavailable || strings.Contains(rr.Body.String(), "Accepted publickey") {
		t.Fatalf("%d %s", rr.Code, rr.Body)
	}
}
