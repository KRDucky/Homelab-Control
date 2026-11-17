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
//     here. Device resolution is currently an inert, fail-closed proposal:
//     PrivateDevices=yes hides device nodes, and the helper does not pass a
//     validated open fd to child tools. Do not activate or wire it until O-1
//     resolves device identity and child-open semantics.
//   - The environment is replaced with a minimal, locale-forced set; LVM runs
//     against a private empty LVM_SYSTEM_DIR so no host lvm.conf or devices
//     file is read, and BLKID_FILE/BLKID_DEBUG are never inherited.
//   - LVM is constrained to the enumerated devices by a fixed filter, runs
//     --readonly with hints disabled; blkid probes read-only with no cache.
//     Nothing is written to a device or a mount.
//   - Each command has a size-capped output and a timeout that kills the
//     process group. At most one helper Run is admitted per rate window.
package storageprobe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"homelab-control/backend/internal/lvmmeta"
)

// Fixed resource bounds.
const (
	maxBlkidBytes  = 16 << 10 // per-device blkid cap: 64×16 KiB = 1 MiB raw, ~2 MiB encoded
	maxStderrBytes = 4 << 10  // bounded child stderr; never returned
	blkidTimeout   = 5 * time.Second
	maxDevices     = 64 // device names accepted per request
)

// runSem caps concurrent helper Runs in-process (defense in depth). The
// socket unit serialises one connection at a time (Accept=yes, one instance
// per connection). minRunInterval rate-limits on-demand probes: blkid can wake
// standby disks.
var (
	runSem         = make(chan struct{}, 4)
	runMu          sync.Mutex
	lastRun        time.Time
	minRunInterval = time.Second
	// totalTimeout bounds the whole Run across all devices. It is a var so
	// tests can shrink it; it must stay below the unit's TimeoutStartSec (30s)
	// so the helper reports a clean deadline instead of being SIGKILLed first.
	// The real cross-instance serialisation is the socket's MaxConnections.
	totalTimeout = 25 * time.Second

	// LVMEnabled gates lvs/pvs. It is false by default: lvs/pvs scan devices
	// by path and their fd identity binding is not proven, so an activation
	// path must keep LVM absent until that is demonstrated.
	LVMEnabled = false
)

// blockName is the kernel block-device name grammar (sdX, nvmeNnM, dm-N, …).
var blockName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.!:-]{0,254}$`)

// binaries maps a fixed tool to its absolute path. It is a var so synthetic
// tests can point at fake scripts; production resolves secure paths at startup
// via ResolveToolPaths.
var binaries = map[string]string{
	"blkid": "/usr/sbin/blkid",
}

// toolCandidates lists reviewed absolute paths per tool across distros, in
// preference order. Only these exact paths are ever accepted.
var toolCandidates = map[string][]string{
	"blkid": {"/usr/sbin/blkid", "/sbin/blkid", "/usr/bin/blkid"},
}

// ResolveToolPaths locates each tool at a reviewed path and verifies it is a
// regular, root-owned, non-writable-by-others file (no symlink escape). It
// fails closed if any tool is missing or insecure.
func ResolveToolPaths() error {
	for tool := range binaries {
		path, err := secureToolPath(toolCandidates[tool])
		if err != nil {
			return fmt.Errorf("%s: %w", tool, err)
		}
		binaries[tool] = path
	}
	return nil
}

// secureToolPath returns the first candidate whose resolved target and its
// directory chain are secure: a regular, root-owned, non-writable file. The
// original path is returned (not the resolved one) so a multi-call binary such
// as lvs -> lvm dispatches on argv[0] correctly; the resolved target was the
// thing verified.
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

// env is the scrubbed child environment: no inherited PATH, secrets, locale or
// blkid/LVM state (BLKID_FILE, BLKID_DEBUG, LVM_SYSTEM_DIR are never present).
var env = []string{"LANG=C", "LC_ALL=C", "PATH=/usr/sbin:/usr/bin:/sbin:/bin"}

// sysBlockRoot is the sysfs block root used for device enumeration and
// revalidation. It is /sys/class/block (whole disks and partitions), and its
// entries are symlinks, so lookups use Stat, not Lstat. It is a var so
// synthetic tests can point it at a fixture tree.
var sysBlockRoot = "/sys/class/block"

// devBlockRoot is udev's /dev/block MAJ:MIN namespace; entries may be symlinks.
var devBlockRoot = "/dev/block"

// procDevicesPath is the kernel's device-major registry. It is a var so
// synthetic tests can point it at a fixture.
var procDevicesPath = "/proc/devices"

// requiredBlockGroups are the reviewed block-device classes the helper is
// authorised to probe (owner-approved O-1 class-level DeviceAllow). The helper
// refuses to probe unless every one is present in /proc/devices.
var requiredBlockGroups = []string{"sd", "blkext", "virtblk", "md"}

// checkDeviceGroups reads /proc/devices and verifies every required block group
// is registered. It returns an error naming any missing group, so the helper
// can refuse (fail closed) before any probe.
func checkDeviceGroups() error {
	b, err := os.ReadFile(procDevicesPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", procDevicesPath, err)
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
	var missing []string
	for _, g := range requiredBlockGroups {
		if !present[g] {
			missing = append(missing, g)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("block device classes not available: %s", strings.Join(missing, ", "))
	}
	return nil
}

// device is one enumerated, revalidated, non-skipped block device whose
// /dev/block/MAJ:MIN node passed the helper's fstat block+rdev check. The fd
// is held and inherited by blkid so it reads this exact device; lvs/pvs remain
// path-based (see the design doc for that unresolved identity gap).
type device struct {
	name string
	path string
	f    *os.File
	size uint64 // device size in bytes (sysfs size * 512)
}

// Request is the closed typed input. Devices are kernel block names only.
type Request struct {
	Version int      `json:"version"`
	Devices []string `json:"devices"`
}

// Result is the closed typed output. LVS/PVS carry raw lvs/pvs --reportformat
// json reports; Blkid maps a device name to its allow-listed blkid -o export
// output.
type Result struct {
	Version int                `json:"version"`
	Code    string             `json:"code,omitempty"` // closed result/refusal code; "" = normal
	LVS     string             `json:"lvs,omitempty"`
	PVS     string             `json:"pvs,omitempty"`
	LVM     *lvmmeta.Inventory `json:"lvm,omitempty"` // native on-disk reader output (LVMEnabled)
	Blkid   map[string]string  `json:"blkid,omitempty"`
	Errors  []string           `json:"errors,omitempty"`
	Partial bool               `json:"partial"`
}

// Closed result codes. CodeDevicePolicyUnresolved is the typed fail-closed
// refusal when a required block-device class is absent from /proc/devices.
const CodeDevicePolicyUnresolved = "device_policy_unresolved"

// Run executes the fixed templates. Devices are revalidated and class-filtered,
// then opened and fstat-verified; blkid reads the pinned fd, lvs/pvs read the
// filtered device set. When identity cannot be verified (e.g. no device access
// in the current unit), the device is reported and the result is Partial.
func Run(ctx context.Context, req Request) Result {
	res := Result{Version: 1, Blkid: map[string]string{}}
	if !admit() {
		res.Errors = append(res.Errors, "probe rate-limited or busy")
		res.Partial = true
		return res
	}
	defer release()

	// Startup self-check: refuse (fail closed) before any probe unless every
	// reviewed block-device class is present in /proc/devices.
	if err := checkDeviceGroups(); err != nil {
		res.Code = CodeDevicePolicyUnresolved
		res.Errors = append(res.Errors, "probe refused: "+err.Error())
		res.Partial = true
		return res
	}

	// Request-wide deadline bounds total probe wall time across all devices.
	totalCtx, cancel := context.WithTimeout(ctx, totalTimeout)
	defer cancel()
	ctx = totalCtx

	names, dropped := validDevices(req.Devices)
	if len(req.Devices) > maxDevices {
		res.Errors = append(res.Errors, fmt.Sprintf("request truncated: %d devices, max %d", len(req.Devices), maxDevices))
		res.Partial = true
	}
	for _, n := range dropped {
		res.Errors = append(res.Errors, "invalid device "+strconv.Quote(n))
		res.Partial = true
	}

	var devs []*device
	for _, n := range names {
		if r := skipReason(n); r != "" {
			res.Errors = append(res.Errors, "skipped "+n+": "+r)
			res.Partial = true
			continue
		}
		d, err := resolveDevice(n)
		if err != nil {
			res.Errors = append(res.Errors, "skip "+n+": "+err.Error())
			res.Partial = true
			continue
		}
		devs = append(devs, d)
	}

	// Empty or fully-ineligible input must be an explicit partial result, not
	// a clean empty success.
	if len(req.Devices) == 0 {
		res.Errors = append(res.Errors, "no devices requested")
		res.Partial = true
	} else if len(devs) == 0 {
		res.Errors = append(res.Errors, "no eligible devices to probe")
		res.Partial = true
	}
	if len(devs) == 0 {
		return res // skip LVM/blkid entirely when nothing is probeable
	}
	defer func() {
		for _, d := range devs {
			d.f.Close()
		}
	}()

	// LVM is gated behind LVMEnabled. When enabled, the native read-only LVM
	// metadata reader parses the already-pinned fds (no lvs/pvs execution).
	if !LVMEnabled {
		res.Errors = append(res.Errors, "lvm disabled: native LVM reader is off")
		res.Partial = true
	} else {
		pvs := make([]lvmmeta.PVRead, 0, len(devs))
		budget := int64(lvmmeta.MaxTotalText)
		for _, d := range devs {
			before, hasSeq := diskseq(d.name)
			pv := lvmmeta.ReadPVBudgetContext(ctx, d.f, d.size, d.name, &budget)
			if !hasSeq {
				pv.Status = lvmmeta.StatusMetadataChanging // no diskseq: cannot bracket the read
			} else {
				after, ok := diskseq(d.name)
				if !ok || after != before {
					pv.Status = lvmmeta.StatusMetadataChanging
				}
			}
			pvs = append(pvs, pv)
			if pv.Status == lvmmeta.StatusLimitExceeded {
				res.Errors = append(res.Errors, "lvm metadata text exceeds MaxTotalText")
				res.Partial = true
				break
			}
			if pv.Status == lvmmeta.StatusCanceled {
				res.Errors = append(res.Errors, "lvm probe canceled")
				res.Partial = true
				break
			}
		}
		inv := lvmmeta.Assemble(pvs)
		res.LVM = &inv
		if len(inv.Notes) > 0 {
			res.Errors = append(res.Errors, inv.Notes...)
			res.Partial = true
		}
	}

	for _, d := range devs {
		before, hasSeq := diskseq(d.name)
		// blkid reads the exact opened device through the inherited fd, not a
		// re-resolved kernel-name path.
		out, err := run(ctx, blkidTimeout, maxBlkidBytes, "blkid", blkidArgs(blkidProcfd), nil, []*os.File{d.f})
		if err == nil && hasSeq {
			after, ok := diskseq(d.name)
			if !ok || after != before {
				err = fmt.Errorf("device replaced during probe (diskseq changed)")
			}
		}
		if err != nil {
			res.Errors = append(res.Errors, "blkid "+d.name+": "+err.Error())
			res.Partial = true
			continue
		}
		res.Blkid[d.name] = filterBlkidExport(out)
	}
	return res
}

// admit rate-limits and caps concurrent Runs. Cross-process serialisation is
// the socket unit's Accept=no; this is the in-process backstop.
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

// blkidProcfd is the path blkid receives for the device. The device fd is the
// first ExtraFile (child fd 3), so /proc/self/fd/3 re-opens that exact pinned
// device rather than a re-resolved kernel-name path.
const blkidProcfd = "/proc/self/fd/3"

// blkidArgs is the fixed blkid template: read-only probe, no cache read or
// write, export output, one pinned device node (a proc fd path).
func blkidArgs(path string) []string {
	return []string{"-p", "-c", "/dev/null", "-o", "export", path}
}

// blkidAllowedKeys and blkidAllowedPrefixes are the allow-list for blkid -p -o
// export output; every other key is dropped so untrusted media strings never
// reach the caller unfiltered. LABEL/PARTLABEL are deliberately excluded (raw
// on-disk, attacker-controlled strings).
var (
	blkidAllowedKeys = map[string]bool{
		"DEVNAME": true, "TYPE": true, "USAGE": true, "VERSION": true,
		"PTTYPE": true, "PTUUID": true, "UUID": true, "PARTUUID": true,
	}
	blkidAllowedPrefixes = []string{"PART_ENTRY_"}
)

// maxFieldValueBytes bounds any single blkid field value; longer or control-
// character-bearing values are dropped.
const maxFieldValueBytes = 255

func filterBlkidExport(raw string) string {
	var keep []string
	for _, line := range strings.Split(raw, "\n") {
		key, val, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		if !(blkidAllowedKeys[key] || hasAllowedPrefix(key)) {
			continue
		}
		if !cleanValue(val) {
			continue
		}
		keep = append(keep, line)
	}
	return strings.Join(keep, "\n")
}

// cleanValue reports whether a blkid field value is safe to return: bounded
// and free of control bytes.
func cleanValue(v string) bool {
	if len(v) > maxFieldValueBytes {
		return false
	}
	for i := 0; i < len(v); i++ {
		if v[i] < 0x20 || v[i] == 0x7f {
			return false
		}
	}
	return true
}

func hasAllowedPrefix(key string) bool {
	for _, p := range blkidAllowedPrefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

// validDevices bounds the request: at most maxDevices valid names are kept,
// names are de-duplicated, and syntactically invalid or missing devices are
// returned in dropped so Run can report them. Work is also bounded for large
// invalid/duplicate input.
func validDevices(names []string) (valid, dropped []string) {
	seen := make(map[string]bool, maxDevices)
	valid = make([]string, 0, maxDevices)
	limit := maxDevices * 2 // bound total work even for invalid/duplicate input
	if len(names) < limit {
		limit = len(names)
	}
	for _, n := range names[:limit] {
		if len(valid) >= maxDevices {
			break
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		if !blockName.MatchString(n) || strings.Contains(n, "/") {
			dropped = append(dropped, n)
			continue
		}
		if fi, err := os.Stat(sysBlockRoot + "/" + n); err != nil || !fi.IsDir() {
			dropped = append(dropped, n)
			continue
		}
		valid = append(valid, n)
	}
	return valid, dropped
}

// skipReason reports why a device is excluded by default, or "" when it is
// eligible. loop/zram/ram/sr/fd/nbd/rbd and device-mapper (multipath, LVM,
// crypt) are never probed; removable and USB media are skipped; a device whose
// sysfs state is not running is skipped. Partitions inherit their whole disk's
// classification.
func skipReason(name string) string {
	disk := wholeDisk(name)
	for _, p := range []string{"loop", "zram", "ram", "sr", "fd", "nbd", "rbd", "dm-"} {
		if strings.HasPrefix(disk, p) {
			return "device class " + p + " excluded"
		}
	}
	if readTrim(sysBlockRoot+"/"+disk+"/removable") == "1" {
		return "removable"
	}
	if state := readTrim(sysBlockRoot + "/" + disk + "/device/state"); state != "" && state != "running" {
		return "device state " + state
	}
	if dev, err := filepath.EvalSymlinks(sysBlockRoot + "/" + disk + "/device"); err == nil {
		if hasPathComponent(dev, "usb") {
			return "usb"
		}
	}
	return ""
}

// wholeDisk returns the whole-disk kernel name for a device: a partition is
// resolved to its parent disk through the sysfs link (…/block/<disk>/<part>),
// so removable/USB/state checks apply to the disk rather than the partition.
func wholeDisk(name string) string {
	if readTrim(sysBlockRoot+"/"+name+"/partition") == "" {
		return name
	}
	target, err := os.Readlink(sysBlockRoot + "/" + name)
	if err != nil {
		return name
	}
	return filepath.Base(filepath.Dir(target))
}

// resolveDevice reads /sys/class/block/<name>/dev, opens the stable
// /dev/block MAJ:MIN node (following the symlink), and fstat-checks the opened
// fd is a block device whose rdev matches. The fd is held so blkid reads this
// exact device; lvs/pvs remain path-based (see docs for that unresolved gap).
func resolveDevice(name string) (*device, error) {
	dev, err := readTrimErr(sysBlockRoot + "/" + name + "/dev")
	if err != nil {
		return nil, fmt.Errorf("no dev node: %w", err)
	}
	parts := strings.Split(dev, ":")
	if len(parts) != 2 {
		return nil, fmt.Errorf("bad dev node %q", dev)
	}
	maj, err1 := strconv.ParseUint(parts[0], 10, 64)
	min, err2 := strconv.ParseUint(parts[1], 10, 64)
	if err1 != nil || err2 != nil {
		return nil, fmt.Errorf("bad dev node %q", dev)
	}
	path := fmt.Sprintf("%s/%d:%d", devBlockRoot, maj, min)
	f, err := openAndVerify(path, maj, min)
	if err != nil {
		return nil, err
	}
	return &device{name: name, path: path, f: f, size: deviceSizeBytes(name)}, nil
}

// deviceSizeBytes reads /sys/class/block/<name>/size (512-byte sectors) and
// returns the device size in bytes. It is 0 when unreadable.
func deviceSizeBytes(name string) uint64 {
	sectors, err := strconv.ParseUint(readTrim(sysBlockRoot+"/"+name+"/size"), 10, 64)
	if err != nil {
		return 0
	}
	return sectors * 512
}

// openAndVerify opens path read-only, following the /dev/block symlink to the
// real devtmpfs node, and confirms the opened fd is a block device whose rdev
// equals the MAJ:MIN resolved from sysfs. Identity is pinned to the opened fd,
// not the path. It is a var so synthetic tests can inject a fake fd.
var openAndVerify = func(path string, maj, min uint64) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		f.Close()
		return nil, fmt.Errorf("not a linux stat")
	}
	if st.Mode&syscall.S_IFMT != syscall.S_IFBLK {
		f.Close()
		return nil, fmt.Errorf("not a block device")
	}
	if st.Rdev != makedev(maj, min) {
		f.Close()
		return nil, fmt.Errorf("device number mismatch")
	}
	return f, nil
}

// makedev mirrors the Linux/glibc device-number encoding so an rdev from
// stat(2) can be compared to the MAJ:MIN text read from sysfs.
func makedev(major, minor uint64) uint64 {
	return ((major & 0xfffff000) << 32) |
		((major & 0x00000fff) << 8) |
		((minor & 0xffffff00) << 12) |
		(minor & 0x000000ff)
}

// diskseq returns the kernel block-device generation counter and whether the
// kernel exposes it (>= 5.12). A change between before and after a probe means
// the device was replaced mid-flight.
func diskseq(name string) (string, bool) {
	b, err := os.ReadFile(sysBlockRoot + "/" + wholeDisk(name) + "/diskseq")
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

// run executes one fixed binary with the given args, a scrubbed environment
// plus extraEnv, a size-capped stdout and a timeout that kills the whole
// process group. extraFiles are inherited as child fds 3.. so a tool can read
// the exact opened device through /proc/self/fd/N instead of re-resolving a
// kernel-name path.
func run(ctx context.Context, timeout time.Duration, max int, tool string, args, extraEnv []string, extraFiles []*os.File) (string, error) {
	bin := binaries[tool]
	if bin == "" {
		return "", fmt.Errorf("%s is not registered", tool)
	}
	childCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(childCtx, bin, args...)
	cmd.Env = append(append([]string{}, env...), extraEnv...)
	cmd.ExtraFiles = extraFiles
	cmd.Stdin = nil
	var stdout cappedBuffer
	stdout.max = max
	var stderr cappedBuffer
	stderr.max = maxStderrBytes
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second

	err := cmd.Run()
	if stdout.tooBig {
		return "", fmt.Errorf("%s output exceeded %d bytes", tool, max)
	}
	if err != nil {
		if childCtx.Err() != nil {
			return "", fmt.Errorf("%s timed out", tool)
		}
		return "", fmt.Errorf("%s exited: %w", tool, err)
	}
	return stdout.buf.String(), nil
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

// WriteResponse encodes res to w, rejecting the result before encoding when
// the exact encoded-length preflight exceeds the frame cap. The encoder writes
// one completed JSON frame directly to a capped writer, avoiding a second
// response-sized buffer.
func WriteResponse(w io.Writer, res Result, maxResponseBytes int) error {
	if maxResponseBytes < 0 || preflightBytes(res) > maxResponseBytes {
		return fmt.Errorf("response exceeds %d bytes", maxResponseBytes)
	}
	enc := json.NewEncoder(&cappedFrameWriter{w: w, max: maxResponseBytes})
	enc.SetEscapeHTML(false)
	return enc.Encode(res)
}

// cappedFrameWriter strips Encoder.Encode's trailing newline and refuses the
// complete frame before forwarding it if it exceeds the protocol limit.
type cappedFrameWriter struct {
	w   io.Writer
	max int
}

func (c *cappedFrameWriter) Write(p []byte) (int, error) {
	if len(p) > 0 && p[len(p)-1] == '\n' {
		p = p[:len(p)-1]
	}
	if len(p) > c.max {
		return 0, fmt.Errorf("response exceeds %d bytes", c.max)
	}
	n, err := c.w.Write(p)
	if err == nil && n != len(p) {
		return n, io.ErrShortWrite
	}
	return n, err
}

// preflightBytes is a safe upper bound on the marshalled size with HTML
// escaping disabled: the exact encoded length of every string plus a generous
// fixed-syntax allowance and per-entry separator over-count.
func preflightBytes(res Result) int {
	const syntax = 128
	n := syntax
	n += jsonStringLen(res.LVS)
	n += jsonStringLen(res.PVS)
	for k, v := range res.Blkid {
		n += jsonStringLen(k) + jsonStringLen(v) + 3 // key : value ,
	}
	for _, e := range res.Errors {
		n += jsonStringLen(e) + 2 // value ,
	}
	if res.LVM != nil {
		// The native LVM inventory is a struct, not a string; marshal it to
		// bound its contribution (it is already capped by the parse limits).
		if b, err := json.Marshal(res.LVM); err == nil {
			n += len(b)
		}
	}
	return n
}

// jsonStringLen returns the exact number of bytes encoding/json would emit for
// s as a JSON string (including the surrounding quotes) with HTML escaping
// disabled. It mirrors appendString: invalid UTF-8 bytes and control bytes
// become six-byte \u00XX (or \ufffd), quotes/backslashes and the common
// escapes are two bytes, U+2028/U+2029 are always escaped to six bytes, and
// everything else is emitted as its UTF-8 bytes.
func jsonStringLen(s string) int {
	n := 2 // opening and closing quotes
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			n += 6 // \ufffd
		case r == '"' || r == '\\' || r == '\b' || r == '\f' || r == '\n' || r == '\r' || r == '\t':
			n += 2
		case r < 0x20:
			n += 6 // \u00XX
		case r == '\u2028' || r == '\u2029':
			n += 6
		default:
			n += size
		}
		i += size
	}
	return n
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
	if req.Version != 1 {
		return req, fmt.Errorf("unsupported protocol version %d", req.Version)
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

// readTrim reads a sysfs attribute, returning "" on any error.
func readTrim(path string) string {
	s, _ := readTrimErr(path)
	return s
}

func readTrimErr(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func hasPathComponent(path, comp string) bool {
	for _, p := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		if p == comp {
			return true
		}
	}
	return false
}
