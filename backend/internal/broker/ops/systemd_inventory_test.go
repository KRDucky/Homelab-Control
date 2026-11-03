// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"fmt"
	. "homelab-control/backend/internal/broker/types"
	"os"
	"reflect"
	"testing"
)

func TestInventoryIncludesInstalledUnloadedUnits(t *testing.T) {
	loaded, err := ParseListUnits([]byte(`[{"unit":"running.service","load":"loaded","active":"active","sub":"running","description":"Observed"},{"unit":"transient.service","load":"loaded","active":"failed","sub":"failed"}]`))
	if err != nil {
		t.Fatal(err)
	}
	files, err := ParseListUnitFiles([]byte(`[{"unit_file":"running.service","state":"enabled","preset":"disabled"},{"unit_file":"stopped.service","state":"disabled","preset":"enabled"},{"unit_file":"masked.service","state":"masked"},{"unit_file":"ssh-alias.service","state":"alias"},{"unit_file":"worker@.service","state":"static"},{"unit_file":"wake.timer","state":"enabled","vendor_preset":"disabled"}]`))
	if err != nil {
		t.Fatal(err)
	}
	got := mergeUnitInventory(loaded, files)
	want := []Unit{
		{Name: "masked.service", Type: "service", Load: "not-loaded", Active: "unknown", Sub: "unknown", UnitFileState: "masked"},
		{Name: "running.service", Type: "service", Load: "loaded", Active: "active", Sub: "running", Description: "Observed", UnitFileState: "enabled", Preset: "disabled"},
		{Name: "ssh-alias.service", Type: "service", Load: "not-loaded", Active: "unknown", Sub: "unknown", UnitFileState: "alias"},
		{Name: "stopped.service", Type: "service", Load: "not-loaded", Active: "unknown", Sub: "unknown", UnitFileState: "disabled", Preset: "enabled"},
		{Name: "transient.service", Type: "service", Load: "loaded", Active: "failed", Sub: "failed"},
		{Name: "wake.timer", Type: "timer", Load: "not-loaded", Active: "unknown", Sub: "unknown", UnitFileState: "enabled", Preset: "disabled"},
		{Name: "worker@.service", Type: "service", Load: "not-loaded", Active: "unknown", Sub: "unknown", UnitFileState: "static"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("inventory = %+v; want %+v", got, want)
	}
}

func TestInventoryWithRealSystemctlFixtures(t *testing.T) {
	for _, rel := range []string{"jammy", "noble"} {
		t.Run(rel, func(t *testing.T) {
			b, err := os.ReadFile("testdata/list-units-" + rel + ".json")
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := ParseListUnits(b)
			if err != nil {
				t.Fatal(err)
			}
			b, err = os.ReadFile("testdata/list-unit-files-" + rel + ".json")
			if err != nil {
				t.Fatal(err)
			}
			files, err := ParseListUnitFiles(b)
			if err != nil {
				t.Fatal(err)
			}
			originals := map[string]Unit{}
			for _, u := range loaded {
				originals[u.Name] = u
			}
			got := mergeUnitInventory(loaded, files)
			seen := map[string]bool{}
			added := 0
			for _, u := range got {
				if seen[u.Name] {
					t.Fatalf("duplicate %s", u.Name)
				}
				seen[u.Name] = true
				if old, ok := originals[u.Name]; ok {
					if u.Load != old.Load || u.Active != old.Active || u.Sub != old.Sub || u.Description != old.Description {
						t.Fatalf("runtime overwritten: %+v", u)
					}
				} else {
					added++
					if u.Load != "not-loaded" || u.Active != "unknown" || u.Sub != "unknown" || u.Description != "" {
						t.Fatalf("invented runtime: %+v", u)
					}
				}
			}
			for name := range files {
				if !seen[name] {
					t.Errorf("missing installed unit %s", name)
				}
			}
			if added == 0 {
				t.Fatal("fixture failed to exercise unloaded installed units")
			}
		})
	}
}

func TestUnitFileInventoryRejectsInvalidNamesAndStates(t *testing.T) {
	files, err := ParseListUnitFiles([]byte(`[{"unit_file":"noextension","state":"enabled"},{"unit_file":"bad.slice","state":"enabled"},{"unit_file":"<script>.service","state":"enabled"},{"unit_file":"bad.service","state":"enabled;exec"},{"unit_file":"valid.service","state":"static"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files["valid.service"].State != "static" {
		t.Fatalf("unexpected files: %+v", files)
	}
}

// Synthetic, test-only names. Runtime units sort after file-only ones, so a
// plain name-order cut would drop runtime observations first.
func TestInventoryCapKeepsRuntimeUnitsBeforeFileOnlyEntries(t *testing.T) {
	var loaded []Unit
	for i := 0; i < maxUnits-10; i++ {
		loaded = append(loaded, Unit{Name: fmt.Sprintf("z-run-%04d.service", i), Type: "service", Load: "loaded", Active: "active", Sub: "running"})
	}
	loaded = append(loaded, Unit{Name: "zz-failed.service", Type: "service", Load: "loaded", Active: "failed", Sub: "failed"})
	files := map[string]rawUnitFile{}
	for i := 0; i < 100; i++ {
		files[fmt.Sprintf("a-file-%04d.service", i)] = rawUnitFile{State: "disabled"}
	}
	merged := mergeUnitInventory(loaded, files)
	if len(merged) != maxUnits-10+1+100 {
		t.Fatalf("merged %d units", len(merged))
	}
	got, truncated := capInventory(merged, maxUnits)
	if !truncated || len(got) != maxUnits {
		t.Fatalf("cap: truncated=%v len=%d", truncated, len(got))
	}
	runtime, fileOnly := 0, 0
	for i, u := range got {
		if i > 0 && got[i-1].Name >= u.Name {
			t.Fatalf("not name-sorted at %d: %s >= %s", i, got[i-1].Name, u.Name)
		}
		if u.Load == fileOnlyLoad {
			fileOnly++
		} else {
			runtime++
		}
	}
	if runtime != maxUnits-10+1 || fileOnly != 9 {
		t.Fatalf("kept runtime=%d fileOnly=%d; want every runtime unit and 9 file-only", runtime, fileOnly)
	}
	if got[len(got)-1].Name != "zz-failed.service" || got[0].Name != "a-file-0000.service" || got[8].Name != "a-file-0008.service" {
		t.Fatalf("unexpected kept boundary: first=%s ninth=%s last=%s", got[0].Name, got[8].Name, got[len(got)-1].Name)
	}
	if small, tr := capInventory(merged[:3], maxUnits); tr || len(small) != 3 {
		t.Fatalf("under the cap: truncated=%v len=%d", tr, len(small))
	}
}
