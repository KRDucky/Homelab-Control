// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"homelab-control/backend/internal/mergerfsapply"
)

func state(t *testing.T, root string) mergerfsapply.RunRecord {
	t.Helper()
	r := readRun(root)
	if r == nil {
		t.Fatal("no run record")
	}
	return *r
}

// The page could only say "locked" while a background run went on, and its
// end was only in the journal. The record now follows the run.
func TestRunRecordFollowsARun(t *testing.T) {
	withGrace(t, 20*time.Millisecond)
	withBudget(t, 20*time.Millisecond)
	root := t.TempDir()
	tr, err := beginRun(root, mergerfsapply.ActionSync, true)
	if err != nil {
		t.Fatal(err)
	}
	if r := state(t, root); r.State != mergerfsapply.RunChecking || r.StartedAt == "" || r.RunningAt != "" {
		t.Fatalf("%+v", r)
	}
	_, rest := startGated([]string{lookPath(t, "sleep"), "0.3"}, mergerfsapply.ActionSync,
		func() string { time.Sleep(50 * time.Millisecond); return "" }, tr)
	time.Sleep(150 * time.Millisecond)
	if r := state(t, root); r.State != mergerfsapply.RunRunning || r.RunningAt == "" {
		t.Fatalf("%+v", r)
	}
	rest()
	r := state(t, root)
	if r.State != mergerfsapply.RunFinished || r.ExitCode == nil || *r.ExitCode != 0 || r.EndedAt == "" {
		t.Fatalf("%+v", r)
	}
	if _, err := beginRun(root, mergerfsapply.ActionScrub, true); err != nil {
		t.Fatalf("lock not released after the run: %v", err)
	}
}

func TestRunRecordSaysRefusedAndFailed(t *testing.T) {
	withGrace(t, time.Second)
	withBudget(t, time.Second)
	root := t.TempDir()
	tr, _ := beginRun(root, mergerfsapply.ActionSync, true)
	startGated([]string{lookPath(t, "true")}, mergerfsapply.ActionSync, func() string { return mergerfsapply.BlockMissingDisk }, tr)
	if r := state(t, root); r.State != mergerfsapply.RunRefused || r.Blocked != mergerfsapply.BlockMissingDisk || r.EndedAt == "" {
		t.Fatalf("%+v", r)
	}
	tr, _ = beginRun(root, mergerfsapply.ActionFix, false)
	startLong([]string{lookPath(t, "false")}, mergerfsapply.ActionFix, tr)
	if r := state(t, root); r.State != mergerfsapply.RunFailed || r.ExitCode == nil || *r.ExitCode != 1 {
		t.Fatalf("%+v", r)
	}
}

// SnapRAID inherits the run lock, so a run whose helper stopped still reads
// as running while SnapRAID runs, and as interrupted once nothing holds it.
func TestRunRecordInterrupted(t *testing.T) {
	root := t.TempDir()
	tr, err := beginRun(root, mergerfsapply.ActionSync, false)
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(lookPath(t, "sleep"), "0.4")
	child.ExtraFiles = []*os.File{tr.lock}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	_ = tr.lock.Close() // the helper stops; SnapRAID goes on
	if r := state(t, root); r.State != mergerfsapply.RunRunning {
		t.Fatalf("while SnapRAID holds the lock: %+v", r)
	}
	_ = child.Wait()
	if r := state(t, root); r.State != mergerfsapply.RunInterrupted {
		t.Fatalf("after SnapRAID stopped: %+v", r)
	}
}

func TestSecondRunIsRefusedWhileOneIsActive(t *testing.T) {
	root := t.TempDir()
	tr, err := beginRun(root, mergerfsapply.ActionSync, true)
	if err != nil {
		t.Fatal(err)
	}
	defer tr.lock.Close()
	if _, err := beginRun(root, mergerfsapply.ActionScrub, true); !errors.Is(err, errRunActive) {
		t.Fatalf("got %v", err)
	}
	spec, err := mergerfsapply.Validate(mergerfsapply.Request{Version: mergerfsapply.ProtocolVersion, Action: mergerfsapply.ActionScrub})
	if err != nil {
		t.Fatal(err)
	}
	res, rest := startSnapraid(root, spec, func() string { t.Fatal("gate ran"); return "" })
	if res.OK || rest != nil || res.Blocked != mergerfsapply.BlockLocked {
		t.Fatalf("%+v", res)
	}
}

func TestDamagedRunRecordIsIgnored(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(runDir(root), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{`, `{"action":"rm","state":"running","startedAt":"2026-11-26T00:00:00Z"}`,
		`{"action":"sync","state":"done","startedAt":"2026-11-26T00:00:00Z"}`, `{"action":"sync","state":"finished","startedAt":"x"}`} {
		if err := os.WriteFile(filepath.Join(runDir(root), mergerfsapply.RunStateFile), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if r := readRun(root); r != nil {
			t.Fatalf("%s read as %+v", body, r)
		}
	}
}
