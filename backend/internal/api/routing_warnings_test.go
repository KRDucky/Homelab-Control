// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package api

import (
	"context"
	"encoding/json"
	"testing"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/composediscovery"
	"homelab-control/backend/internal/traefikclient"
)

// ruleLessTraefik reports the three rule-less routers seen in the live Phase 7
// test environment (file-provider fixture plus Traefik's internal ping).
type ruleLessTraefik struct{ fakeTraefik }

func (f *ruleLessTraefik) Inventory(context.Context) (traefikclient.Inventory, error) {
	f.calls++
	return traefikclient.Inventory{SchemaVersion: 1, Routers: []traefikclient.Resource{
		{Name: "fixture-router@file", Provider: "file", Status: "enabled"},
		{Name: "hc-api@file", Provider: "file", Status: "enabled"},
		{Name: "ping@internal", Provider: "internal", Status: "enabled"},
	}, Services: []traefikclient.Resource{}, Middlewares: []traefikclient.Resource{}}, nil
}

func TestPreviewWarningsAreNotRepeatedPerRouter(t *testing.T) {
	const want = "3 live routers (fixture-router@file, hc-api@file, ping@internal) lack routing rules; hostname conflicts cannot be fully verified."
	count := func(t *testing.T, body []byte) []string {
		t.Helper()
		var out struct {
			Warnings []struct{ Code, Message string } `json:"warnings"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatal(err)
		}
		var msgs []string
		for _, w := range out.Warnings {
			if w.Code == "host_unverified" {
				msgs = append(msgs, w.Message)
			}
		}
		return msgs
	}
	h := harnessWith(t, nil, []string{auth.ScopeNetworkingProxyRead, auth.ScopeComposeRead, auth.ScopePlatformsRead})
	h.srv.WithTraefik(&ruleLessTraefik{}).WithComposeDiscovery(composediscovery.Fixture{}).WithPlatforms(testRegistry(t))
	headers := platformPreviewHeaders(h)

	rr := h.do("POST", proxyPath+"/compose-preview", composeBody, headers)
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body)
	}
	if got := count(t, rr.Body.Bytes()); len(got) != 1 || got[0] != want {
		t.Fatalf("compose-preview: %q", got)
	}

	dw, _ := inventoryKeys(t, h, "docker-fixture")
	body := `{"platform":"docker-fixture","target":"routing.preview.compose-labels","workload":"` + dw["example-notes-web-1"] + `","network":"hc-traefik-test","model":{"name":"notes","host":"notes.example.com","port":8080,"entrypoint":"web","protocol":"http","upstreamScheme":"http"}}`
	rr = h.do("POST", proxyPath+"/platform-preview", body, headers)
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body)
	}
	if got := count(t, rr.Body.Bytes()); len(got) != 1 || got[0] != want {
		t.Fatalf("platform-preview: %q", got)
	}
	// The embedded Compose proposal carries the same consolidated warnings.
	var p struct {
		Compose struct {
			Warnings []struct{ Code, Message string } `json:"warnings"`
		} `json:"compose"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &p)
	n := 0
	for _, w := range p.Compose.Warnings {
		if w.Code == "host_unverified" {
			n++
		}
	}
	if n != 1 {
		t.Fatal("embedded compose proposal still repeats host_unverified", n)
	}
}
