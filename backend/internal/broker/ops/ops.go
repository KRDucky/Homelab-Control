// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ops is the closed registry of broker operations (ADR 0008).
//
// Rules for every operation (review checklist):
//   - read-only in M1; no caller-supplied paths, commands or patterns;
//   - typed, strictly decoded args (M1 ops take none);
//   - fixed file paths opened with O_NOFOLLOW and size caps, or a fixed argv
//     through internal/broker/runner (children run unprivileged);
//   - results contain only allow-listed, non-secret fields.
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	. "homelab-control/backend/internal/broker/types"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/runner"
)

// Roots lets tests point file-reading ops at a fixture tree. Root is "/" in
// production; every op path is an absolute host path joined under it.
type Roots struct {
	Root string
}

// Path maps an absolute host path into the configured root.
func (r Roots) Path(abs string) string {
	if r.Root == "" {
		return abs
	}
	return filepath.Join(r.Root, abs)
}

// Env is passed to every handler.
type Env struct {
	Runner  *runner.Runner
	Roots   Roots
	Version string
}

// Op is one registered operation.
type Op struct {
	Name        string
	Scope       string // required scope; "" = any authenticated actor
	Privilege   Privilege
	Timeout     time.Duration
	Description string
	Handle      func(ctx context.Context, env *Env, args json.RawMessage) (any, error)
}

// Registry returns the compiled-in operation table.
func Registry() map[string]Op {
	ops := []Op{
		{Name: "broker.info", Scope: "", Privilege: PrivNone, Timeout: time.Second,
			Description: "Broker version and the list of registered operations", Handle: brokerInfo},
		{Name: "legacy.webmin.status", Scope: "system.read", Privilege: PrivRootRead, Timeout: 3 * time.Second,
			Description: "Installed upstream Webmin: version, listener, TLS and security posture (no secrets)", Handle: webminStatus},
		{Name: "hardware.dmi.identity", Scope: "hardware.read", Privilege: PrivRootRead, Timeout: 2 * time.Second,
			Description: "Root-only SMBIOS identifiers (system/board/chassis serials, product UUID)", Handle: dmiIdentity},
		{Name: "services.systemd.list", Scope: "services.read", Privilege: PrivUnprivilegedExec, Timeout: 10 * time.Second,
			Description: "systemd units with load/active/sub state and unit-file enablement", Handle: systemdList},
	}
	m := make(map[string]Op, len(ops))
	for _, o := range ops {
		if !proto.ValidOpName(o.Name) {
			panic("invalid op name " + o.Name)
		}
		m[o.Name] = o
	}
	return m
}

// noArgs is the typed argument struct for argument-free ops: any field is rejected.
type noArgs struct{}

func requireNoArgs(raw json.RawMessage) error {
	var a noArgs
	if err := proto.DecodeArgs(raw, &a); err != nil {
		return &proto.Error{Code: proto.CodeBadRequest, Message: "this operation takes no arguments"}
	}
	return nil
}

type infoResult struct {
	Version  string   `json:"version"`
	Protocol int      `json:"protocol"`
	Ops      []OpInfo `json:"ops"`
	Exec     []string `json:"exec_available"`
	// SystemdBooted mirrors sd_booted(): /run/systemd/system exists only when
	// systemd is PID 1. systemctl may be installed on hosts that do not run it.
	SystemdBooted bool `json:"systemd_booted"`
}

func brokerInfo(_ context.Context, env *Env, raw json.RawMessage) (any, error) {
	if err := requireNoArgs(raw); err != nil {
		return nil, err
	}
	res := infoResult{Version: env.Version, Protocol: proto.Version}
	for _, o := range Registry() {
		res.Ops = append(res.Ops, OpInfo{Name: o.Name, Scope: o.Scope, Privilege: o.Privilege, Description: o.Description})
	}
	sort.Slice(res.Ops, func(i, j int) bool { return res.Ops[i].Name < res.Ops[j].Name })
	if env.Runner != nil && env.Runner.Available("systemctl") {
		res.Exec = append(res.Exec, "systemctl")
	}
	if fi, err := os.Stat(env.Roots.Path("/run/systemd/system")); err == nil && fi.IsDir() {
		res.SystemdBooted = true
	}
	return res, nil
}

// readFixed reads a fixed path without following a final symlink, requiring a
// regular file, bounded by limit bytes.
func readFixed(path string, limit int64) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: not a regular file", path)
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s: larger than %d bytes", path, limit)
	}
	return b, nil
}

// parseKV parses Webmin-style key=value files. Later keys win.
func parseKV(b []byte) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		m[k] = v
	}
	return m
}

func isNotExist(err error) bool { return errors.Is(err, os.ErrNotExist) }

func clean(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	if len(s) > max {
		s = s[:max]
	}
	return s
}
