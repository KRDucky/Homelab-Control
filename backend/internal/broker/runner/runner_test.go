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
	"strconv"
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
	// systemctl computes its own unit lookup path from these: never inherited
	// (the review checks PID 1's UnitPath; the client must use the same path).
	t.Setenv("SYSTEMD_UNIT_PATH", "/tmp/leak-units:")
	t.Setenv("SYSTEMD_ROOT", "/tmp/leak-root")
	r := testRunner(t)
	res, err := r.Run(context.Background(), "env")
	if err != nil {
		t.Fatal(err)
	}
	out := string(res.Stdout)
	if strings.Contains(out, "leak-me") || strings.Contains(out, "SYSTEMD_UNIT_PATH") || strings.Contains(out, "leak-root") ||
		!strings.Contains(out, "LC_ALL=C.UTF-8") || !strings.Contains(out, "SYSTEMD_COLORS=0") {
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

// TestAllowListedBinariesAreClosed is the production allow-list snapshot.
// New binaries need security review (ADR 0008 Amendment 4).
func TestAllowListedBinariesAreClosed(t *testing.T) {
	want := map[string]bool{
		"systemctl": true, "journalctl": true, "hostnamectl": true, "timedatectl": true,
		// M4-1b (ADR 0008 Amendment 5, Proposed): read-only package managers.
		"dpkg-query": true, "apt-cache": true, "apt": true, "rpm": true, "dnf": true, "zypper": true, "pacman": true,
	}
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
	type snap struct {
		line    string
		targets []string
	}
	want := map[RootTemplate]snap{
		RootSystemctlStart:   {"systemctl --no-ask-password --job-mode=fail start -- demo.service", []string{"demo.service"}},
		RootSystemctlStop:    {"systemctl --no-ask-password --job-mode=fail stop -- demo.service", []string{"demo.service"}},
		RootSystemctlRestart: {"systemctl --no-ask-password --job-mode=fail restart -- demo.service", []string{"demo.service"}},
		RootSystemctlShow:    {"systemctl " + strings.Join(SystemctlShowArgs("demo.service"), " "), []string{"demo.service"}},
		// M2-3 (ADR 0008 Amendment 3): no --now, no --runtime, never a reload.
		RootSystemctlEnable:      {"systemctl --no-ask-password --no-reload enable -- demo.service", []string{"demo.service"}},
		RootSystemctlDisable:     {"systemctl --no-ask-password --no-reload disable -- demo.service", []string{"demo.service"}},
		RootSystemctlIsEnabled:   {"systemctl is-enabled -- demo.service", []string{"demo.service"}},
		RootSystemctlShowInstall: {"systemctl show --no-pager --property=Id --property=Names --property=LoadState --property=FragmentPath --property=SourcePath --property=DropInPaths --property=Transient --property=NeedDaemonReload --property=UnitFilePreset -- demo.service", []string{"demo.service"}},
		// PR #36 review (probe 8): manager unit-load timestamps, no target.
		RootSystemctlShowManager: {"systemctl show --no-pager --timestamp=us+utc --property=UnitsLoadStartTimestamp --property=UnitsLoadStartTimestampMonotonic --property=UnitsLoadTimestamp --property=UnitsLoadTimestampMonotonic --property=UnitPath", nil},
		// M2-6 (ADR 0008 Amendment 4, proposed).
		RootHostnamectlSetStatic:     {"hostnamectl --no-ask-password set-hostname --static -- nas-01", []string{"nas-01"}},
		RootHostnamectlSetPretty:     {"hostnamectl --no-ask-password set-hostname --pretty -- Lab NAS", []string{"Lab NAS"}},
		RootHostnamectlStatus:        {"hostnamectl --no-ask-password status", nil},
		RootTimedatectlSetTimezone:   {"timedatectl --no-ask-password set-timezone -- Europe/Berlin", []string{"Europe/Berlin"}},
		RootTimedatectlStatus:        {"timedatectl --no-ask-password status", nil},
		RootTimedatectlListTimezones: {"timedatectl --no-ask-password list-timezones", nil},
	}
	if len(rootTemplates) != len(want) {
		t.Fatalf("%d root templates, want %d — new root templates need review", len(rootTemplates), len(want))
	}
	for id, w := range want {
		if len(w.targets) == 0 {
			if _, err := RootArgv(id, "demo.service"); !errors.Is(err, ErrBadArg) {
				t.Errorf("%s accepted a target", id)
			}
		}
		argv, err := RootArgv(id, w.targets...)
		if err != nil || rootTemplates[id].binary+" "+strings.Join(argv, " ") != w.line {
			t.Errorf("%s: %v %v", id, argv, err)
		}
	}
	for _, unknown := range []RootTemplate{"systemctl.mask", "systemctl.unmask", "systemctl.daemon-reload", "systemctl.edit", "systemctl.reenable", "systemctl.preset", "systemctl.link",
		"hostnamectl.set-chassis", "timedatectl.set-ntp", "timedatectl.set-time"} {
		if _, err := RootArgv(unknown, "demo.service"); !errors.Is(err, ErrUnknownTemplate) {
			t.Errorf("unknown template %s: %v", unknown, err)
		}
	}
	for id, tpl := range rootTemplates {
		for _, a := range tpl.argv {
			switch a {
			case "--now", "--runtime", "--force", "--global", "--user", "daemon-reload", "mask", "unmask", "reenable", "preset", "link", "edit",
				"set-ntp", "set-time", "set-chassis", "set-icon", "set-deployment":
				t.Errorf("%s contains forbidden argument %q", id, a)
			}
		}
	}
	if _, err := RootArgv("sh", "-c id"); !errors.Is(err, ErrUnknownTemplate) {
		t.Errorf("shell template: %v", err)
	}
}

func TestRootTargetValidation(t *testing.T) {
	hostWrite := map[RootTemplate]bool{
		RootHostnamectlSetStatic: true, RootHostnamectlSetPretty: true, RootTimedatectlSetTimezone: true,
		RootHostnamectlStatus: true, RootTimedatectlStatus: true, RootTimedatectlListTimezones: true,
	}
	for _, bad := range []string{"", "-x.service", "--", "--now", "a b.service", "a;b.service", "$(id).service",
		"foo@.service", "../x.service", "a/b.service", "x\n.service", "x\x00.service", "foo\\x2d.service",
		strings.Repeat("a", 101) + ".service", "-.mount", "x.unknowntype"} {
		for id := range rootTemplates {
			if hostWrite[id] {
				continue
			}
			if _, err := RootArgv(id, bad); !errors.Is(err, ErrBadArg) && !(id == RootSystemctlShowManager && bad == "") {
				t.Errorf("%s %q: %v", id, bad, err)
			}
		}
	}
	if _, err := RootArgv(RootHostnamectlSetStatic, "nas-01"); err != nil {
		t.Errorf("static: %v", err)
	}
	if _, err := RootArgv(RootHostnamectlSetStatic, "NAS"); !errors.Is(err, ErrBadArg) {
		t.Errorf("static accepted uppercase: %v", err)
	}
	if _, err := RootArgv(RootHostnamectlSetPretty, ""); err != nil {
		t.Errorf("empty pretty (removal): %v", err)
	}
	if _, err := RootArgv(RootHostnamectlSetPretty, "Lab NAS"); err != nil {
		t.Errorf("pretty with space: %v", err)
	}
	if _, err := RootArgv(RootHostnamectlSetPretty, "a\nb"); !errors.Is(err, ErrBadArg) {
		t.Errorf("pretty accepted a newline: %v", err)
	}
	if _, err := RootArgv(RootTimedatectlSetTimezone, "Europe/Berlin"); err != nil {
		t.Errorf("timezone: %v", err)
	}
	if _, err := RootArgv(RootTimedatectlSetTimezone, "../../etc/passwd"); !errors.Is(err, ErrBadArg) {
		t.Errorf("timezone accepted a path: %v", err)
	}
	if _, err := RootArgv(RootHostnamectlStatus, "x"); !errors.Is(err, ErrBadArg) {
		t.Errorf("status accepted a target")
	}
	// Writes take exactly one .service unit; the read-only query takes any
	// unit type, at most 32 at a time.
	for _, id := range []RootTemplate{RootSystemctlEnable, RootSystemctlDisable} {
		if _, err := RootArgv(id, "getty@tty1.service"); !errors.Is(err, ErrBadArg) {
			t.Errorf("%s accepted a template instance", id)
		}
	}
	for _, id := range []RootTemplate{RootSystemctlStart, RootSystemctlStop, RootSystemctlRestart,
		RootSystemctlEnable, RootSystemctlDisable, RootSystemctlIsEnabled, RootSystemctlShowInstall} {
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
	t.Setenv("SYSTEMD_UNIT_PATH", "/tmp/leak-me-units:")
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

// procStat is the part of /proc/<pid>/stat this file needs; ok is false once
// the process has been reaped.
type procStat struct {
	state           string
	ppid, pgrp, sid int
	start           string // starttime in clock ticks; detects PID reuse
}

func readProcStat(pid int) (procStat, bool) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return procStat{}, false
	}
	// comm (field 2) may contain spaces; the fields after it start at ") ".
	s := string(b)
	f := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
	if len(f) < 20 {
		return procStat{}, false
	}
	ppid, _ := strconv.Atoi(f[1])
	pgrp, _ := strconv.Atoi(f[2])
	sid, _ := strconv.Atoi(f[3])
	return procStat{state: f[0], ppid: ppid, pgrp: pgrp, sid: sid, start: f[19]}, true
}

// TestTimeoutKillsGrandchildInGroup checks that cancelling a run kills a
// process the child left behind in its session and process group. The killed
// grandchild is reparented to init or a subreaper and stays in /proc as a
// zombie until that process reaps it, so a zombie counts as killed.
func TestTimeoutKillsGrandchildInGroup(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	self, ok := readProcStat(os.Getpid())
	if !ok {
		t.Skip("procfs not available")
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := r.Run(ctx, "hold")
		done <- err
	}()

	// Cancel only once the grandchild is known, so setup can never pass silently.
	pid := 0
	for deadline := time.Now().Add(5 * time.Second); pid == 0; {
		select {
		case err := <-done:
			t.Fatalf("helper exited before reporting its grandchild: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("grandchild pid not reported within 5s")
		}
		if b, err := os.ReadFile(pidfile); err == nil && strings.HasSuffix(string(b), "\n") {
			if pid, err = strconv.Atoi(strings.TrimSpace(string(b))); err != nil {
				t.Fatalf("bad pidfile %q", b)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	before, ok := readProcStat(pid)
	if !ok || before.state == "Z" {
		t.Fatalf("grandchild %d not running before cancel: %+v", pid, before)
	}
	// Setsid: the child leads a new session and group, and the grandchild is in both.
	if before.pgrp != before.ppid || before.sid != before.ppid || before.sid == self.sid || before.pgrp == self.pgrp {
		t.Fatalf("grandchild %d not in the child's own session and group: %+v (test %+v)", pid, before, self)
	}

	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, ErrTimeout) {
			t.Fatalf("cancelled run returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return within 5s of cancel")
	}
	for deadline := time.Now().Add(5 * time.Second); ; {
		st, ok := readProcStat(pid)
		if !ok || st.start != before.start || st.state == "Z" || st.state == "X" {
			return // reaped, PID reused, or dead and awaiting its reaper
		}
		if time.Now().After(deadline) {
			t.Fatalf("grandchild %d still alive %v after cancel: %+v", pid, time.Since(start), st)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestPackageQueriesAreClosed is the package-query snapshot (ADR 0008
// Amendment 5, Proposed): exactly these read templates, each with a fixed argv
// and no write/refresh/root verb. Changing it requires review.
func TestPackageQueriesAreClosed(t *testing.T) {
	want := map[PackageQuery]bool{
		PkgDebianInstalled: true, PkgDebianSearch: true, PkgDebianUpdates: true,
		PkgFedoraInstalled: true, PkgFedoraSearch: true, PkgFedoraUpdates: true,
		PkgSUSEInstalled: true, PkgSUSESearch: true, PkgSUSEUpdates: true, PkgSUSESources: true,
		PkgArchInstalled: true, PkgArchSearch: true, PkgArchUpdates: true,
	}
	if len(packageQueries) != len(want) {
		t.Fatalf("%d package queries, want %d — new templates need review", len(packageQueries), len(want))
	}
	for id, tpl := range packageQueries {
		if !want[id] {
			t.Errorf("unexpected package query %q", id)
		}
		if _, ok := allowlist[tpl.binary]; !ok {
			t.Errorf("%s: binary %q not in allow-list", id, tpl.binary)
		}
		if tpl.queryAt < -1 || tpl.queryAt >= len(tpl.argv) {
			t.Errorf("%s: invalid query slot %d", id, tpl.queryAt)
		}
		for _, a := range tpl.argv {
			banned := map[string]bool{"install": true, "remove": true, "purge": true, "upgrade": true, "dist-upgrade": true,
				"autoremove": true, "makecache": true, "refresh": true, "update": true, "in": true, "rm": true,
				"addrepo": true, "add-repo": true, "-Sy": true, "-Syy": true, "-S": true, "-R": true, "-U": true, "--sync": true}
			if banned[a] {
				t.Errorf("%s contains forbidden argument %q", id, a)
			}
		}
	}
	for id, tpl := range packageQueries {
		if tpl.queryAt < 0 {
			if _, err := PackageQueryArgv(id, "nginx"); !errors.Is(err, ErrBadArg) {
				t.Errorf("%s accepted a query", id)
			}
		} else if _, err := PackageQueryArgv(id, "nginx"); err != nil {
			t.Errorf("%s refused a query: %v", id, err)
		}
	}
	// Cache-only (ADR 0008 Amendment 5): network-capable tools must carry their
	// no-refresh flag. apt-cache/dpkg-query/rpm/pacman -Q/-Ss/-Qu are local.
	for id, want := range map[PackageQuery]string{
		PkgFedoraSearch:    "-C",
		PkgFedoraUpdates:   "-C",
		PkgSUSEInstalled:   "--no-refresh",
		PkgSUSESearch:      "--no-refresh",
		PkgSUSEUpdates:     "--no-refresh",
		PkgSUSESources:     "--no-refresh",
	} {
		found := false
		for _, a := range packageQueries[id].argv {
			if a == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: missing cache-only flag %q", id, want)
		}
	}
	for _, unknown := range []PackageQuery{"debian.remove", "arch.install", "generic.refresh"} {
		if _, err := PackageQueryArgv(unknown, ""); !errors.Is(err, ErrUnknownTemplate) {
			t.Errorf("unknown %s: %v", unknown, err)
		}
	}
}

func TestValidPackageQuery(t *testing.T) {
	for _, good := range []string{"nginx", "libssl3", "gcc-12", "c++", "python3.11", "a"} {
		if !ValidPackageQuery(good) {
			t.Errorf("query %q should be valid", good)
		}
	}
	for _, bad := range []string{"", "-o", "--config", "-", "--", "a b", "a;b", "$(id)", "`id`", "a|b", "a>b", "a*b", "a?b", "a/b", "a\\b", "a\x00b", strings.Repeat("a", 65), "é", "a\nb", "a\tb"} {
		if ValidPackageQuery(bad) {
			t.Errorf("query %q should be rejected", bad)
		}
	}
}
