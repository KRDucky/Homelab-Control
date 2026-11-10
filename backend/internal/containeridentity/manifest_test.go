// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package containeridentity

import (
	"os"
	"strings"
	"testing"
)

const validRuntime = `{"supportedPlatforms":["docker","podman"],"deploymentMethods":["compose"],
 "identity":{"configurable":true,"method":"env","uidVariable":"PUID","gidVariable":"PGID",
  "umask":{"method":"env","variable":"UMASK"},"supplementalGroups":true},
 "privileged":false,"capabilities":[],"volumes":[{"name":"config","target":"/config","access":"rw"}]}`

func TestManifestRuntimeDeclaration(t *testing.T) {
	c, err := ParseContainerRuntime([]byte(validRuntime))
	if err != nil {
		t.Fatal(err)
	}
	img, _ := c.ImageSupport()
	if img.IdentityMethod != MethodEnv || img.UmaskVariable != "UMASK" || !img.SupplementalGroups || len(c.RequiredVolumes()) != 1 {
		t.Fatalf("%+v", img)
	}
	bad := map[string]string{
		"unknown key":           strings.Replace(validRuntime, `"privileged"`, `"script":"x","privileged"`, 1),
		"duplicate key":         strings.Replace(validRuntime, `"privileged":false`, `"privileged":false,"privileged":true`, 1),
		"trailing data":         validRuntime + `{}`,
		"unknown platform":      strings.Replace(validRuntime, `"podman"]`, `"lxc"]`, 1),
		"k8s without manifests": strings.Replace(validRuntime, `"podman"]`, `"kubernetes"]`, 1),
		"not configurable env":  strings.Replace(validRuntime, `"configurable":true`, `"configurable":false`, 1),
		"fixed without ids":     strings.Replace(strings.Replace(validRuntime, `"configurable":true`, `"configurable":false`, 1), `"method":"env","uidVariable":"PUID","gidVariable":"PGID"`, `"method":"fixed"`, 1),
		"fixed root":            strings.Replace(strings.Replace(validRuntime, `"configurable":true`, `"configurable":false`, 1), `"method":"env","uidVariable":"PUID","gidVariable":"PGID"`, `"method":"fixed","fixedUid":0,"fixedGid":0`, 1),
		"vars on runtime-user":  strings.Replace(validRuntime, `"method":"env","uidVariable"`, `"method":"runtime-user","uidVariable"`, 1),
		"umask var on none":     strings.Replace(validRuntime, `"method":"env","variable":"UMASK"`, `"method":"none","variable":"UMASK"`, 1),
		"lowercase variable":    strings.Replace(validRuntime, `"PUID"`, `"puid"`, 1),
		"relative volume":       strings.Replace(validRuntime, `"/config"`, `"config"`, 1),
		"traversal volume":      strings.Replace(validRuntime, `"/config"`, `"/a/../b"`, 1),
		"bad access":            strings.Replace(validRuntime, `"access":"rw"`, `"access":"rwx"`, 1),
		"cap prefix":            strings.Replace(validRuntime, `"capabilities":[]`, `"capabilities":["CAP_NET_ADMIN"]`, 1),
		"oversize":              `{"supportedPlatforms":["docker"],"x":"` + strings.Repeat("a", 17<<10) + `"}`,
	}
	for name, doc := range bad {
		if _, err := ParseContainerRuntime([]byte(doc)); err == nil {
			t.Error("accepted:", name)
		}
	}
	fixed := strings.Replace(strings.Replace(validRuntime, `"configurable":true`, `"configurable":false`, 1), `"method":"env","uidVariable":"PUID","gidVariable":"PGID"`, `"method":"fixed","fixedUid":65532,"fixedGid":65532`, 1)
	if c, err := ParseContainerRuntime([]byte(fixed)); err != nil {
		t.Fatal(err)
	} else if s, _ := c.ImageSupport(); s.IdentityMethod != MethodFixed || *s.FixedUID != 65532 {
		t.Fatal(s)
	}
}

// The SDK example manifest's runtime section must satisfy the CORE parser,
// keeping the browser/SDK validator and CORE in agreement.
func TestSDKExampleRuntimeSection(t *testing.T) {
	b, err := os.ReadFile("../../../sdk/plugin/examples/container-identity.hlplugin")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	i := strings.Index(s, `"container":`)
	if i < 0 {
		t.Fatal("example lacks runtime.container")
	}
	// Extract the balanced object after "container":
	start := strings.Index(s[i:], "{") + i
	depth, end := 0, -1
	for j := start; j < len(s); j++ {
		switch s[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = j + 1
			}
		}
		if end > 0 {
			break
		}
	}
	if _, err := ParseContainerRuntime([]byte(s[start:end])); err != nil {
		t.Fatal(err)
	}
}

// Invariant relied on by the API: every declaration ParseContainerRuntime
// accepts also converts through ImageSupport without error, and the result
// is a valid ImageSupport (never a zero value).
func TestParsedRuntimeAlwaysConvertsToImageSupport(t *testing.T) {
	variants := []string{
		validRuntime,
		strings.Replace(validRuntime, `"method":"env","uidVariable":"PUID","gidVariable":"PGID"`, `"method":"runtime-user"`, 1),
		strings.Replace(strings.Replace(validRuntime, `"configurable":true`, `"configurable":false`, 1), `"method":"env","uidVariable":"PUID","gidVariable":"PGID"`, `"method":"fixed","fixedUid":65532,"fixedGid":0`, 1),
		strings.Replace(validRuntime, `"method":"env","variable":"UMASK"`, `"method":"entrypoint","variable":"HC_UMASK"`, 1),
		strings.Replace(validRuntime, `"method":"env","variable":"UMASK"`, `"method":"none"`, 1),
	}
	for i, doc := range variants {
		c, err := ParseContainerRuntime([]byte(doc))
		if err != nil {
			t.Fatal(i, err)
		}
		s, err := c.ImageSupport()
		if err != nil || s.Validate() != nil || s.IdentityMethod == "" || s.UmaskMethod == "" {
			t.Fatalf("variant %d: %v %+v", i, err, s)
		}
	}
}
