// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
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
