// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package storageprobe is the M2-4 native read-only storage probe (owner
// decision O-1): a separately packaged, disabled-by-default helper that runs
// fixed, non-mutating LVM and blkid templates on enumerated, revalidated
// block devices. It is the second place allowed to execute programs (ADR 0008;
// guard allowedExecRoots), alongside internal/broker/runner.
//
// Security properties:
//   - No caller path, binary or free-form argument: the templates are fixed
//     here, and a device is only ever the validated kernel name joined to a
//     fixed /dev/ prefix.
//   - The environment is replaced with a minimal, locale-forced set.
//   - Each command has a size-capped output and a timeout that kills the
//     process group.
//   - LVM runs with hints disabled and --readonly; blkid probes read-only with
//     no cache. Nothing is written to a device or a mount.
package storageprobe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// Fixed resource bounds.
const (
	maxReportBytes = 8 << 20 // per LVM report
	maxBlkidBytes  = 64 << 10
	reportTimeout  = 12 * time.Second
	blkidTimeout   = 5 * time.Second
)

// LVM report field lists. These request exactly the fields the storageinfo
// parsers consume; "original" JSON (string-valued columns) is used, with byte
// units and no suffix, so json_std numeric output is never produced.
const (
	lvsFields = "lv_uuid,lv_name,vg_uuid,vg_name,lv_parent,lv_size,lv_attr," +
		"lv_health_status,raid_sync_action,sync_percent,cache_total_blocks," +
		"cache_used_blocks,cache_dirty_blocks,origin_uuid,pool_lv_uuid," +
		"data_lv_uuid,metadata_lv_uuid,segtype,seg_start,seg_size,cache_mode," +
		"cache_policy,devices,metadata_devices"
	pvsFields = "pv_uuid,pv_name,vg_uuid,vg_name,pv_attr,pv_missing,pv_major," +
		"pv_minor,dev_size,lv_uuid,pvseg_start,pvseg_size"
)

// blockName is the kernel block-device name grammar (sdX, nvmeNnM, dm-N, …).
var blockName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.!:-]{0,254}$`)

// binaries maps a fixed tool to its absolute path. It is a var so synthetic
// tests can point at fake scripts; production uses the fixed reviewed paths.
var binaries = map[string]string{
	"lvs":   "/usr/sbin/lvs",
	"pvs":   "/usr/sbin/pvs",
	"blkid": "/usr/sbin/blkid",
}

// env is the scrubbed child environment: no inherited PATH, secrets or locale.
var env = []string{"LANG=C", "LC_ALL=C", "PATH=/usr/sbin:/usr/bin:/sbin:/bin"}

// sysBlockRoot is the sysfs block root used for device revalidation. It is a
// var so synthetic tests can point it at a fixture tree; production uses the
// fixed "/sys/block".
var sysBlockRoot = "/sys/block"

// Request is the closed typed input. Devices are kernel block names only.
type Request struct {
	Version int      `json:"version"`
	Devices []string `json:"devices"`
}

// Result is the closed typed output. LVS/PVS carry raw lvs/pvs --reportformat
// json reports; Blkid maps a device name to its raw blkid -o export output.
type Result struct {
	Version int               `json:"version"`
	LVS     string            `json:"lvs,omitempty"`
	PVS     string            `json:"pvs,omitempty"`
	Blkid   map[string]string `json:"blkid,omitempty"`
	Errors  []string          `json:"errors,omitempty"`
	Partial bool              `json:"partial"`
}

// Run executes the fixed templates. Devices are revalidated by name before any
// command runs; an invalid or missing device is reported, not opened.
func Run(ctx context.Context, req Request) Result {
	res := Result{Version: 1, Blkid: map[string]string{}}

	devs := validDevices(req.Devices)

	lvs, err := run(ctx, reportTimeout, maxReportBytes, "lvs", lvsArgs())
	if err != nil {
		res.Errors = append(res.Errors, "lvs: "+err.Error())
		res.Partial = true
	}
	res.LVS = lvs

	pvs, err := run(ctx, reportTimeout, maxReportBytes, "pvs", pvsArgs())
	if err != nil {
		res.Errors = append(res.Errors, "pvs: "+err.Error())
		res.Partial = true
	}
	res.PVS = pvs

	for _, name := range devs {
		out, err := run(ctx, blkidTimeout, maxBlkidBytes, "blkid", blkidArgs(name))
		if err != nil {
			res.Errors = append(res.Errors, "blkid "+name+": "+err.Error())
			res.Partial = true
			continue
		}
		res.Blkid[name] = out
	}
	return res
}

// lvsArgs is the fixed lvs template: read-only, hints disabled, byte units,
// all LVs with their segments, fixed fields.
func lvsArgs() []string {
	return []string{
		"--readonly", "--reportformat", "json", "--units", "b", "--nosuffix",
		"-a", "--segments", "-o", lvsFields, "--config", "devices{hints=0}",
	}
}

// pvsArgs is the fixed pvs --segments template.
func pvsArgs() []string {
	return []string{
		"--readonly", "--segments", "--reportformat", "json", "--units", "b",
		"--nosuffix", "-o", pvsFields, "--config", "devices{hints=0}",
	}
}

// blkidArgs is the fixed blkid template: read-only probe, no cache read or
// write, export output, one device.
func blkidArgs(name string) []string {
	return []string{"-p", "-c", "/dev/null", "-o", "export", "/dev/" + name}
}

// validDevices keeps only syntactically valid names and revalidates them
// against sysBlockRoot so a stale or caller-supplied name is never opened.
func validDevices(names []string) []string {
	out := []string{}
	for _, n := range names {
		if !blockName.MatchString(n) || strings.Contains(n, "/") {
			continue
		}
		if fi, err := os.Lstat(sysBlockRoot + "/" + n); err != nil || !fi.IsDir() {
			continue
		}
		out = append(out, n)
	}
	return out
}

// run executes one fixed binary with the given args, a scrubbed environment, a
// size-capped stdout and a timeout that kills the whole process group.
func run(ctx context.Context, timeout time.Duration, max int, tool string, args []string) (string, error) {
	bin := binaries[tool]
	if bin == "" {
		return "", fmt.Errorf("%s is not registered", tool)
	}
	childCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(childCtx, bin, args...)
	cmd.Env = env
	cmd.Stdin = nil
	var out cappedBuffer
	out.max = max
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second

	err := cmd.Run()
	if out.tooBig {
		return out.buf.String(), fmt.Errorf("%s output exceeded %d bytes", tool, max)
	}
	if err != nil {
		if childCtx.Err() != nil {
			return out.buf.String(), fmt.Errorf("%s timed out", tool)
		}
		return out.buf.String(), fmt.Errorf("%s: %w", tool, err)
	}
	return out.buf.String(), nil
}

// cappedBuffer bounds the bytes a child may write before its output is
// reported as too large.
type cappedBuffer struct {
	buf    bytes.Buffer
	max    int
	tooBig bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if c.tooBig {
		return len(p), nil
	}
	if c.buf.Len()+len(p) > c.max {
		c.buf.Write(p[:c.max-c.buf.Len()])
		c.tooBig = true
		return len(p), nil
	}
	return c.buf.Write(p)
}

// MarshalResponse serialises a Result for the closed typed protocol.
func MarshalResponse(res Result) ([]byte, error) { return json.Marshal(res) }

// UnmarshalRequest parses a closed typed Request.
func UnmarshalRequest(b []byte) (Request, error) {
	var req Request
	if err := json.Unmarshal(b, &req); err != nil {
		return req, err
	}
	if req.Version != 1 {
		return req, fmt.Errorf("unsupported protocol version %d", req.Version)
	}
	return req, nil
}
