// Package runner is the ONLY place in the code base allowed to execute
// programs (enforced by internal/guard). It runs allow-listed binaries with
// argv fixed by the calling operation, a replaced environment, no stdin,
// size-capped output, a timeout that kills the whole process group, and —
// in M1 — always as the unprivileged service user, never as root.
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
	"syscall"
	"time"
)

// Binary names the runner may execute, with candidate absolute paths.
// Adding an entry requires security review (ADR 0008).
var allowlist = map[string][]string{
	"systemctl": {"/usr/bin/systemctl", "/bin/systemctl"},
}

// argRE limits fixed arguments to a conservative character set; arguments
// are compile-time constants in ops, this is a second line of defence.
var argRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9=,._:@/+-]*$|^--?[A-Za-z0-9][A-Za-z0-9=,._:@/+-]*$`)

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
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if os.Geteuid() == 0 {
		// Never run children as root in M1.
		cmd.SysProcAttr.Credential = &syscall.Credential{Uid: r.identity.UID, Gid: r.identity.GID, Groups: []uint32{}}
	}
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
		return res, err
	}
	return res, nil
}
