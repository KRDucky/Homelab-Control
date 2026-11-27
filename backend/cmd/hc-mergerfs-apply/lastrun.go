// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"homelab-control/backend/internal/mergerfsapply"
	"homelab-control/backend/internal/safefile"
)

// errRunActive: another sync, scrub or fix holds the run lock.
var errRunActive = errors.New("a SnapRAID run is active")

// tracker keeps the record of one sync, scrub or fix in the unit's
// StateDirectory. Its lock is held for the whole run and is passed to
// SnapRAID, so it stays held while SnapRAID runs even if the helper stops.
// A nil tracker records nothing.
type tracker struct {
	mu   sync.Mutex
	path string
	lock *os.File
	rec  mergerfsapply.RunRecord
}

func runDir(root string) string { return filepath.Join(root, mergerfsapply.RunStateDir) }

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func beginRun(root, action string, checking bool) (*tracker, error) {
	dir := runDir(root)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, mergerfsapply.RunLockFile), os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errRunActive
		}
		return nil, err
	}
	t := &tracker{path: filepath.Join(dir, mergerfsapply.RunStateFile), lock: f,
		rec: mergerfsapply.RunRecord{Action: action, State: mergerfsapply.RunRunning, StartedAt: now()}}
	if checking {
		t.rec.State = mergerfsapply.RunChecking
	} else {
		t.rec.RunningAt = t.rec.StartedAt
	}
	t.save()
	return t, nil
}

func (t *tracker) save() {
	b, _ := json.Marshal(t.rec)
	if err := safefile.Write(t.path, append(b, '\n'), 0o600); err != nil {
		fmt.Fprintln(snapraidLog, "hc-mergerfs-apply: the run record was not written")
	}
}

// lockFile is the lock for SnapRAID to inherit, or nil.
func (t *tracker) lockFile() *os.File {
	if t == nil {
		return nil
	}
	return t.lock
}

func (t *tracker) running() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rec.State, t.rec.RunningAt = mergerfsapply.RunRunning, now()
	t.save()
}

// end records the outcome and releases the lock.
func (t *tracker) end(state string, code *int, blocked string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rec.State, t.rec.EndedAt, t.rec.ExitCode, t.rec.Blocked = state, now(), code, blocked
	t.save()
	_ = t.lock.Close()
}

func (t *tracker) finish(code int) {
	state := mergerfsapply.RunFinished
	if code != 0 {
		state = mergerfsapply.RunFailed
	}
	t.end(state, &code, "")
}

func (t *tracker) refuse(blocked string) { t.end(mergerfsapply.RunRefused, nil, blocked) }

// readRun returns the latest run record, or nil. A record that says checking
// or running while nobody holds the run lock is reported as interrupted: the
// helper and SnapRAID stopped (a reboot, a kill) before it ended.
func readRun(root string) *mergerfsapply.RunRecord {
	b, err := safefile.Read(filepath.Join(runDir(root), mergerfsapply.RunStateFile), 4<<10)
	if err != nil {
		return nil
	}
	var rec mergerfsapply.RunRecord
	if json.Unmarshal(b, &rec) != nil || !rec.Valid() || rec.State == mergerfsapply.RunInterrupted {
		return nil
	}
	if rec.State == mergerfsapply.RunChecking || rec.State == mergerfsapply.RunRunning {
		if !runLockHeld(root) {
			rec.State = mergerfsapply.RunInterrupted
		}
	}
	return &rec
}

func runLockHeld(root string) bool {
	f, err := os.OpenFile(filepath.Join(runDir(root), mergerfsapply.RunLockFile), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.Is(err, syscall.EWOULDBLOCK)
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}
