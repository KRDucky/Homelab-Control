// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsapply

import "time"

// RunStateDir is the helper unit's StateDirectory. It holds the record of the
// latest sync, scrub or fix, which the status action returns, so the page can
// show a run that continues after the reply and how it ended.
const (
	RunStateDir  = "/var/lib/homelab-control-mergerfs-apply"
	RunStateFile = "last-run.json"
	// RunLockFile is held (flock) by the helper for the whole run. A record
	// that says checking or running while nobody holds it is interrupted.
	RunLockFile = "last-run.lock"
)

// Run states.
const (
	RunChecking    = "checking"    // snapraid diff, before sync or scrub
	RunRunning     = "running"     // snapraid sync, scrub or fix
	RunFinished    = "finished"    // exit status 0
	RunFailed      = "failed"      // non-zero exit status, or it did not start
	RunRefused     = "refused"     // the check blocked it; Blocked says why
	RunInterrupted = "interrupted" // the helper stopped before the run ended (status only)
)

// RunRecord is the latest sync, scrub or fix. Times are RFC 3339 UTC.
type RunRecord struct {
	Action    string `json:"action"`
	State     string `json:"state"`
	StartedAt string `json:"startedAt"`
	// RunningAt: when SnapRAID itself started (after the check).
	RunningAt string `json:"runningAt,omitempty"`
	EndedAt   string `json:"endedAt,omitempty"`
	ExitCode  *int   `json:"exitCode,omitempty"`
	Blocked   string `json:"blocked,omitempty"`
}

// Valid reports whether r has only known values, so a damaged or hand-edited
// file is not shown as a run.
func (r RunRecord) Valid() bool {
	switch r.Action {
	case ActionSync, ActionScrub, ActionFix:
	default:
		return false
	}
	switch r.State {
	case RunChecking, RunRunning, RunFinished, RunFailed, RunRefused, RunInterrupted:
	default:
		return false
	}
	for _, t := range []string{r.StartedAt, r.RunningAt, r.EndedAt} {
		if t == "" {
			continue
		}
		if _, err := time.Parse(time.RFC3339, t); err != nil {
			return false
		}
	}
	if r.StartedAt == "" {
		return false
	}
	switch r.Blocked {
	case "", BlockMissingDisk, BlockRemovedFiles, BlockDiffUnreadable, BlockLocked:
	default:
		return false
	}
	return true
}
