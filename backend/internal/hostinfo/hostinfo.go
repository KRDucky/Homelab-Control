// Package hostinfo collects a read-only host overview from procfs, sysfs and
// /etc using only unprivileged file reads and statfs(2). It never executes
// external programs and never writes to the host.
//
// Every section reports its data source and a status so the UI can show
// "unavailable" or "unsupported" honestly instead of inventing values.
package hostinfo

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Status values for each section.
const (
	StatusOK          = "ok"
	StatusUnavailable = "unavailable"
	StatusPartial     = "partial"
)

// Roots allows tests to point the collector at fixture trees.
type Roots struct {
	Proc string // normally /proc
	Sys  string // normally /sys
	Etc  string // normally /etc
}

// DefaultRoots are the live host paths.
var DefaultRoots = Roots{Proc: "/proc", Sys: "/sys", Etc: "/etc"}

// Section metadata common to all parts of the overview.
type Meta struct {
	Status  string   `json:"status"`
	Sources []string `json:"sources"`
	Errors  []string `json:"errors,omitempty"`
}

type System struct {
	Meta
	Hostname      string     `json:"hostname,omitempty"`
	Kernel        string     `json:"kernel,omitempty"`
	OS            OSRelease  `json:"os"`
	DistroFamily  string     `json:"distro_family"`
	UptimeSeconds float64    `json:"uptime_seconds,omitempty"`
	LoadAverage   [3]float64 `json:"load_average"`
}

type OSRelease struct {
	ID         string `json:"id,omitempty"`
	IDLike     string `json:"id_like,omitempty"`
	Name       string `json:"name,omitempty"`
	PrettyName string `json:"pretty_name,omitempty"`
	VersionID  string `json:"version_id,omitempty"`
}

type CPU struct {
	Meta
	Model        string   `json:"model,omitempty"`
	LogicalCPUs  int      `json:"logical_cpus"`
	UsagePercent *float64 `json:"usage_percent,omitempty"`
	SampleMillis int      `json:"sample_millis"`
}

type Memory struct {
	Meta
	TotalBytes     uint64 `json:"total_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
	UsedBytes      uint64 `json:"used_bytes"`
	SwapTotalBytes uint64 `json:"swap_total_bytes"`
	SwapFreeBytes  uint64 `json:"swap_free_bytes"`
}

type Filesystem struct {
	// KernelSource is the mount source exactly as reported by the kernel
	// (e.g. /dev/sda1 or pool/dataset). /dev/sdX names are volatile and must
	// not be used as persistent drive identity.
	KernelSource string `json:"kernel_source"`
	MountPoint   string `json:"mount_point"`
	FSType       string `json:"fs_type"`
	ReadOnly     bool   `json:"read_only"`
	Probed       bool   `json:"probed"`
	SkipReason   string `json:"skip_reason,omitempty"`
	TotalBytes   uint64 `json:"total_bytes,omitempty"`
	UsedBytes    uint64 `json:"used_bytes,omitempty"`
	AvailBytes   uint64 `json:"avail_bytes,omitempty"`
}

type Filesystems struct {
	Meta
	// View is "host" when the table came from the host's mount namespace
	// (/proc/1/mounts), or "service" when only hc-api's own sandboxed view was
	// readable. In the service view systemd's ProtectSystem/PrivateTmp/
	// LogsDirectory make mounts look read-only and add bind mounts that do
	// not exist on the host, so read-only flags are withheld and Note says why.
	View  string       `json:"view"`
	Note  string       `json:"note,omitempty"`
	Items []Filesystem `json:"items"`
}

type Interface struct {
	Name      string   `json:"name"`
	OperState string   `json:"oper_state,omitempty"`
	MAC       string   `json:"mac,omitempty"`
	MTU       int      `json:"mtu,omitempty"`
	Physical  bool     `json:"physical"`
	SpeedMbps *int     `json:"speed_mbps,omitempty"`
	RxBytes   uint64   `json:"rx_bytes"`
	TxBytes   uint64   `json:"tx_bytes"`
	Addresses []string `json:"addresses,omitempty"`
}

type Network struct {
	Meta
	Interfaces []Interface `json:"interfaces"`
}

// Legacy describes what can be observed about an installed upstream Webmin
// without privileges. miniserv.conf is root-only (0600) on standard installs,
// so port/SSL are usually reported as requiring privilege.
type Legacy struct {
	Meta
	Installed       bool   `json:"installed"`
	Version         string `json:"version,omitempty"`
	ConfigReadable  bool   `json:"config_readable"`
	ConfiguredURL   string `json:"configured_url,omitempty"`
	PrivilegeNeeded string `json:"privilege_needed,omitempty"`
}

// Overview is the full read-only response for GET /api/v1/host/overview.
type Overview struct {
	CollectedAt time.Time   `json:"collected_at"`
	System      System      `json:"system"`
	CPU         CPU         `json:"cpu"`
	Memory      Memory      `json:"memory"`
	Filesystems Filesystems `json:"filesystems"`
	Network     Network     `json:"network"`
	Legacy      Legacy      `json:"legacy_webmin"`
}

// Collector gathers the overview.
type Collector struct {
	Roots     Roots
	CPUSample time.Duration
	LegacyURL string
	// Statfs is replaceable in tests.
	Statfs func(path string) (total, used, avail uint64, err error)
	// StatfsTimeout overrides the default per-call timeout (tests).
	StatfsTimeout time.Duration
	// Addrs is replaceable in tests; returns interface addresses.
	Addrs func(name string) []string
}

// NewCollector returns a collector for the live host.
func NewCollector(legacyURL string) *Collector {
	return &Collector{Roots: DefaultRoots, CPUSample: 250 * time.Millisecond, LegacyURL: legacyURL, Statfs: statfs, Addrs: liveAddrs}
}

// Collect builds a complete overview. It never fails as a whole; failures are
// reported per section.
func (c *Collector) Collect() Overview {
	return Overview{
		CollectedAt: time.Now().UTC(),
		System:      c.system(),
		CPU:         c.cpu(),
		Memory:      c.memory(),
		Filesystems: c.filesystems(),
		Network:     c.network(),
		Legacy:      c.legacy(),
	}
}

const maxRead = 4 << 20 // bound every read (mount tables on container hosts can be large).

func readSmall(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// procfs/seq_file may return a large file over several reads; read to EOF
	// (bounded) so long mount tables are never silently truncated.
	b, err := io.ReadAll(io.LimitReader(f, maxRead+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxRead {
		return nil, fmt.Errorf("%s: larger than %d bytes", path, maxRead)
	}
	return b, nil
}

func readTrim(path string) (string, error) {
	b, err := readSmall(path)
	return strings.TrimSpace(string(b)), err
}

func finish(m *Meta) {
	switch {
	case len(m.Errors) == 0:
		m.Status = StatusOK
	case m.Status == "":
		m.Status = StatusPartial
	}
}

func (c *Collector) system() System {
	s := System{}
	p := filepath.Join(c.Roots.Proc, "sys/kernel/hostname")
	s.Sources = append(s.Sources, p)
	if v, err := readTrim(p); err == nil {
		s.Hostname = v
	} else {
		s.Errors = append(s.Errors, err.Error())
	}
	p = filepath.Join(c.Roots.Proc, "sys/kernel/osrelease")
	s.Sources = append(s.Sources, p)
	if v, err := readTrim(p); err == nil {
		s.Kernel = v
	} else {
		s.Errors = append(s.Errors, err.Error())
	}
	osr, src, err := ReadOSRelease(c.Roots.Etc)
	s.Sources = append(s.Sources, src)
	if err != nil {
		s.Errors = append(s.Errors, err.Error())
	}
	s.OS = osr
	s.DistroFamily = DistroFamily(osr)
	p = filepath.Join(c.Roots.Proc, "uptime")
	s.Sources = append(s.Sources, p)
	if v, err := readTrim(p); err == nil {
		if f := strings.Fields(v); len(f) > 0 {
			s.UptimeSeconds, _ = strconv.ParseFloat(f[0], 64)
		}
	} else {
		s.Errors = append(s.Errors, err.Error())
	}
	p = filepath.Join(c.Roots.Proc, "loadavg")
	s.Sources = append(s.Sources, p)
	if v, err := readTrim(p); err == nil {
		f := strings.Fields(v)
		for i := 0; i < 3 && i < len(f); i++ {
			s.LoadAverage[i], _ = strconv.ParseFloat(f[i], 64)
		}
	} else {
		s.Errors = append(s.Errors, err.Error())
	}
	finish(&s.Meta)
	return s
}

// ReadOSRelease parses os-release(5), preferring /etc/os-release and falling
// back to /usr/lib/os-release (resolved relative to the etc root's parent).
func ReadOSRelease(etcRoot string) (OSRelease, string, error) {
	candidates := []string{filepath.Join(etcRoot, "os-release"), filepath.Join(filepath.Dir(etcRoot), "usr/lib/os-release")}
	var lastErr error
	for _, p := range candidates {
		b, err := readSmall(p)
		if err != nil {
			lastErr = err
			continue
		}
		return ParseOSRelease(b), p, nil
	}
	return OSRelease{}, candidates[0], lastErr
}

// ParseOSRelease parses KEY=VALUE lines with optional shell-style quoting.
func ParseOSRelease(b []byte) OSRelease {
	kv := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
			v = strings.NewReplacer(`\"`, `"`, `\\`, `\`, `\$`, `$`, "\\`", "`").Replace(v)
		}
		kv[k] = v
	}
	return OSRelease{ID: kv["ID"], IDLike: kv["ID_LIKE"], Name: kv["NAME"], PrettyName: kv["PRETTY_NAME"], VersionID: kv["VERSION_ID"]}
}

// DistroFamily maps os-release to the package families Homelab Control targets.
// Unknown distributions are reported as "unknown", never guessed.
func DistroFamily(o OSRelease) string {
	ids := append([]string{o.ID}, strings.Fields(o.IDLike)...)
	for _, id := range ids {
		switch id {
		case "debian", "ubuntu":
			return "debian"
		case "fedora", "rhel", "centos":
			return "fedora"
		case "opensuse", "suse", "opensuse-leap", "opensuse-tumbleweed", "sles":
			return "suse"
		case "arch":
			return "arch"
		}
	}
	return "unknown"
}

type cpuTimes struct{ idle, total uint64 }

func readCPUTimes(path string) (cpuTimes, error) {
	b, err := readSmall(path)
	if err != nil {
		return cpuTimes{}, err
	}
	line, _, _ := strings.Cut(string(b), "\n")
	f := strings.Fields(line)
	if len(f) < 5 || f[0] != "cpu" {
		return cpuTimes{}, errors.New("unexpected /proc/stat format")
	}
	var t cpuTimes
	for i, v := range f[1:] {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return cpuTimes{}, err
		}
		// guest and guest_nice (fields 9,10) are already included in user/nice.
		if i >= 8 {
			break
		}
		t.total += n
		if i == 3 || i == 4 { // idle + iowait
			t.idle += n
		}
	}
	return t, nil
}

func (c *Collector) cpu() CPU {
	out := CPU{SampleMillis: int(c.CPUSample / time.Millisecond)}
	p := filepath.Join(c.Roots.Proc, "cpuinfo")
	out.Sources = append(out.Sources, p)
	if b, err := readSmall(p); err == nil {
		out.Model, out.LogicalCPUs = ParseCPUInfo(b)
	} else {
		out.Errors = append(out.Errors, err.Error())
	}
	p = filepath.Join(c.Roots.Proc, "stat")
	out.Sources = append(out.Sources, p)
	a, err := readCPUTimes(p)
	if err == nil && c.CPUSample > 0 {
		time.Sleep(c.CPUSample)
		var b cpuTimes
		if b, err = readCPUTimes(p); err == nil && b.total > a.total {
			busy := float64((b.total-a.total)-(b.idle-a.idle)) / float64(b.total-a.total) * 100
			busy = float64(int(busy*10+0.5)) / 10
			out.UsagePercent = &busy
		}
	}
	if err != nil {
		out.Errors = append(out.Errors, err.Error())
	}
	finish(&out.Meta)
	return out
}

// ParseCPUInfo returns the first model name and the number of logical CPUs.
func ParseCPUInfo(b []byte) (string, int) {
	model, n := "", 0
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "processor":
			n++
		case "model name", "Model", "cpu model":
			if model == "" {
				model = v
			}
		}
	}
	return model, n
}

func (c *Collector) memory() Memory {
	m := Memory{}
	p := filepath.Join(c.Roots.Proc, "meminfo")
	m.Sources = []string{p}
	b, err := readSmall(p)
	if err != nil {
		m.Errors = append(m.Errors, err.Error())
		m.Status = StatusUnavailable
		return m
	}
	kv := ParseMeminfo(b)
	m.TotalBytes = kv["MemTotal"]
	avail, ok := kv["MemAvailable"]
	if !ok { // kernels < 3.14
		avail = kv["MemFree"] + kv["Buffers"] + kv["Cached"]
		m.Errors = append(m.Errors, "MemAvailable missing; approximated from MemFree+Buffers+Cached")
	}
	m.AvailableBytes = avail
	if m.TotalBytes >= avail {
		m.UsedBytes = m.TotalBytes - avail
	}
	m.SwapTotalBytes, m.SwapFreeBytes = kv["SwapTotal"], kv["SwapFree"]
	if m.TotalBytes == 0 {
		m.Errors = append(m.Errors, "MemTotal missing")
	}
	finish(&m.Meta)
	return m
}

// ParseMeminfo returns /proc/meminfo values converted to bytes.
func ParseMeminfo(b []byte) map[string]uint64 {
	out := map[string]uint64{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		f := strings.Fields(v)
		if len(f) == 0 {
			continue
		}
		n, err := strconv.ParseUint(f[0], 10, 64)
		if err != nil {
			continue
		}
		if len(f) > 1 && f[1] == "kB" {
			n *= 1024
		}
		out[k] = n
	}
	return out
}

// Local filesystem types probed with statfs. Network and FUSE filesystems
// other than mergerfs are listed but not probed, because statfs on an
// unresponsive remote mount can block indefinitely.
var localFS = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true, "xfs": true, "btrfs": true, "zfs": true,
	"vfat": true, "exfat": true, "ntfs3": true, "ntfs": true, "f2fs": true, "bcachefs": true,
	"jfs": true, "reiserfs": true, "fuse.mergerfs": true,
}
var networkFS = map[string]bool{"nfs": true, "nfs4": true, "cifs": true, "smb3": true, "fuse.sshfs": true, "fuse.rclone": true, "9p": true, "virtiofs": false, "ceph": true, "glusterfs": true}

// MountEntry is one parsed /proc/self/mounts line.
type MountEntry struct {
	Source, MountPoint, FSType string
	ReadOnly                   bool
}

// unescapeMount decodes the octal escapes used by the kernel (\040 etc.).
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// ParseMounts parses /proc/self/mounts content.
func ParseMounts(b []byte) []MountEntry {
	var out []MountEntry
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 {
			continue
		}
		ro := false
		for _, o := range strings.Split(f[3], ",") {
			if o == "ro" {
				ro = true
			}
		}
		out = append(out, MountEntry{Source: unescapeMount(f[0]), MountPoint: unescapeMount(f[1]), FSType: f[2], ReadOnly: ro})
	}
	return out
}

func (c *Collector) filesystems() Filesystems {
	fs := Filesystems{Items: []Filesystem{}, View: "host"}
	// hc-api runs sandboxed (ProtectSystem=strict, PrivateTmp, LogsDirectory):
	// its own /proc/self/mounts describes the sandbox, not the host. PID 1's
	// table is the host's (or the container's) real one and is world-readable
	// unless /proc is mounted with hidepid or ProtectProc is set.
	p := filepath.Join(c.Roots.Proc, "1/mounts")
	b, err := readSmall(p)
	if err != nil {
		p = filepath.Join(c.Roots.Proc, "self/mounts")
		fs.View = "service"
		fs.Note = "The host mount table (/proc/1/mounts) is not readable here, so this is the service's own sandboxed view: " +
			"read-only flags are not shown because the sandbox makes filesystems look read-only, and some entries may be service-only bind mounts."
		b, err = readSmall(p)
	}
	fs.Sources = []string{p, "statfs(2)"}
	if err != nil {
		fs.Errors = append(fs.Errors, err.Error())
		fs.Status = StatusUnavailable
		return fs
	}
	seen := map[string]bool{}
	for _, m := range ParseMounts(b) {
		// The root filesystem is always reported, whatever its type: live,
		// immutable and container hosts commonly mount / as overlay.
		local := localFS[m.FSType] || (m.MountPoint == "/" && !networkFS[m.FSType])
		remote := networkFS[m.FSType]
		if !local && !remote {
			continue // pseudo filesystems (proc, sysfs, tmpfs, overlay, ...)
		}
		if seen[m.MountPoint] {
			continue // keep the first (lowest) entry for stacked mounts
		}
		seen[m.MountPoint] = true
		item := Filesystem{KernelSource: m.Source, MountPoint: m.MountPoint, FSType: m.FSType, ReadOnly: m.ReadOnly && fs.View == "host"}
		if remote {
			item.SkipReason = "network filesystem not probed (statfs could block)"
		} else if c.Statfs != nil {
			total, used, avail, err := c.statfsBounded(m.MountPoint)
			if err != nil {
				item.SkipReason = "statfs failed: " + err.Error()
			} else {
				item.Probed, item.TotalBytes, item.UsedBytes, item.AvailBytes = true, total, used, avail
			}
		}
		fs.Items = append(fs.Items, item)
	}
	sort.Slice(fs.Items, func(i, j int) bool { return fs.Items[i].MountPoint < fs.Items[j].MountPoint })
	finish(&fs.Meta)
	return fs
}

// statfsTimeout bounds each statfs call. A FUSE filesystem (e.g. mergerfs)
// with an unresponsive branch can block statfs indefinitely.
const statfsTimeout = 2 * time.Second

var (
	inflightMu sync.Mutex
	inflight   = map[string]bool{}
)

// errStatfsTimeout is reported when a statfs call does not return in time.
var errStatfsTimeout = errors.New("timed out (filesystem not responding)")

// statfsBounded runs Statfs with a timeout. A call that is still blocked from
// an earlier request is not retried, so stuck mounts cannot accumulate
// goroutines.
func (c *Collector) statfsBounded(path string) (uint64, uint64, uint64, error) {
	inflightMu.Lock()
	if inflight[path] {
		inflightMu.Unlock()
		return 0, 0, 0, errStatfsTimeout
	}
	inflight[path] = true
	inflightMu.Unlock()
	type res struct {
		t, u, a uint64
		err     error
	}
	ch := make(chan res, 1)
	go func() {
		t, u, a, err := c.Statfs(path)
		inflightMu.Lock()
		delete(inflight, path)
		inflightMu.Unlock()
		ch <- res{t, u, a, err}
	}()
	timeout := c.StatfsTimeout
	if timeout <= 0 {
		timeout = statfsTimeout
	}
	select {
	case r := <-ch:
		return r.t, r.u, r.a, r.err
	case <-time.After(timeout):
		return 0, 0, 0, errStatfsTimeout
	}
}

func statfs(path string) (uint64, uint64, uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, 0, err
	}
	bs := uint64(st.Bsize)
	total := st.Blocks * bs
	free := st.Bfree * bs
	return total, total - free, st.Bavail * bs, nil
}

func liveAddrs(name string) []string {
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return nil
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.String())
	}
	return out
}

func readUint(path string) (uint64, error) {
	v, err := readTrim(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(v, 10, 64)
}

func (c *Collector) network() Network {
	n := Network{Interfaces: []Interface{}}
	base := filepath.Join(c.Roots.Sys, "class/net")
	n.Sources = []string{base}
	entries, err := os.ReadDir(base)
	if err != nil {
		n.Errors = append(n.Errors, err.Error())
		n.Status = StatusUnavailable
		return n
	}
	for _, e := range entries {
		name := e.Name()
		if strings.ContainsAny(name, "/\x00") || name == "." || name == ".." {
			continue
		}
		dir := filepath.Join(base, name)
		ifc := Interface{Name: name}
		ifc.OperState, _ = readTrim(filepath.Join(dir, "operstate"))
		ifc.MAC, _ = readTrim(filepath.Join(dir, "address"))
		if mtu, err := readUint(filepath.Join(dir, "mtu")); err == nil {
			ifc.MTU = int(mtu)
		}
		if _, err := os.Stat(filepath.Join(dir, "device")); err == nil {
			ifc.Physical = true
		}
		if v, err := readTrim(filepath.Join(dir, "speed")); err == nil {
			if s, err := strconv.Atoi(v); err == nil && s > 0 {
				ifc.SpeedMbps = &s
			}
		}
		ifc.RxBytes, _ = readUint(filepath.Join(dir, "statistics/rx_bytes"))
		ifc.TxBytes, _ = readUint(filepath.Join(dir, "statistics/tx_bytes"))
		if c.Addrs != nil {
			ifc.Addresses = c.Addrs(name)
		}
		n.Interfaces = append(n.Interfaces, ifc)
	}
	finish(&n.Meta)
	return n
}

// LegacyOnly returns just the unprivileged legacy-Webmin detection.
func (c *Collector) LegacyOnly() Legacy { return c.legacy() }

func (c *Collector) legacy() Legacy {
	l := Legacy{ConfiguredURL: c.LegacyURL}
	conf := filepath.Join(c.Roots.Etc, "webmin/miniserv.conf")
	ver := filepath.Join(c.Roots.Etc, "webmin/version")
	l.Sources = []string{conf, ver}
	if _, err := os.Lstat(conf); err == nil {
		l.Installed = true
		if f, err := os.Open(conf); err == nil {
			l.ConfigReadable = true
			f.Close()
		} else if errors.Is(err, os.ErrPermission) {
			l.PrivilegeNeeded = "miniserv.conf is root-only; port/TLS details require the privileged broker (planned, M1/M2)"
		} else {
			l.Errors = append(l.Errors, err.Error())
		}
		if v, err := readTrim(ver); err == nil {
			l.Version = v
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		l.Errors = append(l.Errors, fmt.Sprintf("stat %s: %v", conf, err))
	}
	finish(&l.Meta)
	return l
}
