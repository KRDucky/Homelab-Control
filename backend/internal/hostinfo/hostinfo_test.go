package hostinfo

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixtureCollector(t *testing.T) *Collector {
	t.Helper()
	root, err := filepath.Abs("testdata/host")
	if err != nil {
		t.Fatal(err)
	}
	return &Collector{
		Roots:     Roots{Proc: filepath.Join(root, "proc"), Sys: filepath.Join(root, "sys"), Etc: filepath.Join(root, "etc")},
		CPUSample: 0,
		Statfs: func(p string) (uint64, uint64, uint64, error) {
			if p == "/srv/raid" {
				return 0, 0, 0, errors.New("permission denied")
			}
			return 1000, 400, 600, nil
		},
		Addrs: func(name string) []string { return []string{"192.0.2.10/24"} },
	}
}

func TestSystemFromFixture(t *testing.T) {
	s := fixtureCollector(t).system()
	if s.Status != StatusOK {
		t.Fatalf("status %q errors %v", s.Status, s.Errors)
	}
	if s.Hostname != "fixture-host" || s.Kernel != "6.8.0-fixture" {
		t.Fatalf("got %+v", s)
	}
	if s.OS.PrettyName != "Ubuntu 24.04.1 LTS" || s.OS.VersionID != "24.04" || s.DistroFamily != "debian" {
		t.Fatalf("os-release parse: %+v family=%s", s.OS, s.DistroFamily)
	}
	if s.UptimeSeconds != 12345.67 || s.LoadAverage != [3]float64{0.12, 0.08, 0.05} {
		t.Fatalf("uptime/load: %v %v", s.UptimeSeconds, s.LoadAverage)
	}
}

func TestDistroFamily(t *testing.T) {
	cases := map[string]OSRelease{
		"debian":  {ID: "debian"},
		"fedora":  {ID: "fedora"},
		"suse":    {ID: "opensuse-tumbleweed", IDLike: "opensuse suse"},
		"arch":    {ID: "cachyos", IDLike: "arch"},
		"unknown": {ID: "gentoo"},
	}
	for want, in := range cases {
		if got := DistroFamily(in); got != want {
			t.Errorf("%+v: got %s want %s", in, got, want)
		}
	}
}

func TestParseOSReleaseQuoting(t *testing.T) {
	o := ParseOSRelease([]byte("PRETTY_NAME='Arch \"Linux\"'\nID=arch\nBROKEN\n"))
	if o.PrettyName != `Arch "Linux"` || o.ID != "arch" {
		t.Fatalf("%+v", o)
	}
}

func TestCPUInfo(t *testing.T) {
	c := fixtureCollector(t).cpu()
	if c.Model != "Fixture CPU @ 3.40GHz" || c.LogicalCPUs != 2 {
		t.Fatalf("%+v", c)
	}
	if c.UsagePercent != nil {
		t.Fatal("usage must be omitted when no sample interval is configured")
	}
}

func TestMemory(t *testing.T) {
	m := fixtureCollector(t).memory()
	if m.Status != StatusOK {
		t.Fatal(m.Errors)
	}
	if m.TotalBytes != 32768000*1024 || m.AvailableBytes != 20000000*1024 || m.UsedBytes != (32768000-20000000)*1024 {
		t.Fatalf("%+v", m)
	}
	if m.SwapTotalBytes != 8192000*1024 {
		t.Fatalf("swap %d", m.SwapTotalBytes)
	}
}

func TestMemoryWithoutMemAvailableIsLabelled(t *testing.T) {
	kv := ParseMeminfo([]byte("MemTotal: 100 kB\nMemFree: 10 kB\nBuffers: 5 kB\nCached: 5 kB\n"))
	if kv["MemTotal"] != 102400 {
		t.Fatal(kv)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "meminfo"), []byte("MemTotal: 100 kB\nMemFree: 10 kB\nBuffers: 5 kB\nCached: 5 kB\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := (&Collector{Roots: Roots{Proc: dir}}).memory()
	if m.Status != StatusPartial || len(m.Errors) == 0 || m.AvailableBytes != 20*1024 {
		t.Fatalf("approximation must be flagged: %+v", m)
	}
}

func TestFilesystems(t *testing.T) {
	fs := fixtureCollector(t).filesystems()
	got := map[string]Filesystem{}
	for _, f := range fs.Items {
		got[f.MountPoint] = f
	}
	for _, pseudo := range []string{"/proc", "/run", "/var/lib/docker/overlay2/x/merged"} {
		if _, ok := got[pseudo]; ok {
			t.Errorf("pseudo filesystem %s must be excluded", pseudo)
		}
	}
	if f := got["/mnt/media library"]; f.FSType != "zfs" || !f.Probed || f.KernelSource != "tank/media" {
		t.Errorf("octal-escaped zfs mount: %+v", f)
	}
	if f := got["/mnt/nas"]; f.Probed || !strings.Contains(f.SkipReason, "network") {
		t.Errorf("network fs must not be probed: %+v", f)
	}
	if f := got["/srv/raid"]; f.Probed || !f.ReadOnly || !strings.Contains(f.SkipReason, "statfs failed") {
		t.Errorf("statfs failure must be reported: %+v", f)
	}
	if f := got["/mnt/pool"]; !f.Probed || f.FSType != "fuse.mergerfs" {
		t.Errorf("mergerfs should be probed: %+v", f)
	}
	if len(fs.Items) != 5 {
		t.Errorf("want 5 filesystems, got %d: %+v", len(fs.Items), fs.Items)
	}
}

func TestUnescapeMount(t *testing.T) {
	cases := map[string]string{`/a\040b`: "/a b", `/tab\011x`: "/tab\tx", `/plain`: "/plain", `/trail\04`: `/trail\04`, `/back\134slash`: `/back\slash`}
	for in, want := range cases {
		if got := unescapeMount(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestNetwork(t *testing.T) {
	n := fixtureCollector(t).network()
	if n.Status != StatusOK || len(n.Interfaces) != 2 {
		t.Fatalf("%+v", n)
	}
	var eth Interface
	for _, i := range n.Interfaces {
		if i.Name == "eth0" {
			eth = i
		}
	}
	if !eth.Physical || eth.SpeedMbps == nil || *eth.SpeedMbps != 1000 || eth.RxBytes != 111 || eth.TxBytes != 222 || eth.MTU != 1500 {
		t.Fatalf("%+v", eth)
	}
}

func TestLegacyNotInstalled(t *testing.T) {
	l := fixtureCollector(t).legacy()
	if l.Installed || l.Status != StatusOK {
		t.Fatalf("%+v", l)
	}
}

func TestLegacyUnreadableConfigNeedsPrivilege(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read 0000 files; permission behaviour not testable as root")
	}
	etc := t.TempDir()
	if err := os.MkdirAll(filepath.Join(etc, "webmin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(etc, "webmin/miniserv.conf"), []byte("port=10000\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(etc, "webmin/version"), []byte("2.670\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := (&Collector{Roots: Roots{Etc: etc}}).legacy()
	if !l.Installed || l.ConfigReadable || l.PrivilegeNeeded == "" || l.Version != "2.670" {
		t.Fatalf("%+v", l)
	}
}

func TestMissingRootsReportUnavailable(t *testing.T) {
	c := &Collector{Roots: Roots{Proc: "/nonexistent", Sys: "/nonexistent", Etc: "/nonexistent"}}
	o := c.Collect()
	if o.Memory.Status != StatusUnavailable || o.Filesystems.Status != StatusUnavailable || o.Network.Status != StatusUnavailable {
		t.Fatalf("missing sources must be reported, not invented: %+v", o)
	}
	if o.System.Hostname != "" || o.Memory.TotalBytes != 0 {
		t.Fatal("no values may be fabricated")
	}
}

func TestLargeMountTableReadCompletely(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i := 0; i < 3000; i++ { // ~200 KiB, typical of busy container hosts
		b.WriteString("overlay /var/lib/docker/overlay2/" + strings.Repeat("x", 40) + "/merged overlay rw 0 0\n")
	}
	b.WriteString("/dev/vdb1 /srv/last ext4 rw 0 0\n")
	if err := os.WriteFile(filepath.Join(dir, "self/mounts"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Collector{Roots: Roots{Proc: dir}, Statfs: func(string) (uint64, uint64, uint64, error) { return 1, 1, 0, nil }}
	fs := c.filesystems()
	if len(fs.Items) != 1 || fs.Items[0].MountPoint != "/srv/last" {
		t.Fatalf("mount table truncated: %+v", fs.Items)
	}
}

func TestStatfsTimeoutDoesNotHang(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	c := &Collector{StatfsTimeout: 50 * time.Millisecond, Statfs: func(string) (uint64, uint64, uint64, error) {
		<-block
		return 0, 0, 0, nil
	}}
	start := time.Now()
	if _, _, _, err := c.statfsBounded("/stuck"); !errors.Is(err, errStatfsTimeout) {
		t.Fatalf("want timeout, got %v", err)
	}
	// A second call while the first is still blocked must return immediately.
	if _, _, _, err := c.statfsBounded("/stuck"); !errors.Is(err, errStatfsTimeout) {
		t.Fatalf("want immediate timeout, got %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("statfs timeout not enforced")
	}
}

func TestOverlayRootIsReported(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	mounts := "overlay / overlay rw 0 0\noverlay /var/lib/docker/x/merged overlay rw 0 0\n"
	if err := os.WriteFile(filepath.Join(dir, "self/mounts"), []byte(mounts), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Collector{Roots: Roots{Proc: dir}, Statfs: func(string) (uint64, uint64, uint64, error) { return 10, 4, 6, nil }}
	fs := c.filesystems()
	if len(fs.Items) != 1 || fs.Items[0].MountPoint != "/" || !fs.Items[0].Probed {
		t.Fatalf("overlay root must be reported, other overlays not: %+v", fs.Items)
	}
}

// hc-api's sandbox makes every mount read-only and adds bind mounts; the host
// table (/proc/1/mounts) must win when readable.
func TestFilesystemsPreferHostMountTable(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"self", "1"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sandbox := "/dev/vda1 / ext4 ro,relatime 0 0\n/dev/vda1 /var/log/homelab-control ext4 rw 0 0\n/dev/vda1 /var/tmp ext4 rw 0 0\n"
	host := "/dev/vda1 / ext4 rw,relatime 0 0\n/dev/vda13 /boot ext4 rw 0 0\n"
	if err := os.WriteFile(filepath.Join(dir, "self/mounts"), []byte(sandbox), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "1/mounts"), []byte(host), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Collector{Roots: Roots{Proc: dir}, Statfs: func(string) (uint64, uint64, uint64, error) { return 10, 4, 6, nil }}
	fs := c.filesystems()
	if fs.View != "host" || fs.Note != "" || fs.Sources[0] != filepath.Join(dir, "1/mounts") {
		t.Fatalf("want host view from 1/mounts: %+v", fs)
	}
	if len(fs.Items) != 2 || fs.Items[0].MountPoint != "/" || fs.Items[0].ReadOnly || fs.Items[1].MountPoint != "/boot" {
		t.Fatalf("host table must be used, sandbox bind mounts and ro flags ignored: %+v", fs.Items)
	}
}

func TestFilesystemsSandboxFallbackWithholdsReadOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "self/mounts"), []byte("/dev/vda1 / ext4 ro 0 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Collector{Roots: Roots{Proc: dir}, Statfs: func(string) (uint64, uint64, uint64, error) { return 10, 4, 6, nil }}
	fs := c.filesystems()
	if fs.View != "service" || !strings.Contains(fs.Note, "sandboxed view") {
		t.Fatalf("fallback must be labelled: %+v", fs)
	}
	if len(fs.Items) != 1 || fs.Items[0].ReadOnly {
		t.Fatalf("sandbox read-only flag must not be reported as host state: %+v", fs.Items)
	}
}
