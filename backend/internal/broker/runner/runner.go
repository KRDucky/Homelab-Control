// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package runner is the ONLY place in the code base allowed to execute
// programs (enforced by internal/guard). It runs allow-listed binaries with
// argv fixed by the calling operation, a replaced environment, no stdin,
// size-capped output, a timeout that kills the whole process group, and
// as the unprivileged service user. An op may add supplementary groups (e.g.
// the journal reader group); the uid stays the service user.
//
// One exception exists (ADR 0008 Amendment 2, owner decision D-4): the
// root-exec class. A Root handle runs one of a closed set of argv templates
// that are fixed in this file (RootTemplate); the caller supplies only the
// template ID and a target that the template's own validator accepts. There
// is no way to pass a binary, flags or free-form arguments to a root child.
// The broker hands a Root handle only to ops registered as root-exec.
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// Binary names the runner may execute, with candidate absolute paths.
// Adding an entry requires security review (ADR 0008).
var allowlist = map[string][]string{
	"systemctl":  {"/usr/bin/systemctl", "/bin/systemctl"},
	"journalctl": {"/usr/bin/journalctl", "/bin/journalctl"},
}

// argRE limits fixed arguments to a conservative character set; arguments
// are compile-time constants in ops, this is a second line of defence. A bare
// "--" (end of options, before unit names) is allowed.
var argRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9=,._:@/+-]*$|^--?[A-Za-z0-9][A-Za-z0-9=,._:@/+-]*$|^--$`)

// RootTemplate identifies one fixed root argv template. Adding a template
// requires security review (ADR 0008 Amendment 2) and the registry snapshot
// test in this package to be updated.
type RootTemplate string

// The root templates approved for M2-2: systemd service start/stop/restart,
// and the read-only state query that services.unit.apply uses to revalidate
// and to verify the outcome (so that its decision does not rest on a child
// process running as the same uid as hc-api, which hc-api could tamper with).
const (
	RootSystemctlStart   RootTemplate = "systemctl.start"
	RootSystemctlStop    RootTemplate = "systemctl.stop"
	RootSystemctlRestart RootTemplate = "systemctl.restart"
	RootSystemctlShow    RootTemplate = "systemctl.show"
)

type rootTemplate struct {
	binary     string   // allow-list key
	argv       []string // fixed arguments; the validated targets follow "--"
	valid      func(target string) bool
	maxTargets int
}

// systemctlShowProperties is the fixed property list of every `systemctl
// show` the broker runs (root or unprivileged): identity, state, safety
// properties, dependencies and the *Action= properties.
var systemctlShowProperties = []string{
	"Id", "Names", "Description", "LoadState", "ActiveState", "SubState", "Result",
	"UnitFileState", "FragmentPath", "MainPID", "ExecMainStatus", "InvocationID", "Type",
	"StateChangeTimestamp", "RefuseManualStart", "RefuseManualStop", "CanStart", "CanStop",
	"OnFailureJobMode", "OnSuccessJobMode", "StopWhenUnneeded",
	"Requires", "Requisite", "Wants", "BindsTo", "Upholds", "PartOf",
	"Conflicts", "ConflictedBy", "RequiredBy", "RequisiteOf", "BoundBy",
	"ConsistsOf", "PropagatesStopTo", "OnFailure", "OnSuccess",
	"FailureAction", "SuccessAction", "StartLimitAction", "JobTimeoutAction",
}

// SystemctlShowProperties returns a copy of the fixed property list.
func SystemctlShowProperties() []string { return append([]string{}, systemctlShowProperties...) }

// SystemctlShowArgs is the argv (without the binary) of the state query for
// units; the unprivileged path uses the same argv as the root template.
func SystemctlShowArgs(units ...string) []string {
	a := []string{"show", "--no-pager"}
	for _, p := range systemctlShowProperties {
		a = append(a, "--property="+p)
	}
	return append(append(a, "--"), units...)
}

// serviceUnitRE mirrors servicepolicy.UnitRE (kept here, independently, as the
// runner's own last line of defence): a .service unit, no leading '-', no
// escapes, separators or whitespace.
var serviceUnitRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_.@-]{0,99}\.service$`)

func validServiceUnit(t string) bool {
	return serviceUnitRE.MatchString(t) && !strings.Contains(t, "@.service") && !strings.Contains(t, "..")
}

// rootTemplates is the closed table of root children. --no-ask-password: never
// wait for an agent; --job-mode=fail: fail instead of replacing another queued
// job on the unit (a concurrent change made elsewhere is not overridden).
var rootTemplates = map[RootTemplate]rootTemplate{
	RootSystemctlStart:   {"systemctl", []string{"--no-ask-password", "--job-mode=fail", "start", "--"}, validServiceUnit, 1},
	RootSystemctlStop:    {"systemctl", []string{"--no-ask-password", "--job-mode=fail", "stop", "--"}, validServiceUnit, 1},
	RootSystemctlRestart: {"systemctl", []string{"--no-ask-password", "--job-mode=fail", "restart", "--"}, validServiceUnit, 1},
	// Read-only: SystemctlShowArgs (ending in "--"), then up to 32 units.
	RootSystemctlShow: {"systemctl", SystemctlShowArgs(), validQueryUnit, 32},
}

// queryUnitRE is the runner's own grammar for state-query targets: any unit
// type, no leading '-', no escapes, separators or whitespace.
var queryUnitRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_.@-]{0,99}\.(service|socket|timer|mount|automount|path|target|swap|slice|scope|device)$`)

func validQueryUnit(t string) bool {
	return queryUnitRE.MatchString(t) && !strings.Contains(t, "..") && !strings.Contains(t, "@.")
}

// Identity is the uid/gid children run as.
type Identity struct {
	UID, GID uint32
}

// Runner executes allow-listed binaries.
type Runner struct {
	paths     map[string]string
	identity  Identity
	maxStdout int
	maxStderr int
	// skipOwnerCheck is only for tests running as non-root against a
	// fixture binary; it cannot be set outside this package.
	skipOwnerCheck bool
}

// Errors returned by Run.
var (
	ErrNotAllowed = errors.New("binary not in allow-list")
	ErrBadArg     = errors.New("argument rejected")
	ErrTooLarge   = errors.New("output exceeded limit")
	ErrTimeout    = errors.New("timed out")
)

// New resolves the allow-list. Binaries that are missing are simply
// unavailable; binaries that exist but are unsafe (not root-owned, or
// group/world-writable) are an error.
func New(id Identity) (*Runner, error) {
	r := &Runner{paths: map[string]string{}, identity: id, maxStdout: 4 << 20, maxStderr: 64 << 10}
	for name, cands := range allowlist {
		for _, c := range cands {
			p, err := filepath.EvalSymlinks(c)
			if err != nil {
				continue
			}
			if err := checkBinary(p, false); err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			r.paths[name] = p
			break
		}
	}
	return r, nil
}

func checkBinary(p string, skipOwner bool) error {
	fi, err := os.Stat(p)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", p)
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s is group/world-writable", p)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && st.Uid != 0 && !skipOwner {
		return fmt.Errorf("%s is not owned by root", p)
	}
	return nil
}

// Available reports whether a binary was found.
func (r *Runner) Available(name string) bool { _, ok := r.paths[name]; return ok }

type capped struct {
	buf   bytes.Buffer
	limit int
	over  bool
}

func (c *capped) Write(p []byte) (int, error) {
	if c.buf.Len()+len(p) > c.limit {
		c.over = true
		return 0, ErrTooLarge
	}
	return c.buf.Write(p)
}

// Result of a run.
type Result struct {
	Stdout   []byte
	Stderr   string
	ExitCode int
	Duration time.Duration
}

// Run executes name with args. ctx should carry the op deadline.
func (r *Runner) Run(ctx context.Context, name string, args ...string) (Result, error) {
	return r.RunWithGroups(ctx, nil, name, args...)
}

// RunWithGroups is Run with additional supplementary groups for the child
// (still the unprivileged service uid/gid; never root).
func (r *Runner) RunWithGroups(ctx context.Context, groups []uint32, name string, args ...string) (Result, error) {
	for _, g := range groups {
		if g == 0 {
			return Result{}, fmt.Errorf("group 0: %w", ErrBadArg)
		}
	}
	path, ok := r.paths[name]
	if !ok {
		if _, known := allowlist[name]; known {
			return Result{}, fmt.Errorf("%s: %w", name, os.ErrNotExist)
		}
		return Result{}, fmt.Errorf("%q: %w", name, ErrNotAllowed)
	}
	for _, a := range args {
		if !argRE.MatchString(a) || len(a) > 128 {
			return Result{}, fmt.Errorf("%q: %w", a, ErrBadArg)
		}
	}
	if err := checkBinary(path, r.skipOwnerCheck); err != nil { // re-check at use time
		return Result{}, err
	}
	return r.exec(ctx, path, args, &syscall.Credential{Uid: r.identity.UID, Gid: r.identity.GID, Groups: append([]uint32{}, groups...)})
}

// exec runs path with args. cred is the child identity when the broker runs
// as root; nil means "run as root" and is only passed by Root.Run.
func (r *Runner) exec(ctx context.Context, path string, args []string, cred *syscall.Credential) (Result, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = []string{
		"PATH=/usr/sbin:/usr/bin:/sbin:/bin",
		"LC_ALL=C.UTF-8",
		"HOME=/nonexistent",
		"SYSTEMD_COLORS=0",
		"SYSTEMD_PAGER=",
		"SYSTEMD_LESS=",
		"TERM=dumb",
	}
	cmd.Dir = "/"
	cmd.Stdin = nil // /dev/null
	out := &capped{limit: r.maxStdout}
	errb := &capped{limit: r.maxStderr}
	cmd.Stdout, cmd.Stderr = out, errb
	cmd.SysProcAttr = childSysProcAttr(cred)
	// kill(-pid) targets the child's process group. After Setsid the
	// session id and pgid both equal the child's pid (setsid(2)), so this
	// is the same W9 contract as Setpgid: true.
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	start := time.Now()
	err := cmd.Run()
	res := Result{Stdout: out.buf.Bytes(), Stderr: errb.buf.String(), Duration: time.Since(start)}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	switch {
	case out.over || errb.over:
		return res, ErrTooLarge
	case ctx.Err() != nil:
		return res, ErrTimeout
	case err != nil:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return res, nil // non-zero exit is reported via ExitCode
		}
		return res, &ExecError{Class: errnoClass(err), err: err}
	}
	return res, nil
}

// childSysProcAttr is the SysProcAttr of every child (unprivileged and root-exec).
//
// Setsid, not Setpgid: after fork the child is not a session or process-group
// leader, so setsid() succeeds and makes sid = pgid = pid. Go's
// forkAndExecInChild (go 1.26, syscall/exec_linux.go) calls setsid before
// Credential and execve; it calls setpgid only when Setpgid is set, and
// setpgid on a session leader is EPERM, so Setpgid must stay false.
// SELinux init_t denies process:setpgid and allows this setsid path (P2).
func childSysProcAttr(cred *syscall.Credential) *syscall.SysProcAttr {
	attr := &syscall.SysProcAttr{Setsid: true}
	if os.Geteuid() == 0 && cred != nil {
		attr.Credential = cred
	}
	return attr
}

// ExecError is a failed fork/exec (not a child exit). Class is a closed errno
// name. Error() never includes a path, argv, token, or environment.
type ExecError struct {
	Class string
	err   error
}

func (e *ExecError) Error() string {
	if e == nil {
		return "exec"
	}
	if e.Class == "" {
		return "exec"
	}
	return "exec " + e.Class
}

func (e *ExecError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

var errnoNames = map[syscall.Errno]string{
	syscall.EACCES: "EACCES",
	syscall.EPERM:  "EPERM",
	syscall.ENOENT: "ENOENT",
	syscall.EAGAIN: "EAGAIN",
	syscall.ENOMEM: "ENOMEM",
	syscall.ENOSYS: "ENOSYS",
	syscall.EINVAL: "EINVAL",
	syscall.EIO:    "EIO",
	syscall.EINTR:  "EINTR",
	syscall.ENFILE: "ENFILE",
	syscall.EMFILE: "EMFILE",
	syscall.EFAULT: "EFAULT",
	syscall.ENOSPC: "ENOSPC",
}

func errnoClass(err error) string {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		if n, ok := errnoNames[errno]; ok {
			return n
		}
		return "errno"
	}
	return "exec"
}

// ClassFromMessage returns a closed errno class if msg ends with " (CLASS)".
func ClassFromMessage(msg string) string {
	for _, n := range errnoNames {
		if strings.HasSuffix(msg, " ("+n+")") {
			return n
		}
	}
	if strings.HasSuffix(msg, " (errno)") {
		return "errno"
	}
	if strings.HasSuffix(msg, " (exec)") {
		return "exec"
	}
	return ""
}

// Root runs the fixed root argv templates (root-exec class). The broker
// creates exactly one and passes it only to ops registered as root-exec.
type Root struct {
	r *Runner
}

// RootExec returns the root-exec handle of this runner.
func (r *Runner) RootExec() *Root { return &Root{r: r} }

// ErrUnknownTemplate is returned for a template ID that is not in the table.
var ErrUnknownTemplate = errors.New("root template not registered")

// RootArgv returns the exact argv (without the binary) a template produces
// for targets, or an error. Exposed for tests and audit records.
func RootArgv(t RootTemplate, targets ...string) ([]string, error) {
	tpl, ok := rootTemplates[t]
	if !ok {
		return nil, fmt.Errorf("%q: %w", t, ErrUnknownTemplate)
	}
	if len(targets) == 0 || len(targets) > tpl.maxTargets {
		return nil, fmt.Errorf("%d targets: %w", len(targets), ErrBadArg)
	}
	for _, target := range targets {
		if !tpl.valid(target) {
			return nil, fmt.Errorf("target %q: %w", target, ErrBadArg)
		}
	}
	return append(append([]string{}, tpl.argv...), targets...), nil
}

// Run executes template t on target as root. The binary is the template's
// allow-listed path, re-checked (root-owned, not group/world-writable) at use
// time; the environment is replaced; stdin is /dev/null; output is capped and
// the process group is killed when ctx expires.
func (x *Root) Run(ctx context.Context, t RootTemplate, targets ...string) (Result, error) {
	if x == nil || x.r == nil {
		return Result{}, ErrNotAllowed
	}
	argv, err := RootArgv(t, targets...)
	if err != nil {
		return Result{}, err
	}
	name := rootTemplates[t].binary
	path, ok := x.r.paths[name]
	if !ok {
		return Result{}, fmt.Errorf("%s: %w", name, os.ErrNotExist)
	}
	if err := checkBinary(path, x.r.skipOwnerCheck); err != nil {
		return Result{}, err
	}
	return x.r.exec(ctx, path, argv, nil)
}
