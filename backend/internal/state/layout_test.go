// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package state

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestLayoutMigrationFromVersionTwoKeepsBackupAndSnapshots(t *testing.T) {
	path := filepath.Join(dir(t), "state.db")
	all := migrations
	migrations = all[:2]
	db, err := Open(path)
	if err != nil {
		migrations = all
		t.Fatal(err)
	}
	if _, err := db.StorageSnapshots().Save(context.Background(), time.Now(), "boot-A", []byte(`{"nodes":[]}`)); err != nil {
		t.Fatal(err)
	}
	db.Close()
	migrations = all
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if v, _ := db.Version(context.Background()); v != 3 || db.Backup == "" {
		t.Fatalf("version %d backup %q", v, db.Backup)
	}
	if _, ok, err := db.StorageSnapshots().Latest(context.Background()); !ok || err != nil {
		t.Fatal("v2 snapshot lost", err)
	}
}

func TestLayoutPersistsAcrossReopenAndNeverOverwritesAssignments(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(dir(t), "state.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	spec := ChassisSpec{Name: "Main NAS", Kind: "server", Rows: 2, Cols: 3, Cells: []CellSpec{{Row: 0, Col: 0, Kind: "hdd-3.5", Label: "Bay 1"}, {Row: 1, Col: 2, Kind: "disabled"}}}
	id, err := db.Layouts().Create(ctx, spec, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Layouts().Assign(ctx, id, 0, 0, "wwn:naa.5000000000000f01", "admin", now); err != nil {
		t.Fatal(err)
	}
	db.Close()

	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	l := db.Layouts()
	list, err := l.List(ctx)
	if err != nil || len(list) != 1 || len(list[0].Cells) != 6 {
		t.Fatal("layout not persisted", list, err)
	}
	a := list[0].Cells[0].Assignment
	if a == nil || a.Identity != "wwn:naa.5000000000000f01" || a.Provenance != "manual" || a.Confidence != "unverified" || a.AssignedBy != "admin" || list[0].Cells[0].Label != "Bay 1" {
		t.Fatalf("assignment not persisted with manual provenance: %+v", a)
	}

	conflicts := map[string]error{
		"occupied bay":           l.Assign(ctx, id, 0, 0, "serial:OTHER", "admin", now),
		"identity elsewhere":     l.Assign(ctx, id, 0, 1, "wwn:naa.5000000000000f01", "admin", now),
		"non-bay cell":           l.Assign(ctx, id, 1, 2, "serial:OTHER", "admin", now),
		"shrink drops assigned":  l.Update(ctx, id, ChassisSpec{Name: "Main NAS", Kind: "server", Rows: 1, Cols: 1, Cells: []CellSpec{{Row: 0, Col: 0, Kind: "empty"}}}, now),
		"delete with assignment": l.Delete(ctx, id),
	}
	for name, err := range conflicts {
		if !errors.Is(err, ErrLayoutConflict) {
			t.Errorf("%s: got %v, want conflict", name, err)
		}
	}
	for _, weak := range []string{"by-id:ata-X", "t10-vendor-id:ATA X", "sda", "8:0", "block:abc", "serial:"} {
		if err := l.Assign(ctx, id, 0, 1, weak, "admin", now); !errors.Is(err, ErrLayoutInvalid) {
			t.Errorf("%q accepted: %v", weak, err)
		}
	}
	// A shrink that keeps the assigned bay is allowed and keeps the assignment.
	if err := l.Update(ctx, id, ChassisSpec{Name: "NAS", Kind: "shelf", Rows: 1, Cols: 2}, now); err != nil {
		t.Fatal(err)
	}
	list, _ = l.List(ctx)
	if len(list[0].Cells) != 2 || list[0].Cells[0].Assignment == nil || list[0].Name != "NAS" {
		t.Fatalf("update lost the assignment: %+v", list[0])
	}
	if err := l.Unassign(ctx, id, 0, 0, now); err != nil {
		t.Fatal(err)
	}
	if err := l.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
}

func TestLayoutSpecBounds(t *testing.T) {
	bad := []ChassisSpec{
		{Name: "", Kind: "server", Rows: 1, Cols: 1},
		{Name: " x", Kind: "server", Rows: 1, Cols: 1},
		{Name: "x", Kind: "rack", Rows: 1, Cols: 1},
		{Name: "x", Kind: "server", Rows: 25, Cols: 1},
		{Name: "x", Kind: "server", Rows: 1, Cols: 0},
		{Name: "x\n", Kind: "server", Rows: 1, Cols: 1},
		{Name: "x", Kind: "server", Rows: 1, Cols: 1, Cells: []CellSpec{{Row: 1, Col: 0, Kind: "tray"}}},
		{Name: "x", Kind: "server", Rows: 1, Cols: 2, Cells: []CellSpec{{Row: 0, Col: 0, Kind: "tray"}, {Row: 0, Col: 0, Kind: "tray"}}},
		{Name: "x", Kind: "server", Rows: 1, Cols: 1, Cells: []CellSpec{{Row: 0, Col: 0, Kind: "floppy"}}},
	}
	for i, s := range bad {
		if s.Validate() == nil {
			t.Errorf("spec %d accepted: %+v", i, s)
		}
	}
}
