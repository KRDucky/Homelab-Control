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
	"time"

	"homelab-control/backend/internal/mergerfsapply"
	"homelab-control/backend/internal/safefile"
)

const (
	maxRequestBytes  = 1 << 20
	maxResponseBytes = 256 << 10
	maxDiffBytes     = 1 << 20
	maxFileBytes     = 256 << 10
	systemdRun       = "/usr/bin/systemd-run"
)

func main() {
	if err := run(os.Stdin, os.Stdout, ""); err != nil {
		fmt.Fprintln(os.Stderr, "hc-mergerfs-apply:", err.Error())
		os.Exit(1)
	}
}

func run(in io.Reader, out io.Writer, root string) error {
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
	res := execute(context.Background(), req, root)
	body, err := json.Marshal(res)
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if len(body) > maxResponseBytes {
		return errors.New("response too large")
	}
	_, err = out.Write(body)
	return err
}

func execute(ctx context.Context, req mergerfsapply.Request, root string) mergerfsapply.Result {
	spec, err := mergerfsapply.Validate(req)
	if err != nil {
		return done(false, 0, "request refused", "")
	}
	ctx, cancel := context.WithTimeout(ctx, 80*time.Second)
	defer cancel()
	switch spec.Action {
	case mergerfsapply.ActionMount:
		return mount(ctx, spec)
	case mergerfsapply.ActionPersist:
		if err := persist(root, spec); err != nil {
			if errors.Is(err, errUnmanagedSnapraid) {
				return done(false, 0, "array definition was not written: the existing snapraid.conf was not written by Homelab Control, so it was not replaced", "")
			}
			return done(false, 0, "array definition was not written", "")
		}
		return done(true, 0, "array definition written", "")
	case mergerfsapply.ActionSync, mergerfsapply.ActionScrub:
		reason := diffBlock(ctx)
		if reason != "" {
			return done(false, 0, "operation refused before it started", reason)
		}
		argv, err := spec.Argv()
		if err != nil {
			return done(false, 0, "request refused", "")
		}
		code, _, _ := runCmd(ctx, argv, 4<<10)
		if code != 0 {
			return done(false, code, "snapraid exited", "")
		}
		if spec.Action == mergerfsapply.ActionSync {
			return done(true, 0, "sync finished", "")
		}
		return done(true, 0, "scrub finished", "")
	case mergerfsapply.ActionStatus:
		argv, err := spec.Argv()
		if err != nil {
			return done(false, 0, "request refused", "")
		}
		code, out, truncated := runCmd(ctx, argv, mergerfsapply.MaxStatusOutput)
		res := done(code == 0, code, "status read", "")
		res.Output = string(out)
		if truncated {
			res.OK, res.Summary = false, "status output exceeded the size limit"
		}
		return res
	case mergerfsapply.ActionFix:
		argv, err := spec.Argv()
		if err != nil || len(argv) < 5 {
			return done(false, 0, "request refused", "")
		}
		code, _, _ := runCmd(ctx, argv, 4<<10)
		if code != 0 {
			return done(false, code, "snapraid exited", "")
		}
		return done(true, 0, "recovery finished", "")
	default:
		return done(false, 0, "request refused", "")
	}
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

func diffBlock(ctx context.Context) string {
	code, out, truncated := runCmd(ctx, []string{mergerfsapply.SnapRAIDBin, "diff"}, maxDiffBytes)
	return mergerfsapply.DiffGate(code, string(out), truncated)
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
	if !argvAllowed(argv) {
		return -1, nil, false
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.Dir = "/"
	buf := &capBuf{max: limit}
	cmd.Stdout = buf
	cmd.Stderr = buf
	err := cmd.Run()
	if buf.truncated {
		return -1, buf.buf.Bytes(), true
	}
	if err == nil {
		return 0, buf.buf.Bytes(), false
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), buf.buf.Bytes(), false
	}
	return -1, buf.buf.Bytes(), false
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
