// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Command hc-mergerfs-apply is the opt-in MergerFS and SnapRAID helper.
// It reads one JSON request, validates it again, and runs only the closed
// argv from mergerfsapply. It is not started unless
// /etc/homelab-control/mergerfs-apply.enabled exists. The package ships that
// marker. The broker is a client of the socket; it does not mount or sync.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"homelab-control/backend/internal/mergerfsapply"
	"homelab-control/backend/internal/safefile"
)

const (
	maxRequestBytes  = 1 << 20
	maxResponseBytes = 256 << 10
	diffKeepBytes    = 64 << 10
	maxFileBytes     = 256 << 10
	systemdRun       = "/usr/bin/systemd-run"
)

func main() {
	detach := func() { endReply(os.Stdout, os.Stdin) }
	if err := run(os.Stdin, os.Stdout, "", detach); err != nil {
		fmt.Fprintln(os.Stderr, "hc-mergerfs-apply:", err.Error())
		os.Exit(1)
	}
}

func run(in io.Reader, out io.Writer, root string, detach func()) error {
	b, err := io.ReadAll(io.LimitReader(in, maxRequestBytes+1))
	if err != nil {
		return err
	}
	if len(b) > maxRequestBytes {
		return errors.New("request too large")
	}
	var req mergerfsapply.Request
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return err
	}
	res, rest := execute(context.Background(), req, root)
	body, err := json.Marshal(res)
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if len(body) > maxResponseBytes {
		return errors.New("response too large")
	}
	if _, err = out.Write(body); err != nil || rest == nil {
		return err
	}
	if detach != nil {
		detach()
	}
	rest()
	return nil
}

// endReply ends the connection so the broker returns while a long SnapRAID
// run continues. stdin and stdout are the accepted socket, and with
// Accept=yes systemd keeps its own copy of it until this instance stops, so
// closing ours is not enough: shutdown ends the connection for every holder.
func endReply(files ...*os.File) {
	for _, f := range files {
		if sc, err := f.SyscallConn(); err == nil {
			_ = sc.Control(func(fd uintptr) { _ = syscall.Shutdown(int(fd), syscall.SHUT_RDWR) })
		}
		_ = f.Close()
	}
}

// execute runs one request. A non-nil second result is the rest of a long
// SnapRAID run: the caller sends the reply first, then waits for it.
func execute(ctx context.Context, req mergerfsapply.Request, root string) (mergerfsapply.Result, func()) {
	spec, err := mergerfsapply.Validate(req)
	if err != nil {
		return done(false, 0, "request refused", ""), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 80*time.Second)
	defer cancel()
	switch spec.Action {
	case mergerfsapply.ActionMount:
		return mount(ctx, spec), nil
	case mergerfsapply.ActionPersist:
		if err := persist(root, spec); err != nil {
			if errors.Is(err, errUnmanagedSnapraid) {
				return done(false, 0, "array definition was not written: the existing snapraid.conf was not written by Homelab Control, so it was not replaced", ""), nil
			}
			return done(false, 0, "array definition was not written", ""), nil
		}
		return done(true, 0, "array definition written", ""), nil
	case mergerfsapply.ActionSync, mergerfsapply.ActionScrub:
		return startSnapraid(root, spec, diffCheck)
	case mergerfsapply.ActionStatus:
		argv, err := spec.Argv()
		if err != nil {
			return done(false, 0, "request refused", ""), nil
		}
		res := statusResult(runCmd(ctx, argv, mergerfsapply.MaxStatusOutput))
		res.LastRun = readRun(root)
		return res, nil
	case mergerfsapply.ActionFix:
		return startSnapraid(root, spec, nil)
	default:
		return done(false, 0, "request refused", ""), nil
	}
}

// startGrace: SnapRAID stops within moments on a held array lock, a bad
// configuration or a missing disk. A run still going after this is reported
// as started; sync, scrub and fix take minutes to many hours on a real array.
var startGrace = 5 * time.Second

// snapraidLog receives SnapRAID's own output: the helper's stderr, which the
// unit sends to the journal. It is not piped, so a long run is never stopped
// by a full pipe or an output cap.
var snapraidLog io.Writer = os.Stderr

// replyBudget bounds the wait for a reply. The broker waits 80 seconds for
// the helper; this leaves a margin.
var replyBudget = 60 * time.Second

// diffCheck is the check before sync and scrub (a test seam).
var diffCheck = func() string { return diffBlock(context.Background()) }

// startSnapraid runs gate (if any) and then SnapRAID. gate is snapraid diff,
// which reads every file of the array, so it has no deadline: on a large
// array it can take longer than the broker waits.
func startSnapraid(root string, spec mergerfsapply.Spec, gate func() string) (mergerfsapply.Result, func()) {
	argv, err := spec.Argv()
	if err != nil || !argvAllowed(argv) || argv[0] != mergerfsapply.SnapRAIDBin || (spec.Action == mergerfsapply.ActionFix && len(argv) < 5) {
		return done(false, 0, "request refused", ""), nil
	}
	t, err := beginRun(root, spec.Action, gate != nil)
	switch {
	case errors.Is(err, errRunActive):
		return done(false, 0, "operation refused before it started", mergerfsapply.BlockLocked), nil
	case err != nil:
		// The record is for the page; SnapRAID's own lock still guards the array.
		fmt.Fprintln(snapraidLog, "hc-mergerfs-apply: the run record is not available")
		t = nil
	}
	if gate == nil {
		return startLong(argv, spec.Action, t)
	}
	return startGated(argv, spec.Action, gate, t)
}

// startGated replies with the gate's block or startLong's reply if they come
// within replyBudget. Otherwise it replies that the check is running, and the
// returned func waits for it and logs the block or the run's exit status.
func startGated(argv []string, action string, gate func() string, t *tracker) (mergerfsapply.Result, func()) {
	type reply struct {
		res  mergerfsapply.Result
		rest func()
	}
	first := make(chan reply, 1)
	go func() {
		if reason := gate(); reason != "" {
			t.refuse(reason)
			first <- reply{done(false, 0, "operation refused before it started", reason), nil}
			return
		}
		t.running()
		res, rest := startLong(argv, action, t)
		first <- reply{res, rest}
	}()
	timer := time.NewTimer(replyBudget)
	defer timer.Stop()
	select {
	case r := <-first:
		return r.res, r.rest
	case <-timer.C:
		res := done(true, 0, "the check before "+action+" continues in the background", "")
		res.Running, res.Checking = true, true
		return res, func() {
			r := <-first
			switch {
			case r.res.Blocked != "":
				fmt.Fprintf(snapraidLog, "hc-mergerfs-apply: snapraid %s was not started: %s\n", action, r.res.Blocked)
			case r.rest != nil:
				r.rest()
			default:
				fmt.Fprintf(snapraidLog, "hc-mergerfs-apply: snapraid %s exited with status %d\n", action, r.res.ExitCode)
			}
		}
	}
}

// startLong starts argv without a deadline. If it exits within startGrace the
// reply carries its exit status; otherwise the reply says it is running and
// the returned func waits for it and logs the exit status.
func startLong(argv []string, action string, t *tracker) (mergerfsapply.Result, func()) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.Dir = "/"
	cmd.Stdout, cmd.Stderr = snapraidLog, snapraidLog
	if f := t.lockFile(); f != nil {
		cmd.ExtraFiles = []*os.File{f}
	}
	if err := cmd.Start(); err != nil {
		t.finish(-1)
		return done(false, -1, "snapraid did not start", ""), nil
	}
	exit := make(chan int, 1)
	go func() {
		code := exitStatus(cmd.Wait())
		t.finish(code)
		exit <- code
	}()
	timer := time.NewTimer(startGrace)
	defer timer.Stop()
	select {
	case code := <-exit:
		if code != 0 {
			return done(false, code, "snapraid exited", ""), nil
		}
		return done(true, 0, finishedSummary(action), ""), nil
	case <-timer.C:
		res := done(true, 0, action+" started and continues in the background", "")
		res.Running = true
		return res, func() {
			fmt.Fprintf(snapraidLog, "hc-mergerfs-apply: snapraid %s exited with status %d\n", action, <-exit)
		}
	}
}

func finishedSummary(action string) string {
	switch action {
	case mergerfsapply.ActionSync:
		return "sync finished"
	case mergerfsapply.ActionScrub:
		return "scrub finished"
	default:
		return "recovery finished"
	}
}

func exitStatus(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// statusResult carries snapraid's status text to the broker and marks text
// that was cut at the output cap.
func statusResult(code int, out []byte, truncated bool) mergerfsapply.Result {
	res := done(code == 0, code, "status read", "")
	res.Output = string(out)
	if truncated {
		res.OK, res.Summary, res.OutputTruncated = false, "status output exceeded the size limit", true
	}
	return res
}

func mount(ctx context.Context, spec mergerfsapply.Spec) mergerfsapply.Result {
	info, err := os.Lstat(spec.Mountpoint)
	switch {
	case err != nil && !os.IsNotExist(err):
		return done(false, 0, "mountpoint was not used", "")
	case err != nil:
		if err := os.Mkdir(spec.Mountpoint, 0o755); err != nil {
			return done(false, 0, "mountpoint was not created", "")
		}
	case info.Mode()&os.ModeSymlink != 0 || !info.IsDir():
		return done(false, 0, "mountpoint was not used", "")
	}
	argv, err := spec.Argv()
	if err != nil {
		return done(false, 0, "request refused", "")
	}
	// The oneshot helper is stopped when it replies. A scope keeps mergerfs
	// out of that cgroup, so the mount remains.
	wrapped := append([]string{systemdRun, "--scope", "--collect", "--quiet"}, argv...)
	code, _, _ := runCmd(ctx, wrapped, 4<<10)
	if code != 0 {
		return done(false, code, "mergerfs exited", "")
	}
	return done(true, 0, "mounted", "")
}

// diffBlock runs snapraid diff and keeps the start and end of its output.
// diff lists every changed file between its disk checks and its summary, so
// a capped prefix would lose the summary after a busy day of new files.
func diffBlock(ctx context.Context) string {
	out := &mergerfsapply.HeadTail{Keep: diffKeepBytes}
	code := runTo(ctx, []string{mergerfsapply.SnapRAIDBin, "diff"}, out)
	return mergerfsapply.DiffGate(code, out.String())
}

func done(ok bool, code int, summary, blocked string) mergerfsapply.Result {
	if containsPath(summary) || containsPath(blocked) {
		summary = "summary omitted"
		blocked = ""
		ok = false
	}
	return mergerfsapply.Result{Version: mergerfsapply.ProtocolVersion, OK: ok, ExitCode: code, Summary: summary, Blocked: blocked}
}

func containsPath(s string) bool {
	return stringsContainsSlash(s)
}

func stringsContainsSlash(s string) bool {
	for _, r := range s {
		if r == '/' || r == '\\' {
			return true
		}
	}
	return false
}

type capBuf struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (c *capBuf) Write(p []byte) (int, error) {
	if c.buf.Len() >= c.max {
		c.truncated = true
		return 0, errors.New("output cap")
	}
	room := c.max - c.buf.Len()
	if len(p) > room {
		_, _ = c.buf.Write(p[:room])
		c.truncated = true
		return 0, errors.New("output cap")
	}
	return c.buf.Write(p)
}

func argvAllowed(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	switch argv[0] {
	case mergerfsapply.MergerFSBin, mergerfsapply.SnapRAIDBin:
		return true
	case systemdRun:
		return len(argv) >= 5 && argv[1] == "--scope" && argv[2] == "--collect" && argv[3] == "--quiet" && argv[4] == mergerfsapply.MergerFSBin
	default:
		return false
	}
}

func runCmd(ctx context.Context, argv []string, limit int) (int, []byte, bool) {
	buf := &capBuf{max: limit}
	code := runTo(ctx, argv, buf)
	if buf.truncated {
		return -1, buf.buf.Bytes(), true
	}
	return code, buf.buf.Bytes(), false
}

// runTo runs one allowed argv with its output to w and returns the exit status.
func runTo(ctx context.Context, argv []string, w io.Writer) int {
	if !argvAllowed(argv) {
		return -1
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.Dir = "/"
	cmd.Stdout = w
	cmd.Stderr = w
	return exitStatus(cmd.Run())
}

func under(root, abs string) string { return root + abs }

func persist(root string, spec mergerfsapply.Spec) error {
	line, err := spec.FstabLine()
	if err != nil {
		return err
	}
	conf, err := spec.SnapraidConf()
	if err != nil {
		return err
	}
	snap := under(root, mergerfsapply.SnapraidPath)
	existing, err := readNoFollow(snap)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if !mergerfsapply.SnapraidReplaceable(existing) {
		return errUnmanagedSnapraid
	}
	fstab := under(root, mergerfsapply.FstabPath)
	old, err := readNoFollow(fstab)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	merged, err := mergerfsapply.MergeFstab(string(old), line, spec.Mountpoint)
	if err != nil {
		return err
	}
	if err := backupAndWrite(fstab, under(root, mergerfsapply.FstabBackup), []byte(merged)); err != nil {
		return err
	}
	if err := backupAndWrite(snap, under(root, mergerfsapply.SnapraidBak), []byte(conf)); err != nil {
		if old == nil {
			_ = os.Remove(fstab)
		} else {
			_ = backupAndWrite(fstab, under(root, mergerfsapply.FstabBackup), old)
		}
		return err
	}
	return nil
}

// errUnmanagedSnapraid: an existing snapraid.conf was not written by this
// helper, so the array definition is not saved over it.
var errUnmanagedSnapraid = errors.New("snapraid.conf was not written by Homelab Control")

func readNoFollow(path string) ([]byte, error) {
	b, err := safefile.Read(path, maxFileBytes)
	if errors.Is(err, safefile.ErrNotRegular) {
		return nil, errors.New("not a regular file")
	}
	if errors.Is(err, safefile.ErrTooLarge) {
		return nil, errors.New("file too large")
	}
	return b, err
}

func backupAndWrite(path, backup string, data []byte) error {
	if len(data) > maxFileBytes {
		return errors.New("file too large")
	}
	if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
		return err
	}
	if existing, err := readNoFollow(path); err == nil {
		if err := writeNoFollow(backup, existing); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return writeNoFollow(path, data)
}

func writeNoFollow(path string, data []byte) error {
	return safefile.Write(path, data, 0o644)
}

func dirOf(path string) string {
	i := len(path) - 1
	for i >= 0 && path[i] != '/' {
		i--
	}
	if i <= 0 {
		return "/"
	}
	return path[:i]
}
