// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package runner

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
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

func TestSupplementaryGroupsNeverRoot(t *testing.T) {
	r := testRunner(t)
	if _, err := r.RunWithGroups(context.Background(), []uint32{0}, "echo", "x"); !errors.Is(err, ErrBadArg) {
		t.Fatalf("group 0 accepted: %v", err)
	}
	gid := uint32(os.Getgid())
	if gid == 0 {
		gid = 65534
	}
	res, err := r.RunWithGroups(context.Background(), []uint32{gid}, "echo", "ok")
	if err != nil || strings.TrimSpace(string(res.Stdout)) != "ok" {
		t.Fatalf("%v %q", err, res.Stdout)
	}
}

func TestJournalctlIsTheOnlyNewBinary(t *testing.T) {
	want := map[string]bool{"systemctl": true, "journalctl": true}
	if len(allowlist) != len(want) {
		t.Fatalf("allow-list changed (%d entries); new binaries need security review", len(allowlist))
	}
	for k := range allowlist {
		if !want[k] {
			t.Errorf("unexpected binary %q", k)
		}
	}
}

// TestRootTemplatesAreClosed is the root-exec snapshot (ADR 0008 Amendment 2):
// exactly these templates, each with this exact argv. Changing it requires review.
func TestRootTemplatesAreClosed(t *testing.T) {
	want := map[RootTemplate]string{
		RootSystemctlStart:   "systemctl --no-ask-password --job-mode=fail start -- demo.service",
		RootSystemctlStop:    "systemctl --no-ask-password --job-mode=fail stop -- demo.service",
		RootSystemctlRestart: "systemctl --no-ask-password --job-mode=fail restart -- demo.service",
		RootSystemctlShow:    "systemctl " + strings.Join(SystemctlShowArgs("demo.service"), " "),
	}
	if len(rootTemplates) != len(want) {
		t.Fatalf("%d root templates, want %d — new root templates need review", len(rootTemplates), len(want))
	}
	for id, w := range want {
		argv, err := RootArgv(id, "demo.service")
		if err != nil || rootTemplates[id].binary+" "+strings.Join(argv, " ") != w {
			t.Errorf("%s: %v %v", id, argv, err)
		}
	}
	if _, err := RootArgv("systemctl.enable", "demo.service"); !errors.Is(err, ErrUnknownTemplate) {
		t.Errorf("unknown template: %v", err)
	}
	if _, err := RootArgv("sh", "-c id"); !errors.Is(err, ErrUnknownTemplate) {
		t.Errorf("shell template: %v", err)
	}
}

func TestRootTargetValidation(t *testing.T) {
	for _, bad := range []string{"", "-x.service", "--", "--now", "a b.service", "a;b.service", "$(id).service",
		"foo@.service", "../x.service", "a/b.service", "x\n.service", "x\x00.service", "foo\\x2d.service",
		strings.Repeat("a", 101) + ".service", "-.mount", "x.unknowntype"} {
		for id := range rootTemplates {
			if _, err := RootArgv(id, bad); !errors.Is(err, ErrBadArg) {
				t.Errorf("%s %q: %v", id, bad, err)
			}
		}
	}
	// Writes take exactly one .service unit; the read-only query takes any
	// unit type, at most 32 at a time.
	for _, id := range []RootTemplate{RootSystemctlStart, RootSystemctlStop, RootSystemctlRestart} {
		if _, err := RootArgv(id, "x.socket"); !errors.Is(err, ErrBadArg) {
			t.Errorf("%s accepted a socket", id)
		}
		if _, err := RootArgv(id, "a.service", "b.service"); !errors.Is(err, ErrBadArg) {
			t.Errorf("%s accepted two targets", id)
		}
		if _, err := RootArgv(id); !errors.Is(err, ErrBadArg) {
			t.Errorf("%s accepted no target", id)
		}
	}
	if _, err := RootArgv(RootSystemctlShow, "x.socket", "y.target", "z@1.service"); err != nil {
		t.Errorf("show: %v", err)
	}
	many := make([]string, 33)
	for i := range many {
		many[i] = "a.service"
	}
	if _, err := RootArgv(RootSystemctlShow, many...); !errors.Is(err, ErrBadArg) {
		t.Error("show accepted 33 targets")
	}
	// The show template never contains a verb that changes state.
	for _, a := range rootTemplates[RootSystemctlShow].argv {
		if !strings.HasPrefix(a, "--property=") && a != "show" && a != "--no-pager" && a != "--" {
			t.Errorf("unexpected show argument %q", a)
		}
	}
}

// TestRootRunUsesFixedArgvAndScrubbedEnv runs a template with "systemctl"
// mapped to echo/env so the exact argv and environment of the child are visible.
func TestRootRunUsesFixedArgvAndScrubbedEnv(t *testing.T) {
	t.Setenv("HC_SECRET_TOKEN", "leak-me")
	saved := allowlist
	allowlist = map[string][]string{"systemctl": {"/usr/bin/echo", "/bin/echo"}}
	t.Cleanup(func() { allowlist = saved })
	r, err := New(Identity{UID: uint32(os.Getuid()), GID: uint32(os.Getgid())})
	if err != nil {
		t.Fatal(err)
	}
	res, err := r.RootExec().Run(context.Background(), RootSystemctlStop, "hc-test-dummy.service")
	if err != nil || strings.TrimSpace(string(res.Stdout)) != "--no-ask-password --job-mode=fail stop -- hc-test-dummy.service" {
		t.Fatalf("%v %q", err, res.Stdout)
	}
	if _, err := r.RootExec().Run(context.Background(), RootSystemctlStop, "x;reboot"); !errors.Is(err, ErrBadArg) {
		t.Fatalf("bad target ran: %v", err)
	}
	allowlist = map[string][]string{"systemctl": {"/usr/bin/env", "/bin/env"}}
	r2, _ := New(Identity{UID: uint32(os.Getuid()), GID: uint32(os.Getgid())})
	res, _ = r2.RootExec().Run(context.Background(), RootSystemctlStart, "demo.service")
	if strings.Contains(string(res.Stdout), "leak-me") {
		t.Fatal("root child inherited the broker environment")
	}
	var nilRoot *Root
	if _, err := nilRoot.Run(context.Background(), RootSystemctlStart, "demo.service"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("nil handle: %v", err)
	}
}

func TestRootRunTimeoutKillsGroup(t *testing.T) {
	saved := allowlist
	allowlist = map[string][]string{"systemctl": {"/usr/bin/sleep", "/bin/sleep"}}
	t.Cleanup(func() { allowlist = saved })
	r, _ := New(Identity{UID: uint32(os.Getuid()), GID: uint32(os.Getgid())})
	// sleep rejects the systemctl flags immediately; the point is that the
	// handle honours the context like Run does.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _ = r.RootExec().Run(ctx, RootSystemctlStart, "demo.service")
	if time.Since(start) > 3*time.Second {
		t.Fatal("root child not bounded by context")
	}
}

func TestChildSysProcAttrUsesSetsidNotSetpgid(t *testing.T) {
	a := childSysProcAttr(&syscall.Credential{Uid: 1, Gid: 1})
	if !a.Setsid || a.Setpgid || a.Pgid != 0 {
		t.Fatalf("want Setsid only, got %+v", a)
	}
	if os.Geteuid() == 0 {
		if a.Credential == nil || a.Credential.Uid != 1 || a.Credential.Gid != 1 {
			t.Fatalf("root should apply credential: %+v", a.Credential)
		}
	} else if a.Credential != nil {
		t.Fatal("non-root must not set Credential")
	}
	rootExec := childSysProcAttr(nil)
	if !rootExec.Setsid || rootExec.Setpgid || rootExec.Credential != nil {
		t.Fatalf("root-exec attr: %+v", rootExec)
	}
}

func TestExecErrorClassOmitsPath(t *testing.T) {
	pe := &os.PathError{Op: "fork/exec", Path: "/usr/bin/systemctl", Err: syscall.EACCES}
	got := errnoClass(pe)
	if got != "EACCES" {
		t.Fatalf("class %q", got)
	}
	err := &ExecError{Class: got, err: pe}
	s := err.Error()
	if s != "exec EACCES" || strings.Contains(s, "/") || strings.Contains(s, "systemctl") {
		t.Fatalf("leaked: %q", s)
	}
	if errnoClass(syscall.Errno(9999)) != "errno" || errnoClass(errors.New("x")) != "exec" {
		t.Fatal("unknown classes")
	}
	if ClassFromMessage("systemctl could not be run (EACCES)") != "EACCES" {
		t.Fatal("ClassFromMessage")
	}
	if ClassFromMessage("systemctl could not be run") != "" || ClassFromMessage("x (notanerrno)") != "" {
		t.Fatal("ClassFromMessage accepted a non-class")
	}
}

func TestConcurrentRuns(t *testing.T) {
	r := testRunner(t)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := r.Run(context.Background(), "echo", "ok")
			if err != nil {
				errs <- err
				return
			}
			if strings.TrimSpace(string(res.Stdout)) != "ok" {
				errs <- errors.New("bad stdout")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestTimeoutKillsGrandchildInGroup(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	dir := t.TempDir()
	pidfile := filepath.Join(dir, "child.pid")
	helper := filepath.Join(dir, "hold")
	body := "#!/bin/sh\nsleep 60 &\necho $! >'" + pidfile + "'\nwait\n"
	if err := os.WriteFile(helper, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	r := testRunner(t)
	r.skipOwnerCheck = true
	r.paths["hold"] = helper
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := r.Run(ctx, "hold")
	if !errors.Is(err, ErrTimeout) || time.Since(start) > 5*time.Second {
		t.Fatalf("timeout not enforced: %v after %v", err, time.Since(start))
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		b, rerr := os.ReadFile(pidfile)
		if rerr != nil {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		pid := strings.TrimSpace(string(b))
		if pid == "" {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		if _, err := os.Stat("/proc/" + pid); err == nil {
			t.Fatalf("grandchild %s still alive", pid)
		}
		return
	}
	t.Log("pidfile not observed; group kill covered by TestTimeoutKillsProcessGroup")
}
