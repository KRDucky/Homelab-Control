// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
	"time"

	"homelab-control/backend/internal/mergerfsapply"
)

func lookPath(t *testing.T, name string) string {
	t.Helper()
	p, err := exec.LookPath(name)
	if err != nil {
		t.Skip(name, "not found")
	}
	return p
}

func withGrace(t *testing.T, d time.Duration) *bytes.Buffer {
	t.Helper()
	oldGrace, oldLog := startGrace, snapraidLog
	log := &bytes.Buffer{}
	startGrace, snapraidLog = d, log
	t.Cleanup(func() { startGrace, snapraidLog = oldGrace, oldLog })
	return log
}

// A sync on a real array runs for hours. The helper used to run it under its
// 80-second request deadline, which killed it. It now replies that the run
// started and waits for it without a deadline, logging the exit status.
func TestLongRunRepliesWhileItContinues(t *testing.T) {
	log := withGrace(t, 50*time.Millisecond)
	start := time.Now()
	res, rest := startLong([]string{lookPath(t, "sleep"), "0.6"}, mergerfsapply.ActionSync)
	if took := time.Since(start); took > 400*time.Millisecond {
		t.Fatalf("reply waited %v for the run", took)
	}
	if !res.OK || !res.Running || rest == nil || !strings.Contains(res.Summary, "started") {
		t.Fatalf("%+v rest=%v", res, rest != nil)
	}
	rest()
	if time.Since(start) < 600*time.Millisecond {
		t.Fatal("rest returned before the run ended")
	}
	if !strings.Contains(log.String(), "snapraid sync exited with status 0") {
		t.Fatalf("log: %q", log.String())
	}
}

// SnapRAID stops at once on a held lock or a bad configuration; that exit
// status is the reply.
func TestLongRunReportsAnEarlyExit(t *testing.T) {
	withGrace(t, 2*time.Second)
	res, rest := startLong([]string{lookPath(t, "false")}, mergerfsapply.ActionScrub)
	if res.OK || res.Running || rest != nil || res.ExitCode != 1 {
		t.Fatalf("%+v", res)
	}
	res, rest = startLong([]string{lookPath(t, "true")}, mergerfsapply.ActionScrub)
	if !res.OK || res.Running || rest != nil || res.Summary != "scrub finished" {
		t.Fatalf("%+v", res)
	}
}
