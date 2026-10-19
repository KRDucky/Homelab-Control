// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package runner

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// testRunner registers host tools for this test only.
func testRunner(t *testing.T) *Runner {
	t.Helper()
	saved := allowlist
	allowlist = map[string][]string{
		"echo":  {"/usr/bin/echo", "/bin/echo"},
		"sleep": {"/usr/bin/sleep", "/bin/sleep"},
		"yes":   {"/usr/bin/yes", "/bin/yes"},
		"env":   {"/usr/bin/env", "/bin/env"},
	}
	t.Cleanup(func() { allowlist = saved })
	r, err := New(Identity{UID: uint32(os.Getuid()), GID: uint32(os.Getgid())})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestOnlyAllowlistedBinaries(t *testing.T) {
	r := testRunner(t)
	for _, name := range []string{"sh", "bash", "/bin/sh", "../echo", "perl"} {
		if _, err := r.Run(context.Background(), name, "x"); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("%q: want ErrNotAllowed, got %v", name, err)
		}
	}
}

func TestShellMetacharactersRejected(t *testing.T) {
	r := testRunner(t)
	for _, a := range []string{"a;b", "$(id)", "`id`", "a b", "a|b", "a&&b", ">x", "", "\n", "-", "a'b", strings.Repeat("a", 200)} {
		if _, err := r.Run(context.Background(), "echo", a); !errors.Is(err, ErrBadArg) {
			t.Errorf("%q: want ErrBadArg, got %v", a, err)
		}
	}
	res, err := r.Run(context.Background(), "echo", "--output=json", "list-units", "service,timer")
	if err != nil || strings.TrimSpace(string(res.Stdout)) != "--output=json list-units service,timer" {
		t.Fatalf("%v %q", err, res.Stdout)
	}
}

func TestEnvironmentReplaced(t *testing.T) {
	t.Setenv("HC_SECRET_TOKEN", "leak-me")
	r := testRunner(t)
	res, err := r.Run(context.Background(), "env")
	if err != nil {
		t.Fatal(err)
	}
	out := string(res.Stdout)
	if strings.Contains(out, "leak-me") || !strings.Contains(out, "LC_ALL=C.UTF-8") || !strings.Contains(out, "SYSTEMD_COLORS=0") {
		t.Fatalf("environment not replaced: %s", out)
	}
}

func TestTimeoutKillsProcessGroup(t *testing.T) {
	r := testRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := r.Run(ctx, "sleep", "30")
	if !errors.Is(err, ErrTimeout) || time.Since(start) > 5*time.Second {
		t.Fatalf("timeout not enforced: %v after %v", err, time.Since(start))
	}
}

func TestOutputCapped(t *testing.T) {
	r := testRunner(t)
	r.maxStdout = 1 << 16
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := r.Run(ctx, "yes"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("unbounded output not stopped: %v", err)
	}
}

func TestUnsafeBinaryRejected(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/tool"
	if err := os.WriteFile(p, []byte("#!/bin/true\n"), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o777); err != nil { // umask may strip w bits
		t.Fatal(err)
	}
	if err := checkBinary(p, true); err == nil {
		t.Fatal("world-writable binary accepted")
	}
	if err := os.Chmod(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if os.Getuid() != 0 {
		if err := checkBinary(p, false); err == nil {
			t.Fatal("non-root-owned binary accepted")
		}
	}
}

func TestMissingKnownBinaryIsNotExist(t *testing.T) {
	r := testRunner(t)
	allowlist["ghost"] = []string{"/nonexistent/ghost"}
	if _, err := r.Run(context.Background(), "ghost"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want ErrNotExist, got %v", err)
	}
}
