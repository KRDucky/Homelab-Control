// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package guard

import (
	"path/filepath"
	"strings"
	"testing"
)

// The deployed hc-docker-metadata binary is reviewed by digest. It must link only
// its metadata packages: CORE-side code (API, platform registry, preview warning
// consolidation) changing must never change the broker binary.
func TestMetadataBrokerLinksOnlyMetadataPackages(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"homelab-control/backend/cmd/hc-docker-metadata":    true,
		"homelab-control/backend/internal/dockerbroker":     true,
		"homelab-control/backend/internal/composediscovery": true,
		"homelab-control/backend/internal/traefikclient":    true,
	}
	visited := map[string]bool{}
	queue := []string{"homelab-control/backend/cmd/hc-docker-metadata"}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if visited[p] {
			continue
		}
		visited[p] = true
		if !allowed[p] {
			t.Errorf("hc-docker-metadata links %s; broker inputs need review", p)
		}
		for _, imp := range moduleImports(t, root, p) {
			if imp == "os/exec" {
				t.Errorf("%s imports os/exec", p)
			}
			if strings.HasPrefix(imp, "homelab-control/backend/") {
				queue = append(queue, imp)
			}
		}
	}
	if len(visited) != len(allowed) {
		t.Fatalf("import walk visited %d packages, want %d", len(visited), len(allowed))
	}
}
