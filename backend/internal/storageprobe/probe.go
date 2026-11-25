// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package storageprobe is the read-only storage helper (hc-storage-probe). It
// runs the standard tools with fixed arguments and JSON output: lsblk
// (util-linux) for the block-device list, smartctl (smartmontools) for SMART
// health on whole disks, and pvs/lvs (LVM2) in --readonly mode. It is the
// second place allowed to execute programs (ADR 0008; guard allowedExecRoots),
// alongside internal/broker/runner.
//
// Security properties:
//   - The caller names nothing: the request carries only the protocol
//     version. The helper takes disk names from its own lsblk run, checks them
//     against the kernel-name grammar and the SMART device classes, and builds
//     every command line from fixed templates (storagetools).
//   - Tools resolve only to reviewed absolute paths that are root-owned and not
//     writable by others; the environment is replaced with a minimal one.
//   - smartctl runs with --nocheck=standby, so a sleeping disk is not woken.
//     pvs/lvs run with --readonly (no locks, no device-mapper). Nothing writes
//     to a device; the unit grants read-only device access only.
//   - Each command has a size-capped output and a timeout that kills its
//     process group. At most one Run is admitted per rate window.
package storageprobe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"homelab-control/backend/internal/mdadminfo"
	"homelab-control/backend/internal/mountinfo"
	"homelab-control/backend/internal/storagetools"
)

// Fixed resource bounds.
const (
	maxStderrBytes = 4 << 10 // bounded child stderr; never returned
	lsblkTimeout   = 10 * time.Second
	smartTimeout   = 8 * time.Second
	lvmTimeout     = 10 * time.Second
)

// runSem caps concurrent Runs in-process (defence in depth; the socket unit
// serialises connections). minRunInterval rate-limits on-demand probes.
var (
	runSem         = make(chan struct{}, 1)
	runMu          sync.Mutex
	lastRun        time.Time
	minRunInterval = time.Second
	// budget bounds a whole Run per request scope (storagetools.HelperBudget,
	// which the broker's op timeouts derive from); a var only as a test seam.
	budget = storagetools.HelperBudget
)

// binaries maps a fixed tool to its absolute path. It is a var so tests can
// point at fake scripts; production resolves secure paths via ResolveToolPaths.
var binaries = map[string]string{
	"lsblk":    "/usr/bin/lsblk",
	"smartctl": "/usr/sbin/smartctl",
	"pvs":      "/usr/sbin/pvs",
	"lvs":      "/usr/sbin/lvs",
	"zpool":    "/usr/sbin/zpool",
	"zfs":      "/usr/sbin/zfs",
	"btrfs":    "/usr/bin/btrfs",
	"mdadm":    "/usr/sbin/mdadm",
}

// requiredTools must resolve; the others are optional (not installed means
// "no SMART" or "no LVM", reported in Result.Tools).
var requiredTools = map[string]bool{"lsblk": true}

// toolCandidates lists reviewed absolute paths per tool across distros, in
// preference order. Only these exact paths are ever accepted.
var toolCandidates = map[string][]string{
	"lsblk":    {"/usr/bin/lsblk", "/bin/lsblk"},
	"smartctl": {"/usr/sbin/smartctl", "/sbin/smartctl"},
	"pvs":      {"/usr/sbin/pvs", "/sbin/pvs"},
	"lvs":      {"/usr/sbin/lvs", "/sbin/lvs"},
	"zpool":    {"/usr/sbin/zpool", "/sbin/zpool", "/usr/bin/zpool"},
	"zfs":      {"/usr/sbin/zfs", "/sbin/zfs", "/usr/bin/zfs"},
	"btrfs":    {"/usr/bin/btrfs", "/bin/btrfs", "/usr/sbin/btrfs", "/sbin/btrfs"},
	"mdadm":    {"/usr/sbin/mdadm", "/sbin/mdadm", "/usr/bin/mdadm"},
}

// resolved records which tools passed ResolveToolPaths.
var resolved = map[string]bool{}

// ResolveToolPaths locates each tool at a reviewed path and verifies it is a
// regular, root-owned, non-writable-by-others file (no symlink escape). A
// required tool that is missing or insecure is an error; an optional one is
// recorded as unavailable.
func ResolveToolPaths() error {
	for tool := range binaries {
		path, err := secureToolPath(toolCandidates[tool])
		if err != nil {
			resolved[tool] = false
			if requiredTools[tool] {
				return fmt.Errorf("%s: %w", tool, err)
			}
			continue
		}
		binaries[tool] = path
		resolved[tool] = true
	}
	return nil
}

// secureToolPath returns the first candidate whose resolved target and its
// directory chain are secure: a regular, root-owned, non-writable file. The
// original path is returned (not the resolved one) so a multi-call binary
// dispatches on argv[0] correctly; the resolved target was the thing verified.
func secureToolPath(candidates []string) (string, error) {
	for _, p := range candidates {
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil {
			continue
		}
		if err := checkSecureFile(resolved); err != nil {
			continue
		}
		if err := checkSecureDirs(resolved); err != nil {
			continue
		}
		return p, nil
	}
	return "", fmt.Errorf("no secure binary (checked %v)", candidates)
}

func checkSecureFile(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("not a regular file")
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("group/other writable")
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != 0 {
		return fmt.Errorf("not root-owned")
	}
	return nil
}

// checkSecureDirs walks the resolved path's ancestors to the filesystem root
// and requires each to be root-owned and non-writable by group/other, so an
// unprivileged user cannot swap the file through a writable directory.
func checkSecureDirs(path string) error {
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		fi, err := os.Stat(dir)
		if err != nil {
			return err
		}
		if fi.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("directory %s group/other writable", dir)
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 {
			return fmt.Errorf("directory %s not root-owned", dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return nil
}

// env is the scrubbed child environment: no inherited PATH, secrets or locale.
// LVM_SUPPRESS_FD_WARNINGS keeps pvs/lvs from warning about inherited fds.
var env = []string{"LANG=C", "LC_ALL=C", "PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LVM_SUPPRESS_FD_WARNINGS=1"}

// procDevicesPath is the kernel's device-major registry. It is a var so
// synthetic tests can point it at a fixture.
var procDevicesPath = "/proc/devices"

// reviewedBlockGroups are the block-device classes the unit's DeviceAllow=
// grants read access to. systemd adds a group's rule only when the group is
// registered in /proc/devices at unit start, so a missing group simply stays
// unreadable (fail closed). A host without software RAID has no "md" group.
var reviewedBlockGroups = []string{"sd", "blkext", "virtblk", "md"}

// checkDeviceGroups reads /proc/devices and returns the reviewed groups that
// are not registered. It is an error only when none of them is registered,
// because then the helper cannot read any disk.
func checkDeviceGroups() (missing []string, err error) {
	b, err := os.ReadFile(procDevicesPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", procDevicesPath, err)
	}
	present := map[string]bool{}
	inBlock := false
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "Block devices:" {
			inBlock = true
			continue
		}
		if !inBlock || line == "" {
			continue
		}
		if fields := strings.Fields(line); len(fields) >= 2 {
			present[fields[1]] = true
		}
	}
	for _, g := range reviewedBlockGroups {
		if !present[g] {
			missing = append(missing, g)
		}
	}
	if len(missing) == len(reviewedBlockGroups) {
		return missing, fmt.Errorf("no reviewed block device class is available (%s)", strings.Join(reviewedBlockGroups, ", "))
	}
	return missing, nil
}

// Request and Result are the closed protocol (storagetools).
type (
	Request = storagetools.Request
	Result  = storagetools.Result
)

// CodeDevicePolicyUnresolved is re-exported for callers of this package.
const CodeDevicePolicyUnresolved = storagetools.CodeDevicePolicyUnresolved

// Run executes the fixed templates: lsblk, then smartctl on each SMART-capable
// whole disk, then pvs and lvs. A failing or missing optional tool makes the
// result Partial; it never fails the whole Run.
func Run(ctx context.Context, req Request) Result {
	res := Result{Version: storagetools.ProtocolVersion, Blocks: []storagetools.BlockDevice{}, SMART: map[string]storagetools.SMART{}, Tools: map[string]bool{}}
	for tool := range binaries {
		res.Tools[tool] = resolved[tool]
	}
	fail := func(msg string) { res.Errors = append(res.Errors, msg); res.Partial = true }
	if !admit() {
		fail("probe rate-limited or busy")
		return res
	}
	defer release()

	ctx, cancel := context.WithTimeout(ctx, budget(req.Scope))
	defer cancel()
	// The tool scopes open no block device: ZFS talks only to /dev/zfs (its
	// own DeviceAllow entry) and btrfs-progs uses ioctls on a mountpoint. The
	// block-class self-check below does not apply to them.
	switch req.Scope {
	case storagetools.ScopeZFS:
		runZFS(ctx, &res, fail)
		return res
	case storagetools.ScopeBtrfs:
		runBtrfs(ctx, &res, fail)
		return res
	}

	// Startup self-check for the block-device probe: refuse before any block
	// device is read when no reviewed class is registered; note classes that
	// are absent.
	if missing, err := checkDeviceGroups(); err != nil {
		res.Code = CodeDevicePolicyUnresolved
		fail("probe refused: " + err.Error())
		return res
	} else if len(missing) > 0 {
		res.Errors = append(res.Errors, "block device classes not registered on this host (their devices are not read): "+strings.Join(missing, ", "))
	}

	// mdadm --detail opens each md block device, so it runs after the
	// block-class self-check.
	if req.Scope == storagetools.ScopeMd {
		runMd(ctx, &res, fail)
		return res
	}

	out, _, err := run(ctx, lsblkTimeout, storagetools.MaxLsblkBytes, "lsblk", storagetools.LsblkArgs())
	if err != nil {
		fail("lsblk: " + err.Error())
		return res
	}
	blocks, err := storagetools.ParseLsblk(out)
	if err != nil {
		fail(err.Error())
		return res
	}
	res.Blocks = blocks

	if !res.Tools["smartctl"] {
		fail("smartctl is not installed (package smartmontools); SMART health is not shown")
	} else {
		n := 0
		for _, d := range blocks {
			if !storagetools.SMARTCandidate(d) {
				continue
			}
			if n++; n > storagetools.MaxDisks {
				fail(fmt.Sprintf("SMART read for the first %d disks only", storagetools.MaxDisks))
				break
			}
			out, code, err := run(ctx, smartTimeout, storagetools.MaxSmartBytes, "smartctl", storagetools.SmartctlArgs(d.KName))
			if err != nil {
				fail("smartctl " + d.KName + ": " + err.Error())
				continue
			}
			s, err := storagetools.ParseSMART(out, code)
			if err != nil {
				fail("smartctl " + d.KName + ": " + err.Error())
				continue
			}
			res.SMART[d.KName] = s
		}
	}

	if !res.Tools["pvs"] || !res.Tools["lvs"] {
		res.Errors = append(res.Errors, "LVM tools are not installed (package lvm2); no LVM data")
	} else {
		for _, r := range []struct {
			tool string
			args []string
			dst  *json.RawMessage
		}{{"pvs", storagetools.PVSArgs(), &res.PVS}, {"lvs", storagetools.LVSArgs(), &res.LVS}} {
			out, code, err := run(ctx, lvmTimeout, storagetools.MaxLVMBytes, r.tool, r.args)
			if err == nil && code != 0 {
				err = fmt.Errorf("exit status %d", code)
			}
			if err == nil {
				err = storagetools.CheckJSONReport(out)
			}
			if err != nil {
				fail(r.tool + ": " + err.Error())
				continue
			}
			*r.dst = json.RawMessage(out)
		}
	}
	return res
}

// runZFS runs the fixed read-only OpenZFS templates (zpool list, zpool status,
// zfs list). It needs /dev/zfs (the unit's DeviceAllow). Missing tools are a
// partial result with the package name.
func runZFS(ctx context.Context, res *Result, fail func(string)) {
	if !res.Tools["zpool"] || !res.Tools["zfs"] {
		fail("zpool/zfs are not installed (package zfsutils-linux); no ZFS data")
		return
	}
	var out [3][]byte
	for i, c := range []struct {
		tool string
		args []string
	}{{"zpool", storagetools.ZpoolListArgs()}, {"zpool", storagetools.ZpoolStatusArgs()}, {"zfs", storagetools.ZfsListArgs()}} {
		b, code, err := run(ctx, storagetools.HelperCommandTimeout, storagetools.MaxZFSBytes, c.tool, c.args)
		if err == nil && code != 0 {
			err = fmt.Errorf("exit status %d", code)
		}
		if err != nil {
			fail(c.tool + " " + c.args[0] + ": " + err.Error())
			return
		}
		out[i] = b
	}
	rep, err := storagetools.ParseZFS(out[0], out[1], out[2])
	if err != nil {
		fail(err.Error())
		return
	}
	res.ZFS = &rep
	if rep.Truncated {
		res.Partial = true
		res.Errors = append(res.Errors, "the ZFS report exceeded its limits; some pools, vdevs or datasets are not listed")
	}
}

// mountinfoPath is the helper's own mount table (a test seam).
var mountinfoPath = "/proc/self/mountinfo"

// hiddenPrefixes are paths the unit's ProtectHome=yes replaces with empty
// mounts, so a Btrfs mount there cannot be opened from the helper.
var hiddenPrefixes = []string{"/home", "/root", "/run/user"}

func hiddenPath(p string) bool {
	for _, h := range hiddenPrefixes {
		if p == h || strings.HasPrefix(p, h+"/") {
			return true
		}
	}
	return false
}

// btrfsTarget is one Btrfs filesystem: the mountpoint the reports are taken
// from, and every mount of it.
type btrfsTarget struct {
	mountpoint string
	mounts     []string
}

// btrfsTargets groups btrfs mounts by device number (one filesystem, any
// number of subvolume mounts) and picks the first mountpoint the helper can
// open. The helper names the paths itself; no caller supplies one.
func btrfsTargets(recs []mountinfo.Record) (targets []btrfsTarget, skipped []storagetools.BtrfsSkipped, truncated bool) {
	order := []string{}
	groups := map[string][]string{}
	for _, r := range recs {
		if r.FSType != "btrfs" || mountinfo.HasDotDot(r.Mountpoint) || len(r.Mountpoint) > mountinfo.MountpointMaxBytes {
			continue
		}
		if _, ok := groups[r.DeviceNumber]; !ok {
			order = append(order, r.DeviceNumber)
		}
		if len(groups[r.DeviceNumber]) < 32 {
			groups[r.DeviceNumber] = append(groups[r.DeviceNumber], r.Mountpoint)
		}
	}
	for _, dev := range order {
		mounts := groups[dev]
		pick := ""
		for _, m := range mounts {
			if !hiddenPath(m) {
				pick = m
				break
			}
		}
		switch {
		case pick == "":
			skipped = append(skipped, storagetools.BtrfsSkipped{Mountpoint: mounts[0],
				Reason: "mounted only under /home, /root or /run/user, which the read-only helper cannot open"})
		case len(targets) >= storagetools.MaxBtrfsFilesystems:
			truncated = true
			skipped = append(skipped, storagetools.BtrfsSkipped{Mountpoint: pick, Reason: "over the limit of filesystems inspected at once"})
		default:
			targets = append(targets, btrfsTarget{mountpoint: pick, mounts: mounts})
		}
	}
	return targets, skipped, truncated
}

// runBtrfs runs the fixed read-only btrfs-progs templates for each mounted
// Btrfs filesystem. A failing command is recorded on its filesystem; the
// others are still reported.
func runBtrfs(ctx context.Context, res *Result, fail func(string)) {
	if !res.Tools["btrfs"] {
		fail("btrfs is not installed (package btrfs-progs); no Btrfs data")
		return
	}
	recs, info, err := mountinfo.ReadFile(mountinfoPath)
	if err != nil {
		fail("cannot read the mount table: " + err.Error())
		return
	}
	targets, skipped, truncated := btrfsTargets(recs)
	rep := storagetools.BtrfsReport{Filesystems: []storagetools.BtrfsFilesystem{}, Skipped: skipped, Truncated: truncated || info.Truncated}
	for _, t := range targets {
		fs := storagetools.BtrfsFilesystem{Mountpoint: t.mountpoint, Mounts: t.mounts, Profiles: []storagetools.BtrfsProfile{},
			Devices: []storagetools.BtrfsDevice{}, Subvolumes: []storagetools.BtrfsSubvol{}}
		bad := func(what string, err error) { fs.Errors = append(fs.Errors, what+": "+err.Error()) }
		get := func(max int, args []string) ([]byte, error) {
			b, code, err := run(ctx, storagetools.HelperCommandTimeout, max, "btrfs", args)
			if err == nil && code != 0 {
				err = fmt.Errorf("exit status %d", code)
			}
			return b, err
		}
		if b, err := get(storagetools.MaxBtrfsBytes, storagetools.BtrfsUsageArgs(t.mountpoint)); err != nil {
			bad("filesystem usage", err)
		} else if fs.Overall, fs.Profiles, err = storagetools.ParseBtrfsUsage(b); err != nil {
			bad("filesystem usage", err)
		}
		if b, err := get(storagetools.MaxBtrfsBytes, storagetools.BtrfsDeviceStatsArgs(t.mountpoint)); err != nil {
			bad("device stats", err)
		} else if fs.Devices, err = storagetools.ParseBtrfsDeviceStats(b); err != nil {
			bad("device stats", err)
		}
		if b, err := get(storagetools.MaxBtrfsBytes, storagetools.BtrfsScrubStatusArgs(t.mountpoint)); err != nil {
			bad("scrub status", err)
		} else if fs.UUID, fs.Scrub, err = storagetools.ParseBtrfsScrub(b); err != nil {
			bad("scrub status", err)
		}
		if b, err := get(storagetools.MaxBtrfsSubvolBytes, storagetools.BtrfsSubvolumeListArgs(t.mountpoint)); err != nil {
			bad("subvolume list", err)
		} else if fs.Subvolumes, fs.SubvolumesTruncated, err = storagetools.ParseBtrfsSubvolumes(b); err != nil {
			bad("subvolume list", err)
		}
		if fs.Devices == nil {
			fs.Devices = []storagetools.BtrfsDevice{}
		}
		if fs.Subvolumes == nil {
			fs.Subvolumes = []storagetools.BtrfsSubvol{}
		}
		if fs.Profiles == nil {
			fs.Profiles = []storagetools.BtrfsProfile{}
		}
		if len(fs.Errors) > 0 {
			res.Partial = true
		}
		rep.Filesystems = append(rep.Filesystems, fs)
	}
	res.Btrfs = &rep
}

// mdstatPath is the kernel's md status file (a test seam).
var mdstatPath = "/proc/mdstat"

// runMd runs the fixed `mdadm --detail /dev/<name>` template for each array
// /proc/mdstat lists. An array mdadm cannot describe keeps its name and the
// error; the others are still reported.
func runMd(ctx context.Context, res *Result, fail func(string)) {
	if !res.Tools["mdadm"] {
		fail("mdadm is not installed (package mdadm); no array details")
		return
	}
	inv := mdadminfo.ReadMdstatFile(mdstatPath)
	if !inv.Present {
		fail(inv.Reason)
		return
	}
	rep := storagetools.MdReport{Arrays: []storagetools.MdArray{}, Truncated: inv.ArraysTruncated}
	for _, arr := range inv.Arrays {
		if !storagetools.ValidMdName(arr.Name) {
			continue
		}
		if len(rep.Arrays) >= storagetools.MaxMdArrays {
			rep.Truncated = true
			break
		}
		dev := "/dev/" + arr.Name
		b, code, err := run(ctx, storagetools.HelperCommandTimeout, storagetools.MaxMdBytes, "mdadm", storagetools.MdDetailArgs(arr.Name))
		if err == nil && code != 0 {
			err = fmt.Errorf("exit status %d", code)
		}
		var a storagetools.MdArray
		if err == nil {
			a, err = storagetools.ParseMdDetail(dev, b)
		}
		if err != nil {
			a = storagetools.MdArray{Device: dev, Level: arr.Level, State: arr.State, Members: []storagetools.MdMember{}, Error: "mdadm --detail: " + err.Error()}
			res.Partial = true
		}
		rep.Arrays = append(rep.Arrays, a)
	}
	res.Md = &rep
}

// admit rate-limits and caps concurrent Runs.
func admit() bool {
	select {
	case runSem <- struct{}{}:
	default:
		return false
	}
	runMu.Lock()
	ok := time.Since(lastRun) >= minRunInterval
	if ok {
		lastRun = time.Now()
	}
	runMu.Unlock()
	if !ok {
		<-runSem
	}
	return ok
}

func release() { <-runSem }

// run executes one registered tool with a fixed argument list, a scrubbed
// environment, a capped stdout and a timeout that kills the process group. It
// returns stdout and the exit code; a non-zero exit is not an error here
// (smartctl reports findings in its exit bits), but a timeout, oversized
// output or a failure to start is.
func run(ctx context.Context, timeout time.Duration, max int, tool string, args []string) ([]byte, int, error) {
	bin := binaries[tool]
	if bin == "" || !resolved[tool] {
		return nil, -1, fmt.Errorf("%s is not available", tool)
	}
	childCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(childCtx, bin, args...)
	cmd.Env = append([]string{}, env...)
	cmd.Stdin = nil
	stdout := cappedBuffer{max: max}
	stderr := cappedBuffer{max: maxStderrBytes}
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second

	err := cmd.Run()
	if stdout.tooBig {
		return nil, -1, fmt.Errorf("output exceeded %d bytes", max)
	}
	if childCtx.Err() != nil {
		return nil, -1, fmt.Errorf("timed out")
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout.buf.Bytes(), exitErr.ExitCode(), nil
	}
	if err != nil {
		return nil, -1, err
	}
	return stdout.buf.Bytes(), 0, nil
}

// cappedBuffer bounds the bytes a child may write.
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

// WriteResponse encodes res as one JSON frame and refuses a frame larger than
// maxResponseBytes before writing anything.
func WriteResponse(w io.Writer, res Result, maxResponseBytes int) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(res); err != nil {
		return err
	}
	b := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	if len(b) > maxResponseBytes {
		return fmt.Errorf("response exceeds %d bytes", maxResponseBytes)
	}
	_, err := w.Write(b)
	return err
}

// UnmarshalRequest parses a closed typed Request. It rejects unknown fields,
// duplicate fields, trailing data, and unsupported protocol versions.
func UnmarshalRequest(b []byte) (Request, error) {
	var req Request
	if err := rejectDupKeys(b); err != nil {
		return req, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return req, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return req, fmt.Errorf("trailing data after request")
	}
	if req.Version != storagetools.ProtocolVersion {
		return req, fmt.Errorf("unsupported protocol version %d", req.Version)
	}
	switch req.Scope {
	case "", storagetools.ScopeZFS, storagetools.ScopeBtrfs, storagetools.ScopeMd:
	default:
		return req, fmt.Errorf("unsupported scope %q", req.Scope)
	}
	return req, nil
}

// rejectDupKeys returns an error if the top-level JSON object repeats a key.
// encoding/json takes the last value, so duplicates are rejected explicitly.
func rejectDupKeys(b []byte) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("request must be a JSON object")
	}
	seen := map[string]bool{}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := keyTok.(string)
		if !ok {
			return fmt.Errorf("bad request key")
		}
		if seen[key] {
			return fmt.Errorf("duplicate field %q", key)
		}
		seen[key] = true
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
	}
	return nil
}
