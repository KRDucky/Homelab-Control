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
//     here. A device is resolved through /sys/class/block/<name>/dev to the stable
//     /dev/block/MAJ:MIN node, opened O_NOFOLLOW, and fstat-checked to be a
//     block device whose rdev matches the resolved MAJ:MIN.
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
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
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

// runSem caps concurrent helper Runs in-process (defense in depth). The
// socket unit serialises one connection at a time (Accept=yes, one instance
// per connection). minRunInterval rate-limits on-demand probes: blkid can wake
// standby disks.
var (
	runSem         = make(chan struct{}, 4)
	runMu          sync.Mutex
	lastRun        time.Time
	minRunInterval = time.Second
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

// env is the scrubbed child environment: no inherited PATH, secrets, locale or
// blkid/LVM state (BLKID_FILE, BLKID_DEBUG, LVM_SYSTEM_DIR are never present).
var env = []string{"LANG=C", "LC_ALL=C", "PATH=/usr/sbin:/usr/bin:/sbin:/bin"}

// sysBlockRoot is the sysfs block root used for device enumeration and
// revalidation. It is /sys/class/block (whole disks and partitions), and its
// entries are symlinks, so lookups use Stat, not Lstat. It is a var so
// synthetic tests can point it at a fixture tree.
var sysBlockRoot = "/sys/class/block"

// devBlockRoot is where the stable MAJ:MIN device nodes live (devtmpfs).
var devBlockRoot = "/dev/block"

// device is one enumerated, revalidated, non-skipped block device whose
// /dev/block/MAJ:MIN node was opened and verified. f is held open for the
// lifetime of the probe.
type device struct {
	name string
	path string
	f    *os.File
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
	Version int               `json:"version"`
	LVS     string            `json:"lvs,omitempty"`
	PVS     string            `json:"pvs,omitempty"`
	Blkid   map[string]string `json:"blkid,omitempty"`
	Errors  []string          `json:"errors,omitempty"`
	Partial bool              `json:"partial"`
}

// Run executes the fixed templates. Devices are revalidated by name, class-
// filtered, and TOCTOU-resolved before any command runs; an invalid, missing,
// skipped or unresolvable device is reported, not opened.
func Run(ctx context.Context, req Request) Result {
	res := Result{Version: 1, Blkid: map[string]string{}}
	if !admit() {
		res.Errors = append(res.Errors, "probe rate-limited or busy")
		res.Partial = true
		return res
	}
	defer release()

	var devs []*device
	for _, n := range validDevices(req.Devices) {
		if r := skipReason(n); r != "" {
			res.Errors = append(res.Errors, "skipped "+n+": "+r)
			continue
		}
		d, err := resolveDevice(n)
		if err != nil {
			res.Errors = append(res.Errors, "skip "+n+": "+err.Error())
			continue
		}
		devs = append(devs, d)
	}
	defer func() {
		for _, d := range devs {
			d.f.Close()
		}
	}()

	names := make([]string, len(devs))
	for i, d := range devs {
		names[i] = d.name
	}

	// LVM: private empty config dir + fixed filter so no host lvm.conf or
	// devices file influences the scan, and only enumerated devices are seen.
	lvmDir, err := os.MkdirTemp("", "hc-storage-probe-lvm-")
	if err != nil {
		res.Errors = append(res.Errors, "lvm: "+err.Error())
		res.Partial = true
		return res
	}
	defer os.RemoveAll(lvmDir)
	lvmEnv := []string{"LVM_SYSTEM_DIR=" + lvmDir}

	lvs, err := run(ctx, reportTimeout, maxReportBytes, "lvs", lvsArgs(names), lvmEnv)
	if err != nil {
		res.Errors = append(res.Errors, "lvs: "+err.Error())
		res.Partial = true
	}
	res.LVS = lvs

	pvs, err := run(ctx, reportTimeout, maxReportBytes, "pvs", pvsArgs(names), lvmEnv)
	if err != nil {
		res.Errors = append(res.Errors, "pvs: "+err.Error())
		res.Partial = true
	}
	res.PVS = pvs

	for _, d := range devs {
		before, hasSeq := diskseq(d.name)
		out, err := run(ctx, blkidTimeout, maxBlkidBytes, "blkid", blkidArgs(d.path), nil)
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

// lvsArgs is the fixed lvs template: read-only, hints disabled, fixed private
// device filter, byte units, all LVs with their segments, fixed fields.
func lvsArgs(names []string) []string {
	return []string{
		"--readonly", "--reportformat", "json", "--units", "b", "--nosuffix",
		"-a", "--segments", "-o", lvsFields, "--config", lvmConfig(names),
	}
}

// pvsArgs is the fixed pvs --segments template.
func pvsArgs(names []string) []string {
	return []string{
		"--readonly", "--segments", "--reportformat", "json", "--units", "b",
		"--nosuffix", "-o", pvsFields, "--config", lvmConfig(names),
	}
}

// lvmConfig builds a fixed LVM devices{} override: hints off and a filter that
// accepts only the enumerated device names and rejects everything else, so no
// host device file or other disk is ever scanned.
func lvmConfig(names []string) string {
	accept := make([]string, 0, len(names)+1)
	for _, n := range names {
		accept = append(accept, "a|^/dev/"+regexp.QuoteMeta(n)+"$|")
	}
	accept = append(accept, "r|.*|")
	return "devices{hints=0 filter=[" + strings.Join(accept, ",") + "]}"
}

// blkidArgs is the fixed blkid template: read-only probe, no cache read or
// write, export output, one already-resolved device node.
func blkidArgs(path string) []string {
	return []string{"-p", "-c", "/dev/null", "-o", "export", path}
}

// blkidAllowedKeys and blkidAllowedPrefixes are the allow-list for blkid -p -o
// export output; every other key is dropped so untrusted media strings never
// reach the caller unfiltered.
var (
	blkidAllowedKeys = map[string]bool{
		"DEVNAME": true, "TYPE": true, "USAGE": true, "VERSION": true,
		"PTTYPE": true, "PTUUID": true, "LABEL": true, "UUID": true,
		"PARTUUID": true, "PARTLABEL": true,
	}
	blkidAllowedPrefixes = []string{"PART_ENTRY_"}
)

func filterBlkidExport(raw string) string {
	var keep []string
	for _, line := range strings.Split(raw, "\n") {
		key, _, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		if blkidAllowedKeys[key] || hasAllowedPrefix(key) {
			keep = append(keep, line)
		}
	}
	return strings.Join(keep, "\n")
}

func hasAllowedPrefix(key string) bool {
	for _, p := range blkidAllowedPrefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

// validDevices keeps only syntactically valid names and revalidates them
// against sysBlockRoot so a stale or caller-supplied name is never opened.
func validDevices(names []string) []string {
	out := []string{}
	for _, n := range names {
		if !blockName.MatchString(n) || strings.Contains(n, "/") {
			continue
		}
		if fi, err := os.Stat(sysBlockRoot + "/" + n); err != nil || !fi.IsDir() {
			continue
		}
		out = append(out, n)
	}
	return out
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

// resolveDevice reads /sys/block/<name>/dev, opens the stable /dev/block
// MAJ:MIN node O_NOFOLLOW, and fstat-checks that it is a block device whose
// rdev matches. The open fd is kept to pin the resolved node for the probe.
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
	return &device{name: name, path: path, f: f}, nil
}

// openAndVerify opens path read-only without following symlinks and confirms
// it is a block device whose rdev equals the MAJ:MIN resolved from sysfs. It
// is a var so synthetic tests can inject a fake fd; production uses the real
// devtmpfs node.
var openAndVerify = func(path string, maj, min uint64) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
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
// process group.
func run(ctx context.Context, timeout time.Duration, max int, tool string, args, extraEnv []string) (string, error) {
	bin := binaries[tool]
	if bin == "" {
		return "", fmt.Errorf("%s is not registered", tool)
	}
	childCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(childCtx, bin, args...)
	cmd.Env = append(append([]string{}, env...), extraEnv...)
	cmd.Stdin = nil
	var out cappedBuffer
	out.max = max
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
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
