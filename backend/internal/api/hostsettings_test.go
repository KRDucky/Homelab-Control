// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/hostinfo"
)

// hostSettingsHarness logs in "full" with scopes, and points the collector at
// a private copy of the labelled synthetic fixture testdata/hostsettings.
func hostSettingsHarness(t *testing.T, scopes []string) (*harness, string) {
	t.Helper()
	h := harnessWith(t, nil, scopes)
	root := filepath.Join(t.TempDir(), "host")
	if err := os.CopyFS(root, os.DirFS("../hostinfo/testdata/hostsettings")); err != nil {
		t.Fatal(err)
	}
	h.srv.collector = &hostinfo.Collector{Roots: hostinfo.Roots{Proc: root + "/proc", Sys: root + "/sys", Etc: root + "/etc"}}
	return h, root
}

func previewHeaders(h *harness) map[string]string {
	return map[string]string{"Content-Type": "application/json", "Origin": testOrigin, "X-CSRF-Token": h.csrf}
}

// treeDigest hashes every path, mode, link target and file content beneath root.
func treeDigest(t *testing.T, root string) string {
	t.Helper()
	sum := sha256.New()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		sum.Write([]byte(rel + "\x00" + info.Mode().String() + "\x00" + info.ModTime().String() + "\x00"))
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			l, _ := os.Readlink(p)
			sum.Write([]byte(l))
		case info.Mode().IsRegular():
			b, _ := os.ReadFile(p)
			sum.Write(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func TestHostSettingsReadRequiresSessionAndScope(t *testing.T) {
	h := newHarness(t)
	if rr := h.do("GET", "/api/v1/host/settings", "", nil); rr.Code != http.StatusUnauthorized {
		t.Fatal(rr.Code)
	}
	if rr := h.login("noscope", "noscope-password-123"); rr.Code != 200 {
		t.Fatal(rr.Code)
	}
	if rr := h.do("GET", "/api/v1/host/settings", "", nil); rr.Code != http.StatusForbidden {
		t.Fatal(rr.Code)
	}
}

func TestHostSettingsReadReturnsObservations(t *testing.T) {
	h, _ := hostSettingsHarness(t, []string{auth.ScopeSystemRead})
	rr := h.do("GET", "/api/v1/host/settings", "", nil)
	if rr.Code != http.StatusOK {
		t.Fatal(rr.Code, rr.Body.String())
	}
	var out hostSettingsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	s := out.Settings
	if !out.ReadOnly || out.CollectedAt.IsZero() || s.StaticHostname.Value != "fixture-static" || s.PrettyHostname.Value != "Fixture Lab (example)" ||
		s.Timezone.Value != "Europe/Berlin" || s.KernelHostname.Value != "fixture-kernel-name" {
		t.Fatalf("%+v", out)
	}
}

func TestHostSettingsUnavailableWithoutCollector(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeSystemRead, auth.ScopeHostSettingsPreview})
	h.srv.collector = nil
	if rr := h.do("GET", "/api/v1/host/settings", "", nil); rr.Code != http.StatusServiceUnavailable || !strings.Contains(rr.Body.String(), "host_settings_unavailable") {
		t.Fatal(rr.Code, rr.Body.String())
	}
	if rr := h.do("POST", "/api/v1/host/settings/preview", `{"timezone":"Etc/UTC"}`, previewHeaders(h)); rr.Code != http.StatusServiceUnavailable {
		t.Fatal(rr.Code, rr.Body.String())
	}
	if c := caps(t, h)["host_settings"]; c.Status != CapUnavailable {
		t.Fatalf("%+v", c)
	}
}

func TestHostSettingsCapability(t *testing.T) {
	h, _ := hostSettingsHarness(t, []string{auth.ScopeSystemRead})
	c := caps(t, h)["host_settings"]
	if c.Path != "/host-settings" || c.Scope != auth.ScopeSystemRead || c.Status != CapDegraded || !strings.Contains(c.Reason, "cannot apply") {
		t.Fatalf("%+v", c)
	}
}

func TestHostSettingsPreviewScopeIsSeparateAndNotDefault(t *testing.T) {
	if strings.Contains(auth.DefaultScopes, auth.ScopeHostSettingsPreview) || !auth.KnownScopes[auth.ScopeHostSettingsPreview] {
		t.Fatal("host.settings.preview must be known and never granted by default")
	}
	h, _ := hostSettingsHarness(t, []string{auth.ScopeSystemRead})
	before := h.audit.Len()
	rr := h.do("POST", "/api/v1/host/settings/preview", `{"timezone":"Etc/UTC"}`, previewHeaders(h))
	if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "host.settings.preview") {
		t.Fatal(rr.Code, rr.Body.String())
	}
	if strings.Contains(h.audit.String()[before:], "fields=") {
		t.Fatal("refused preview was processed")
	}
}

func TestHostSettingsPreviewRequiresSessionOriginAndCSRF(t *testing.T) {
	h, _ := hostSettingsHarness(t, []string{auth.ScopeSystemRead, auth.ScopeHostSettingsPreview})
	body := `{"static_hostname":"nas-01"}`
	for name, hdr := range map[string]map[string]string{
		"no origin":   {"Content-Type": "application/json", "X-CSRF-Token": h.csrf},
		"cross-site":  {"Content-Type": "application/json", "X-CSRF-Token": h.csrf, "Origin": "https://evil.example"},
		"no token":    {"Content-Type": "application/json", "Origin": testOrigin},
		"wrong token": {"Content-Type": "application/json", "Origin": testOrigin, "X-CSRF-Token": "not-the-token"},
	} {
		if rr := h.do("POST", "/api/v1/host/settings/preview", body, hdr); rr.Code != http.StatusForbidden {
			t.Errorf("%s: %d %s", name, rr.Code, rr.Body.String())
		}
	}
	cookie := h.cookie
	h.cookie = nil
	if rr := h.do("POST", "/api/v1/host/settings/preview", body, previewHeaders(h)); rr.Code != http.StatusUnauthorized {
		t.Errorf("no session: %d", rr.Code)
	}
	h.cookie = cookie
	if rr := h.do("POST", "/api/v1/host/settings/preview", body, previewHeaders(h)); rr.Code != http.StatusOK {
		t.Fatal(rr.Code, rr.Body.String())
	}
}

// The preview reads the host and compares; it must not change anything,
// must say so, and there is no apply route.
func TestHostSettingsPreviewDoesNotExecute(t *testing.T) {
	h, root := hostSettingsHarness(t, []string{auth.ScopeSystemRead, auth.ScopeHostSettingsPreview})
	before := treeDigest(t, root)
	rr := h.do("POST", "/api/v1/host/settings/preview",
		`{"static_hostname":"nas-01","pretty_hostname":"Ian's NAS","timezone":"Etc/UTC"}`, previewHeaders(h))
	if rr.Code != http.StatusOK {
		t.Fatal(rr.Code, rr.Body.String())
	}
	if after := treeDigest(t, root); after != before {
		t.Fatal("preview changed the host fixture")
	}
	var out hostSettingsPreviewResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.PreviewOnly || out.Executed || out.HostChanged || out.ApplyAvailable || !out.Valid || !strings.Contains(out.Notice, "Nothing was changed") {
		t.Fatalf("%+v", out)
	}
	want := map[string][2]string{
		"static_hostname": {"fixture-static", hostSettingsWould},
		"pretty_hostname": {"Fixture Lab (example)", hostSettingsWould},
		"timezone":        {"Europe/Berlin", hostSettingsWould},
	}
	if len(out.Changes) != 3 {
		t.Fatalf("%+v", out.Changes)
	}
	for _, c := range out.Changes {
		if w := want[c.Field]; c.Current != w[0] || c.Outcome != w[1] {
			t.Errorf("%+v", c)
		}
	}
	// The response is a snapshot: the current value after the preview is unchanged.
	if rr := h.do("GET", "/api/v1/host/settings", "", nil); !strings.Contains(rr.Body.String(), `"fixture-static"`) {
		t.Fatal(rr.Body.String())
	}
	// Preview scope never authorizes plan or apply (403, not 404).
	for _, p := range []string{"/api/v1/host/settings/apply", "/api/v1/host/settings/plan"} {
		if rr := h.do("POST", p, `{"static_hostname":"nas-01"}`, previewHeaders(h)); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "host.settings.apply") {
			t.Errorf("%s: %d %s", p, rr.Code, rr.Body)
		}
	}
	// Audited with field names only, never the requested values.
	a := h.audit.String()
	if !strings.Contains(a, "host.settings.preview") || !strings.Contains(a, "fields=static_hostname,pretty_hostname,timezone") || strings.Contains(a, "nas-01") || strings.Contains(a, "Ian's NAS") {
		t.Fatal(a)
	}
}

const hostSettingsWould = PreviewWouldChange

func TestHostSettingsPreviewValidation(t *testing.T) {
	h, _ := hostSettingsHarness(t, []string{auth.ScopeSystemRead, auth.ScopeHostSettingsPreview})
	post := func(body string) (*hostSettingsPreviewResponse, int, string) {
		rr := h.do("POST", "/api/v1/host/settings/preview", body, previewHeaders(h))
		var out hostSettingsPreviewResponse
		_ = json.Unmarshal(rr.Body.Bytes(), &out)
		return &out, rr.Code, rr.Body.String()
	}
	for _, body := range []string{`{}`, `{"hostname":"x"}`, `{"timezone":"Etc/UTC"} {}`, `not json`} {
		if _, code, b := post(body); code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", body, code, b)
		}
	}
	hdr := previewHeaders(h)
	hdr["Content-Type"] = "text/plain"
	if rr := h.do("POST", "/api/v1/host/settings/preview", `{"timezone":"Etc/UTC"}`, hdr); rr.Code != http.StatusUnsupportedMediaType {
		t.Errorf("content type: %d", rr.Code)
	}
	for body, field := range map[string]string{
		`{"static_hostname":"nas;reboot"}`:              "static_hostname",
		`{"static_hostname":"NAS"}`:                     "static_hostname",
		`{"pretty_hostname":"a\nb"}`:                    "pretty_hostname",
		`{"timezone":"../../etc/hostname"}`:             "timezone",
		`{"timezone":"Europe/Nowhere"}`:                 "timezone",
		`{"timezone":"zone.tab"}`:                       "timezone",
		`{"timezone":"$(touch /tmp/hc-pwned)"}`:         "timezone",
		`{"static_hostname":"-x","timezone":"Etc/UTC"}`: "static_hostname",
	} {
		out, code, b := post(body)
		if code != http.StatusOK || out.Valid || out.Executed || out.HostChanged {
			t.Errorf("%s: %d %s", body, code, b)
			continue
		}
		for _, c := range out.Changes {
			if c.Field == field && (c.Outcome != PreviewInvalid || c.Reason == "") {
				t.Errorf("%s: %+v", body, c)
			}
		}
	}
	// Same value: unchanged. Empty pretty name: removal would change it.
	out, _, _ := post(`{"static_hostname":"fixture-static","pretty_hostname":""}`)
	if !out.Valid || out.Changes[0].Outcome != PreviewUnchanged || out.Changes[1].Outcome != PreviewWouldChange {
		t.Fatalf("%+v", out.Changes)
	}
	if out, _, _ := post(`{"static_hostname":"localhost"}`); !out.Valid || len(out.Warnings) != 1 {
		t.Fatalf("%+v", out)
	}
}

// When the current value cannot be read the preview says it cannot compare,
// and when the time zone database is unreadable a name cannot be confirmed.
func TestHostSettingsPreviewUnavailableInputs(t *testing.T) {
	h, root := hostSettingsHarness(t, []string{auth.ScopeSystemRead, auth.ScopeHostSettingsPreview})
	if err := os.Remove(filepath.Join(root, "etc/hostname")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "etc/hostname"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "usr/share/zoneinfo")); err != nil {
		t.Fatal(err)
	}
	rr := h.do("POST", "/api/v1/host/settings/preview", `{"static_hostname":"nas-01","timezone":"Etc/UTC"}`, previewHeaders(h))
	var out hostSettingsPreviewResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil || rr.Code != http.StatusOK {
		t.Fatal(rr.Code, rr.Body.String())
	}
	if out.Valid || out.Changes[0].Outcome != PreviewUnknown || out.Changes[1].Outcome != PreviewInvalid || !strings.Contains(out.Changes[1].Reason, "could not be read") {
		t.Fatalf("%+v", out)
	}
}

func hostSettingsApplyFake() *fakeBroker {
	fb := newFake()
	fb.results["broker.info"] = map[string]any{"version": "t", "systemd_booted": true, "writes_available": true,
		"exec_available": []string{"systemctl", "hostnamectl", "timedatectl"}, "ops": []map[string]string{
			{"name": "broker.info"}, {"name": "host.settings.plan"}, {"name": "host.settings.apply"}}}
	fb.results["host.settings.plan"] = map[string]any{"action": "set", "risk": "reversible", "step_up_required": false,
		"confirmation_required": true, "token": validToken, "expires_at": time.Now().Add(2 * time.Minute),
		"observed": map[string]any{"static_hostname": "fixture-static", "static_status": "observed",
			"pretty_status": "observed", "timezone_status": "observed",
			"notes": []string{"This result reports hostnamectl and timedatectl."}},
		"effect": "This would set the static host name.", "consequences": []string{"Confirmation is required."},
		"warnings": []string{"Changing the static host name can invalidate TLS certificates that name this host, mDNS/Avahi names, and entries in /etc/hosts. Homelab Control does not update those."},
		"changes":  []map[string]any{{"field": "static_hostname", "current": "fixture-static", "requested": "nas-01", "outcome": "would-change"}},
		"fields":   []string{"static_hostname"}}
	fb.results["host.settings.apply"] = map[string]any{"operation_id": "op-hs-1", "action": "set", "outcome": "succeeded",
		"message": "hostnamectl/timedatectl report the requested values.", "fields": []string{"static_hostname"},
		"started_at": time.Now(), "finished_at": time.Now()}
	return fb
}

var applyScopes = []string{auth.ScopeSystemRead, auth.ScopeHostSettingsPreview, auth.ScopeHostSettingsApply}

func TestHostSettingsApplyScopeIsSeparateAndNotDefault(t *testing.T) {
	if strings.Contains(auth.DefaultScopes, auth.ScopeHostSettingsApply) || !auth.KnownScopes[auth.ScopeHostSettingsApply] {
		t.Fatal("host.settings.apply must be known and never granted by default")
	}
	fb := hostSettingsApplyFake()
	h, _ := hostSettingsHarness(t, []string{auth.ScopeSystemRead, auth.ScopeHostSettingsPreview})
	h.srv.WithBroker(fb)
	before := h.audit.Len()
	for _, p := range []string{"/api/v1/host/settings/plan", "/api/v1/host/settings/apply"} {
		if rr := h.do("POST", p, `{"static_hostname":"nas-01","token":"`+validToken+`"}`, previewHeaders(h)); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "host.settings.apply") {
			t.Errorf("%s: %d %s", p, rr.Code, rr.Body)
		}
	}
	if len(fb.args) != 0 {
		t.Fatalf("broker called without apply scope: %v", fb.args)
	}
	if strings.Contains(h.audit.String()[before:], "fields=") {
		t.Fatal("refused plan/apply was processed")
	}
}

func TestHostSettingsPlanApplyRequireOriginAndCSRF(t *testing.T) {
	fb := hostSettingsApplyFake()
	h, _ := hostSettingsHarness(t, applyScopes)
	h.srv.WithBroker(fb)
	body := `{"static_hostname":"nas-01","token":"` + validToken + `"}`
	for _, p := range []string{"/api/v1/host/settings/plan", "/api/v1/host/settings/apply"} {
		for name, hdr := range map[string]map[string]string{
			"no origin":   {"Content-Type": "application/json", "X-CSRF-Token": h.csrf},
			"cross-site":  {"Content-Type": "application/json", "X-CSRF-Token": h.csrf, "Origin": "https://evil.example"},
			"no token":    {"Content-Type": "application/json", "Origin": testOrigin},
			"wrong token": {"Content-Type": "application/json", "Origin": testOrigin, "X-CSRF-Token": "not-the-token"},
		} {
			if rr := h.do("POST", p, body, hdr); rr.Code != http.StatusForbidden {
				t.Errorf("%s %s: %d %s", p, name, rr.Code, rr.Body)
			}
		}
	}
	if len(fb.args) != 0 {
		t.Fatal("broker called")
	}
}

func TestHostSettingsApplyAvailableAndPlanApply(t *testing.T) {
	fb := hostSettingsApplyFake()
	h, root := hostSettingsHarness(t, applyScopes)
	h.srv.WithBroker(fb)
	c := caps(t, h)["host_settings"]
	if c.Status != CapAvailable || c.Reason != "" {
		t.Fatalf("capability: %+v", c)
	}
	before := treeDigest(t, root)
	rr := h.do("POST", "/api/v1/host/settings/preview", `{"static_hostname":"nas-01"}`, previewHeaders(h))
	var preview hostSettingsPreviewResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &preview); err != nil || rr.Code != http.StatusOK || !preview.ApplyAvailable || preview.Executed || preview.HostChanged {
		t.Fatal(rr.Code, rr.Body.String())
	}
	if after := treeDigest(t, root); after != before {
		t.Fatal("preview changed the host fixture")
	}
	pr := h.do("POST", "/api/v1/host/settings/plan", `{"static_hostname":"nas-01"}`, previewHeaders(h))
	if pr.Code != http.StatusOK || !strings.Contains(pr.Body.String(), `"risk":"reversible"`) || !strings.Contains(pr.Body.String(), "TLS") {
		t.Fatalf("plan: %d %s", pr.Code, pr.Body)
	}
	ar := h.do("POST", "/api/v1/host/settings/apply", `{"static_hostname":"nas-01","token":"`+validToken+`"}`, previewHeaders(h))
	if ar.Code != http.StatusOK || !strings.Contains(ar.Body.String(), `"outcome":"succeeded"`) {
		t.Fatalf("apply: %d %s", ar.Code, ar.Body)
	}
	if after := treeDigest(t, root); after != before {
		t.Fatal("API apply must not write the hc-api fixture; the broker fake is the only executor")
	}
	a := h.audit.String()
	if strings.Contains(a, "nas-01") || strings.Contains(a, validToken) || strings.Contains(a, "hcp1.") {
		t.Fatalf("audit leaked values or token: %s", a)
	}
	if !strings.Contains(a, "host.settings.plan") || !strings.Contains(a, "host.settings.apply") || !strings.Contains(a, "fields=static_hostname") {
		t.Fatalf("audit missing events: %s", a)
	}
}

func TestHostSettingsApplyValidatesBeforeBroker(t *testing.T) {
	fb := hostSettingsApplyFake()
	h, _ := hostSettingsHarness(t, applyScopes)
	h.srv.WithBroker(fb)
	for _, body := range []string{`{}`, `{"hostname":"x"}`, `{"token":"` + validToken + `"}`} {
		if rr := h.do("POST", "/api/v1/host/settings/plan", body, previewHeaders(h)); rr.Code != http.StatusBadRequest {
			t.Errorf("plan %s: %d %s", body, rr.Code, rr.Body)
		}
		if rr := h.do("POST", "/api/v1/host/settings/apply", body, previewHeaders(h)); rr.Code != http.StatusBadRequest {
			t.Errorf("apply %s: %d %s", body, rr.Code, rr.Body)
		}
	}
	for _, tok := range []string{"", "x", "hcp1.a.b", strings.Repeat("A", 3000)} {
		b := `{"static_hostname":"nas-01","token":"` + tok + `"}`
		if rr := h.do("POST", "/api/v1/host/settings/apply", b, previewHeaders(h)); rr.Code != http.StatusBadRequest {
			t.Errorf("token %.20q: %d", tok, rr.Code)
		}
	}
	if len(fb.args) != 0 {
		t.Fatalf("broker reached with invalid input: %v", fb.args)
	}
}

func TestHostSettingsOtherWriteScopesDoNotAuthorizeApply(t *testing.T) {
	fb := hostSettingsApplyFake()
	h, _ := hostSettingsHarness(t, []string{auth.ScopeSystemRead, auth.ScopeServicesOperate, auth.ScopeServicesConfigure, auth.ScopeLayoutManage})
	h.srv.WithBroker(fb)
	if rr := h.do("POST", "/api/v1/host/settings/plan", `{"static_hostname":"nas-01"}`, previewHeaders(h)); rr.Code != http.StatusForbidden {
		t.Fatal(rr.Code, rr.Body.String())
	}
	if len(fb.args) != 0 {
		t.Fatal("broker called")
	}
}
