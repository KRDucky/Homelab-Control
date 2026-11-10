// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package api

import (
	"encoding/json"
	"strings"
	"testing"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/containeridentity"
)

const envRuntime = `{"supportedPlatforms":["docker","podman","kubernetes"],"deploymentMethods":["compose","kubernetes-manifest"],"identity":{"configurable":true,"method":"env","uidVariable":"PUID","gidVariable":"PGID","umask":{"method":"env","variable":"UMASK"},"supplementalGroups":true},"privileged":false,"capabilities":[],"volumes":[{"name":"config","target":"/config","access":"rw"}]}`
const userRuntime = `{"supportedPlatforms":["docker","podman","kubernetes"],"deploymentMethods":["compose","kubernetes-manifest"],"identity":{"configurable":true,"method":"runtime-user","umask":{"method":"none"},"supplementalGroups":true},"privileged":false,"capabilities":[],"volumes":[]}`

type identityOut struct {
	Preview containeridentity.Preview `json:"preview"`
}

func identityPreview(t *testing.T, h *harness, body string) (int, identityOut, string) {
	t.Helper()
	rr := h.do("POST", "/api/v1/containers/identity/preview", body, platformPreviewHeaders(h))
	var out identityOut
	if rr.Code == 200 {
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
	}
	return rr.Code, out, rr.Body.String()
}

func issueCodes(p containeridentity.Preview) string {
	var c []string
	for _, i := range p.Issues {
		c = append(c, string(i.Severity)+":"+i.Code)
	}
	return strings.Join(c, ",")
}

func TestIdentityEndpointsAuthorizationAndContract(t *testing.T) {
	all := []string{auth.ScopePlatformsRead, auth.ScopeComposeRead, auth.ScopePodmanRead, auth.ScopeKubernetesRead}
	// No registry: unavailable, not a guess.
	h := harnessWith(t, nil, all)
	if rr := h.do("GET", "/api/v1/containers/identity/profiles", "", packagesHeaders(h)); rr.Code != 503 {
		t.Fatal("no registry", rr.Code)
	}
	h.srv.WithPlatforms(testRegistry(t))
	rr := h.do("GET", "/api/v1/containers/identity/profiles", "", packagesHeaders(h))
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"media-application"`) || !strings.Contains(rr.Body.String(), `"docker"`) || !strings.Contains(h.audit.String(), "container.identity.profiles.read") {
		t.Fatal(rr.Code, rr.Body)
	}
	if rr := h.do("GET", "/api/v1/containers/identity/profiles", "", nil); rr.Code != 403 {
		t.Fatal("GET without CSRF", rr.Code)
	}
	// Docker env-method custom identity renders Compose environment only.
	code, out, raw := identityPreview(t, h, `{"platform":"docker-fixture","mode":"custom","runtime":`+envRuntime+`,"override":{"uid":3000,"gid":3001,"umask":"002","supplementalGroups":[33,33]}}`)
	p := out.Preview
	if code != 200 || p.Compose == nil || p.Compose.Environment["PUID"] != "3000" || p.Compose.Environment["UMASK"] != "002" || strings.Join(p.Compose.GroupAdd, ",") != "33" || p.Applied || p.Deployable {
		t.Fatal(code, raw)
	}
	if !strings.Contains(h.audit.String(), "platform.docker.identity.preview") {
		t.Fatal("not audited")
	}
	// Volumes are unverified: CORE never inspects host paths for previews.
	if len(p.Volumes) != 1 || p.Volumes[0].Status != containeridentity.VolumeUnverified {
		t.Fatal(p.Volumes)
	}
	// Kubernetes: security contexts, never Compose.
	code, out, raw = identityPreview(t, h, `{"platform":"k3s-fixture","mode":"custom","runtime":`+userRuntime+`,"override":{"uid":3000,"gid":3000,"supplementalGroups":[44]}}`)
	if code != 200 || out.Preview.Compose != nil || out.Preview.Kubernetes == nil || out.Preview.Kubernetes.PodSecurityContext.SupplementalGroups[0] != 44 {
		t.Fatal(code, raw)
	}
	// No secrets or host details leak into responses.
	for _, leak := range []string{"fixture:docker", "/var/run", "docker.sock", "must-not-leak", "DB_PASSWORD"} {
		if strings.Contains(raw, leak) {
			t.Fatal("leaked", leak)
		}
	}
}

func TestIdentityPreviewRejectsAndScopes(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopePlatformsRead, auth.ScopeComposeRead})
	h.srv.WithPlatforms(testRegistry(t))
	base := `{"platform":"docker-fixture","mode":"custom","runtime":` + envRuntime + `,"override":{"uid":3000,"gid":3000}}`
	for name, tc := range map[string]struct {
		body string
		code int
	}{
		"k3s without kubernetes.read": {strings.Replace(base, "docker-fixture", "k3s-fixture", 1), 404},
		"unknown platform":            {strings.Replace(base, "docker-fixture", "nope", 1), 404},
		"client host metadata":        {strings.Replace(base, `"mode"`, `"volumes":[{"name":"x","host":{"ownerUid":0}}],"mode"`, 1), 400},
		"client observed identity":    {strings.Replace(base, `"mode"`, `"observed":{"uid":0},"mode"`, 1), 400},
		"client platform context":     {strings.Replace(base, `"mode"`, `"uidMap":[],"mode"`, 1), 400},
		"endpoint injection":          {strings.Replace(base, `"mode"`, `"endpoint":"unix:///var/run/docker.sock","mode"`, 1), 400},
		"invalid runtime":             {strings.Replace(base, `"method":"env"`, `"method":"magic"`, 1), 400},
		"invalid umask":               {strings.Replace(base, `"gid":3000`, `"gid":3000,"umask":"0999"`, 1), 400},
		"negative uid":                {strings.Replace(base, `"uid":3000`, `"uid":-1`, 1), 400},
		"unknown workload":            {strings.Replace(base, `"mode"`, `"workload":"docker-fixture/container/-/nope/nope","mode"`, 1), 400},
		"trailing":                    {base + `{}`, 400},
	} {
		if code, _, raw := identityPreview(t, h, tc.body); code != tc.code {
			t.Fatal(name, code, raw)
		}
	}
	noCSRF := platformPreviewHeaders(h)
	delete(noCSRF, "X-CSRF-Token")
	if rr := h.do("POST", "/api/v1/containers/identity/preview", base, noCSRF); rr.Code != 403 {
		t.Fatal("POST without CSRF", rr.Code)
	}
	// Discovery scope alone never reaches management: no apply routes exist.
	for _, path := range []string{"/api/v1/containers/identity/apply", "/api/v1/containers/identity", "/api/v1/platforms/docker-fixture/identity"} {
		if rr := h.do("POST", path, base, platformPreviewHeaders(h)); rr.Code != 404 && rr.Code != 405 {
			t.Fatal(path, rr.Code)
		}
	}
	// Root identity, unsupported platform and privileged runtime are blocked, not rendered.
	_, out, _ := identityPreview(t, h, strings.Replace(base, `"uid":3000`, `"uid":0`, 1))
	if !strings.Contains(issueCodes(out.Preview), "blocker:root_identity") || (out.Preview.Compose != nil && out.Preview.Compose.Environment["PUID"] != "") {
		t.Fatal(issueCodes(out.Preview))
	}
	_, out, _ = identityPreview(t, h, strings.Replace(base, `"supportedPlatforms":["docker","podman","kubernetes"],"deploymentMethods":["compose","kubernetes-manifest"]`, `"supportedPlatforms":["kubernetes"],"deploymentMethods":["kubernetes-manifest"]`, 1))
	if !strings.Contains(issueCodes(out.Preview), "blocker:platform_not_supported_by_plugin") {
		t.Fatal(issueCodes(out.Preview))
	}
	_, out, _ = identityPreview(t, h, strings.Replace(base, `"privileged":false`, `"privileged":true`, 1))
	if !strings.Contains(issueCodes(out.Preview), "blocker:privileged_runtime") {
		t.Fatal(issueCodes(out.Preview))
	}
}

func TestIdentityPreviewWithAdministratorConfigAndObservation(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopePlatformsRead, auth.ScopeComposeRead, auth.ScopeKubernetesRead})
	cfg, err := containeridentity.ParseConfig([]byte(`{"schemaVersion":1,"platforms":{
	  "docker-fixture":{"defaults":{"uid":2500,"gid":2500,"umask":"022"},"mappingKnown":true,
	    "uidMap":[{"containerId":0,"hostId":2001,"size":1},{"containerId":1,"hostId":300000,"size":65536}],
	    "gidMap":[{"containerId":0,"hostId":2001,"size":1},{"containerId":1,"hostId":300000,"size":65536}],
	    "groups":[{"name":"docker","gid":970},{"name":"mediashare","gid":4100}],"roles":{"media":4100}},
	  "k3s-fixture":{"defaults":{},"mappingKnown":false,"uidMap":[],"gidMap":[],"groups":[],"roles":{}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	h.srv.WithPlatforms(testRegistry(t)).WithContainerIdentity(cfg)
	_, out, raw := identityPreview(t, h, `{"platform":"docker-fixture","mode":"inherit","profile":"media-application","runtime":`+envRuntime+`}`)
	p := out.Preview
	if p.Resolved.UID.Source != containeridentity.LayerPlatform || *p.Resolved.UID.Host != 302499 || p.Resolved.Umask.Source != containeridentity.LayerProfile || p.Resolved.SupplementalGroups.Value[0] != 4100 {
		t.Fatal(raw)
	}
	// Privileged group from the platform's group table blocks without acknowledgement.
	_, out, _ = identityPreview(t, h, `{"platform":"docker-fixture","mode":"custom","runtime":`+envRuntime+`,"override":{"supplementalGroups":[970]}}`)
	if !strings.Contains(issueCodes(out.Preview), "blocker:privileged_group") || (out.Preview.Compose != nil && out.Preview.Compose.GroupAdd != nil && len(out.Preview.Compose.GroupAdd) > 0) {
		t.Fatal(issueCodes(out.Preview), out.Preview.Compose)
	}
	// Observed identity comes from the adapter (K3s pod template), not the client.
	inv := h.do("GET", "/api/v1/platforms/k3s-fixture/inventory", "", packagesHeaders(h)).Body.String()
	i := strings.Index(inv, `"name":"gallery","uid":"`)
	if i < 0 {
		t.Fatal("gallery missing")
	}
	uid := inv[i+len(`"name":"gallery","uid":"`):]
	uid = uid[:strings.Index(uid, `"`)]
	key := "k3s-fixture/Deployment/demo/gallery/" + uid
	body := `{"platform":"k3s-fixture","mode":"custom","workload":"` + key + `","runtime":` + userRuntime + `,"override":{"uid":3100,"gid":3000,"supplementalGroups":[44,5000]}}`
	if _, out, raw = identityPreview(t, h, body); out.Preview.Verification != containeridentity.VerifyMatch {
		t.Fatal(raw)
	}
	if _, out, _ = identityPreview(t, h, strings.Replace(body, `"uid":3100`, `"uid":3200`, 1)); out.Preview.Verification != containeridentity.VerifyMismatch {
		t.Fatal(out.Preview.Verification)
	}
	// Invalid administrator configuration is rejected at load time.
	for _, bad := range []string{`{"schemaVersion":1,"platforms":{"docker-fixture":{"defaults":{"uid":0}}}}`, `{"schemaVersion":1,"platforms":{"Bad":{}}}`, `{"schemaVersion":1,"platforms":{"x1":{"uidMap":[{"containerId":0,"hostId":1,"size":1}]}}}`, `{"schemaVersion":2,"platforms":{}}`, `{"schemaVersion":1,"platforms":{},"extra":1}`} {
		if _, err := containeridentity.ParseConfig([]byte(bad)); err == nil {
			t.Fatal("accepted", bad)
		}
	}
}
