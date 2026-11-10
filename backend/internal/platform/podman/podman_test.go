// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package podman

import (
	"strings"
	"testing"

	"homelab-control/backend/internal/composediscovery"
)

const row = `{"Id":"%s","Names":["app"],"State":"%s","Labels":{%s},"Ports":[%s],"Networks":["net"],"Pod":"","PodName":"","IsInfra":false}`

func one(id, state, labels, ports string) []byte {
	out := row
	for _, v := range []string{id, state, labels, ports} {
		out = strings.Replace(out, "%s", v, 1)
	}
	return []byte("[" + out + "]")
}

var good = strings.Repeat("a", 64)

func TestParseAcceptsLibpodSemantics(t *testing.T) {
	s, err := Parse(one(good, "stopped", `"com.docker.compose.project":"p","com.docker.compose.service":"s"`, `{"container_port":80,"host_port":8080,"range":0,"protocol":"tcp"}`), "podman-rootless", composediscovery.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if v := s.Services[0]; v.State != "exited" || !v.Compose || v.Project != "p" || len(v.Ports) != 1 || v.Ports[0].Port != 80 || s.Fixture {
		t.Fatalf("%+v", v)
	}
}

func TestParseFailsClosed(t *testing.T) {
	for name, raw := range map[string][]byte{
		"bad id":                     one("xyz", "running", ``, ``),
		"unknown state":              one(good, "zombie", ``, ``),
		"conflicting compose labels": one(good, "running", `"io.podman.compose.project":"a","io.podman.compose.service":"s","com.docker.compose.project":"b","com.docker.compose.service":"s"`, ``),
		"half compose identity":      one(good, "running", `"io.podman.compose.project":"a"`, ``),
		"bad protocol":               one(good, "running", ``, `{"container_port":80,"protocol":"icmp"}`),
		"range overflow":             one(good, "running", ``, `{"container_port":65535,"range":2,"protocol":"tcp"}`),
		"duplicate key":              []byte(`[{"Id":"` + good + `","Id":"` + good + `","Names":["a"],"State":"running"}]`),
		"not a list":                 []byte(`{}`),
		"trailing data":              append(one(good, "running", ``, ``), []byte(`[]`)...),
	} {
		if _, err := Parse(raw, "podman-rootless", composediscovery.Context{}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
