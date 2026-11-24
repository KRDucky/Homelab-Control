// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"homelab-control/backend/internal/aptmirror"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/hostinfo"
)

const testToken = "hcp1.abc." + "0123456789012345678901234567890123456789012"

func jsonHeaders(h *harness) map[string]string {
	m := packagesHeaders(h)
	m["Content-Type"] = "application/json"
	return m
}

func aptFake() *fakeBroker {
	fb := packagesFake()
	fb.results["packages.aptconfig.read"] = map[string]any{"managed": map[string]any{}, "managed_present": false, "managed_changed_by_hand": false, "path": "/etc/apt/apt.conf.d/60homelab-control"}
	fb.results["packages.aptconfig.plan"] = map[string]any{"action": "conf.write", "risk": "reversible", "token": testToken, "file": "/etc/apt/apt.conf.d/60homelab-control", "effect": "x", "consequences": []string{}}
	fb.results["packages.aptconfig.apply"] = map[string]any{"operation_id": "op1", "action": "conf.write", "outcome": "succeeded", "message": "Written."}
	fb.results["packages.mirrors.inventory"] = map[string]any{
		"distro": map[string]any{"id": "ubuntu", "codename": "noble"},
		"files":  []map[string]any{{"path": "/etc/apt/sources.list.d/ubuntu.sources", "format": "deb822", "digest": "d"}},
		"entries": []map[string]any{
			{"file": "/etc/apt/sources.list.d/ubuntu.sources", "uri": "http://us.archive.ubuntu.com/ubuntu/", "role": "archive", "switchable": true},
			{"file": "/etc/apt/sources.list.d/ubuntu.sources", "uri": "http://security.ubuntu.com/ubuntu/", "role": "security", "switchable": false},
		},
	}
	fb.results["packages.mirror.plan"] = map[string]any{"action": "mirror.set", "risk": "reversible", "token": testToken, "file": "/etc/apt/sources.list.d/ubuntu.sources", "effect": "x", "consequences": []string{}}
	return fb
}

func TestAptConfigScopes(t *testing.T) {
	readOnly := harnessWith(t, aptFake(), []string{auth.ScopePackagesRead})
	if rr := readOnly.do("GET", "/api/v1/packages/apt-config", "", packagesHeaders(readOnly)); rr.Code != http.StatusOK {
		t.Fatalf("read: %d %s", rr.Code, rr.Body)
	}
	body := `{"tunables":{"retries":3}}`
	if rr := readOnly.do("POST", "/api/v1/packages/apt-config/plan", body, jsonHeaders(readOnly)); rr.Code != http.StatusForbidden {
		t.Fatalf("packages.read must not plan: %d", rr.Code)
	}
	if rr := readOnly.do("POST", "/api/v1/packages/mirrors/probe", `{"country":"US"}`, jsonHeaders(readOnly)); rr.Code != http.StatusForbidden {
		t.Fatalf("packages.read must not probe: %d", rr.Code)
	}
	cfg := harnessWith(t, aptFake(), []string{auth.ScopePackagesConfigure})
	if rr := cfg.do("POST", "/api/v1/packages/apt-config/plan", body, map[string]string{"Origin": testOrigin, "Content-Type": "application/json"}); rr.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF: %d", rr.Code)
	}
	if rr := cfg.do("GET", "/api/v1/packages/apt-config", "", packagesHeaders(cfg)); rr.Code != http.StatusForbidden {
		t.Fatalf("packages.configure does not imply packages.read: %d", rr.Code)
	}
}

func TestAptConfigPlanApplyAuditsWithoutProxy(t *testing.T) {
	h := harnessWith(t, aptFake(), []string{auth.ScopePackagesConfigure})
	body := `{"tunables":{"http_proxy":"http://apt-cache.lan:3142","retries":3}}`
	if rr := h.do("POST", "/api/v1/packages/apt-config/plan", body, jsonHeaders(h)); rr.Code != http.StatusOK {
		t.Fatalf("plan: %d %s", rr.Code, rr.Body)
	}
	for _, bad := range []string{`{"tunables":{"bogus":1}}`, `{"tunables":{},"token":"` + testToken + `"}`} {
		if rr := h.do("POST", "/api/v1/packages/apt-config/plan", bad, jsonHeaders(h)); rr.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, rr.Code)
		}
	}
	if rr := h.do("POST", "/api/v1/packages/apt-config/apply", body, jsonHeaders(h)); rr.Code != http.StatusBadRequest {
		t.Fatalf("apply without token: %d", rr.Code)
	}
	apply := `{"tunables":{"http_proxy":"http://apt-cache.lan:3142","retries":3},"token":"` + testToken + `"}`
	if rr := h.do("POST", "/api/v1/packages/apt-config/apply", apply, jsonHeaders(h)); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "succeeded") {
		t.Fatalf("apply: %d %s", rr.Code, rr.Body)
	}
	a := h.audit.String()
	if !strings.Contains(a, "packages.aptconfig.apply") || strings.Contains(a, "apt-cache.lan") || strings.Contains(a, testToken) {
		t.Fatalf("audit: %s", a)
	}
}

func TestAptMirrorProbeThenPlanOnlyOfferedMirrors(t *testing.T) {
	h := harnessWith(t, aptFake(), []string{auth.ScopePackagesConfigure})
	var gotCurrent map[string]bool
	h.srv.aptMirror = &aptMirrorState{
		list: func(_ context.Context, u string) ([]byte, error) {
			if u != "http://mirrors.ubuntu.com/US.txt" {
				t.Errorf("list URL %s", u)
			}
			return []byte("http://mirror.example.edu/ubuntu/\nhttp://stale.example.org/ubuntu/\n"), nil
		},
		prober: func(_ context.Context, cs []aptmirror.Candidate, codename string, current map[string]bool) []aptmirror.Result {
			gotCurrent = current
			out := []aptmirror.Result{}
			for i, c := range cs {
				r := aptmirror.Result{URI: c.URI, Source: c.Source, Status: aptmirror.StatusOK, Rank: i + 1}
				// A stale mirror is listed but must not become plannable.
				if strings.Contains(c.URI, "stale") {
					r.Status, r.Rank = aptmirror.StatusStale, 0
				}
				out = append(out, r)
			}
			return out
		},
	}
	plan := `{"file":"/etc/apt/sources.list.d/ubuntu.sources","from":"http://us.archive.ubuntu.com/ubuntu/","to":"http://mirror.example.edu/ubuntu/"}`
	if rr := h.do("POST", "/api/v1/packages/mirrors/plan", plan, jsonHeaders(h)); rr.Code != http.StatusConflict {
		t.Fatalf("plan before any probe: %d", rr.Code)
	}
	rr := h.do("POST", "/api/v1/packages/mirrors/probe", `{"country":"US"}`, jsonHeaders(h))
	if rr.Code != http.StatusOK {
		t.Fatalf("probe: %d %s", rr.Code, rr.Body)
	}
	var resp mirrorProbeResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Codename != "noble" || len(resp.Results) != 3 || resp.Results[0].Source != aptmirror.SourceCurrent {
		t.Fatalf("probe response %+v", resp)
	}
	stale := strings.Replace(plan, "mirror.example.edu", "stale.example.org", 1)
	if rr := h.do("POST", "/api/v1/packages/mirrors/plan", stale, jsonHeaders(h)); rr.Code != http.StatusConflict {
		t.Fatalf("plan with a stale probed mirror: %d", rr.Code)
	}
	if !gotCurrent["us.archive.ubuntu.com"] || gotCurrent["security.ubuntu.com"] {
		t.Fatalf("only the switchable current mirror may be private-exempt: %v", gotCurrent)
	}
	if rr := h.do("POST", "/api/v1/packages/mirrors/plan", plan, jsonHeaders(h)); rr.Code != http.StatusOK {
		t.Fatalf("plan with an offered mirror: %d %s", rr.Code, rr.Body)
	}
	other := strings.Replace(plan, "mirror.example.edu", "evil.example", 1)
	if rr := h.do("POST", "/api/v1/packages/mirrors/plan", other, jsonHeaders(h)); rr.Code != http.StatusConflict {
		t.Fatalf("plan with a mirror nobody offered: %d", rr.Code)
	}
	for _, bad := range []string{
		`{"file":"/etc/shadow","from":"http://a.example/","to":"http://mirror.example.edu/ubuntu/"}`,
		`{"file":"/etc/apt/sources.list.d/ubuntu.sources","from":"x","to":"http://u:p@mirror.example.edu/ubuntu/"}`,
	} {
		if rr := h.do("POST", "/api/v1/packages/mirrors/plan", bad, jsonHeaders(h)); rr.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, rr.Code)
		}
	}
	if rr := h.do("POST", "/api/v1/packages/mirrors/probe", `{"country":"usa"}`, jsonHeaders(h)); rr.Code != http.StatusBadRequest {
		t.Fatalf("bad country: %d", rr.Code)
	}
}

func TestDefaultCountryFromTimeZone(t *testing.T) {
	root := t.TempDir()
	zi := root + "/usr/share/zoneinfo"
	if err := os.MkdirAll(zi+"/Europe", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(zi+"/Europe/Berlin", []byte("TZif"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Fixture: zone.tab lines (test data only).
	if err := os.WriteFile(zi+"/zone.tab", []byte("# x\nDE\t+5230+01322\tEurope/Berlin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root+"/etc", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../usr/share/zoneinfo/Europe/Berlin", root+"/etc/localtime"); err != nil {
		t.Fatal(err)
	}
	s := &Server{collector: &hostinfo.Collector{Roots: hostinfo.Roots{Etc: root + "/etc"}}}
	if got := s.defaultCountry(); got != "DE" {
		t.Fatalf("country = %q", got)
	}
	if got := (&Server{}).defaultCountry(); got != "" {
		t.Fatalf("no collector: %q", got)
	}
}
