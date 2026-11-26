// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
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

// With Accept=yes systemd holds its own copy of the connection until the
// helper instance stops. Closing the helper's descriptors then leaves the
// broker waiting until its deadline; endReply must end the connection anyway.
func TestEndReplyEndsTheConnectionWhileAnotherCopyIsOpen(t *testing.T) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Non-blocking, so the read deadline below applies.
	if err := syscall.SetNonblock(fds[1], true); err != nil {
		t.Fatal(err)
	}
	helper, broker := os.NewFile(uintptr(fds[0]), "helper"), os.NewFile(uintptr(fds[1]), "broker")
	defer broker.Close()
	held, err := syscall.Dup(fds[0]) // systemd's copy
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(held)
	if _, err := helper.Write([]byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	endReply(helper)
	_ = broker.SetReadDeadline(time.Now().Add(2 * time.Second))
	b, err := io.ReadAll(broker)
	if err != nil || string(b) != "{}\n" {
		t.Fatalf("broker read %q, %v; want the reply then end of file", b, err)
	}
}
