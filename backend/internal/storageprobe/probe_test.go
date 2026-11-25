// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storageprobe

import (
	"context"
	"encoding/json"
	"fmt"
	"homelab-control/backend/internal/mountinfo"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"homelab-control/backend/internal/storagetools"
)

const allGroupsProcDevices = "Character devices:\n  1 mem\nBlock devices:\n  7 loop\n  8 sd\n  9 md\n  65 sd\n  252 virtblk\n  253 device-mapper\n  259 blkext\n  265 virtblk\n"

const fakeLsblk = `{"blockdevices":[
 {"name":"sda","kname":"sda","pkname":null,"path":"/dev/sda","type":"disk","maj:min":"8:0","size":1000204886016,"ro":false,"rm":false,"rota":true,"tran":"sata","model":"WDC WD10EZEX","serial":"WD-1","wwn":"0x50014ee2b1","fstype":null,"uuid":null,"label":null,"partuuid":null,"partlabel":null},
 {"name":"sda1","kname":"sda1","pkname":"sda","path":"/dev/sda1","type":"part","maj:min":"8:1","size":1000203837440,"ro":false,"rm":false,"rota":true,"tran":null,"model":null,"serial":null,"wwn":"0x50014ee2b1","fstype":"LVM2_member","uuid":"pv-uuid","label":null,"partuuid":"p1","partlabel":null},
 {"name":"nvme0n1","kname":"nvme0n1","pkname":null,"path":"/dev/nvme0n1","type":"disk","maj:min":"259:0","size":512110190592,"ro":false,"rm":false,"rota":false,"tran":"nvme","model":"Samsung 980","serial":"S1","wwn":null,"fstype":"ext4","uuid":"fs-uuid","label":"root","partuuid":null,"partlabel":null},
 {"name":"vda","kname":"vda","pkname":null,"path":"/dev/vda","type":"disk","maj:min":"252:0","size":32212254720,"ro":false,"rm":false,"rota":true,"tran":null,"model":null,"serial":null,"wwn":null,"fstype":null,"uuid":null,"label":null,"partuuid":null,"partlabel":null}
]}`

const fakeSmartATA = `{"smartctl":{"exit_status":4,"messages":[{"string":"Warning: ATA error count","severity":"warning"}]},"device":{"protocol":"ATA"},"model_name":"WDC WD10EZEX","serial_number":"WD-1","smart_support":{"available":true,"enabled":true},"smart_status":{"passed":true},"temperature":{"current":34},"power_on_time":{"hours":1234},"ata_smart_attributes":{"table":[{"id":5,"raw":{"value":0}},{"id":197,"raw":{"value":2}}]}}`

const fakeSmartNVMe = `{"smartctl":{"exit_status":0},"device":{"protocol":"NVMe"},"model_name":"Samsung 980","serial_number":"S1","smart_status":{"passed":true},"nvme_smart_health_information_log":{"critical_warning":0,"percentage_used":3,"media_errors":0,"available_spare":100}}`

// fixture installs fake tools and a full /proc/devices, and returns the file
// each fake appends its argv to.
func fixture(t *testing.T) (argLog string) {
	t.Helper()
	base := t.TempDir()
	bin := filepath.Join(base, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	argLog = filepath.Join(base, "args.log")
	logArgs := "echo \"$(basename \"$0\") $*\" >> '" + argLog + "'\n"
	writeScript(t, filepath.Join(bin, "lsblk"), "#!/bin/sh\n"+logArgs+"cat <<'EOF'\n"+fakeLsblk+"\nEOF\n")
	writeScript(t, filepath.Join(bin, "smartctl"), "#!/bin/sh\n"+logArgs+
		"for a in \"$@\"; do last=$a; done\n"+
		"case $last in /dev/sda) cat <<'EOF'\n"+fakeSmartATA+"\nEOF\nexit 4;;\n"+
		"/dev/nvme0n1) cat <<'EOF'\n"+fakeSmartNVMe+"\nEOF\n;;\n*) exit 1;; esac\n")
	report := `{"report":[{"pv":[]}]}`
	writeScript(t, filepath.Join(bin, "pvs"), "#!/bin/sh\n"+logArgs+"echo '"+report+"'\n")
	writeScript(t, filepath.Join(bin, "lvs"), "#!/bin/sh\n"+logArgs+"echo '"+strings.Replace(report, "pv", "lv", 1)+"'\n")

	oldBin, oldRes, oldMin, oldLast, oldProc, oldMI, oldMd := binaries, resolved, minRunInterval, lastRun, procDevicesPath, mountinfoPath, mdstatPath
	binaries = map[string]string{}
	resolved = map[string]bool{}
	writeScript(t, filepath.Join(bin, "zpool"), "#!/bin/sh\n"+logArgs+
		"case $1 in list) printf 'tank\\t100\\t10\\t90\\t1\\t10\\tONLINE\\n';; status) printf '  pool: tank\\n state: ONLINE\\nconfig:\\n\\n\\tNAME STATE READ WRITE CKSUM\\n\\ttank ONLINE 0 0 0\\n\\t  vdb ONLINE 0 0 0\\n\\nerrors: No known data errors\\n';; esac\n")
	writeScript(t, filepath.Join(bin, "zfs"), "#!/bin/sh\n"+logArgs+"printf 'tank\\t10\\t90\\t10\\t/tank\\ton\\tfilesystem\\n'\n")
	writeScript(t, filepath.Join(bin, "btrfs"), "#!/bin/sh\n"+logArgs+
		"case \"$1 $2\" in\n"+
		"'filesystem usage') printf 'Overall:\\n    Device size:\\t\\t 1000\\n    Used:\\t\\t 10\\n\\nData,single: Size:100, Used:10 (10.00%%)\\n   /dev/vdd\\t 100\\n';;\n"+
		"'device stats') printf '[/dev/vdd].write_io_errs    0\\n[/dev/vdd].corruption_errs  2\\n';;\n"+
		"'scrub status') printf 'UUID:             u-1\\nStatus:           finished\\nError summary:    no errors found\\n';;\n"+
		"'subvolume list') printf 'ID 256 gen 9 top level 5 path @data\\n'; if [ \"$3\" = /srv/b ]; then exit 1; fi;;\n"+
		"esac\n")
	writeScript(t, filepath.Join(bin, "mdadm"), "#!/bin/sh\n"+logArgs+
		"case $2 in /dev/md127) printf '/dev/md127:\\n        Raid Level : raid1\\n             State : clean, degraded\\n    Failed Devices : 1\\n\\n    Number   Major   Minor   RaidDevice State\\n       0       8       17        0      active sync   /dev/sdb1\\n       1       8       33        1      faulty   /dev/sdc1\\n';;\n"+
		"*) echo 'mdadm: cannot open' >&2; exit 1;; esac\n")
	mdstatPath = filepath.Join(base, "mdstat")
	if err := os.WriteFile(mdstatPath, []byte(fakeMdstat), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"lsblk", "smartctl", "pvs", "lvs", "zpool", "zfs", "btrfs", "mdadm"} {
		binaries[tool] = filepath.Join(bin, tool)
		resolved[tool] = true
	}
	minRunInterval, lastRun = 0, time.Time{}
	procDevicesPath = fakeProcDevices(t, base, allGroupsProcDevices)
	mountinfoPath = filepath.Join(base, "mountinfo")
	if err := os.WriteFile(mountinfoPath, []byte(fakeMountinfo), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		mountinfoPath = oldMI
		mdstatPath = oldMd
		binaries, resolved, minRunInterval, lastRun, procDevicesPath = oldBin, oldRes, oldMin, oldLast, oldProc
	})
	return argLog
}

func TestRunCollectsWithFixedTemplates(t *testing.T) {
	argLog := fixture(t)
	res := Run(context.Background(), Request{Version: storagetools.ProtocolVersion})
	if res.Version != storagetools.ProtocolVersion || res.Partial || res.Code != "" {
		t.Fatalf("result: %+v", res)
	}
	if len(res.Blocks) != 4 {
		t.Fatalf("blocks: %+v", res.Blocks)
	}
	// SMART only on the SATA and NVMe whole disks, not the partition or virtio.
	if len(res.SMART) != 2 || res.SMART["sda"].Protocol != "ATA" || res.SMART["nvme0n1"].Protocol != "NVMe" {
		t.Fatalf("smart: %+v", res.SMART)
	}
	if s := res.SMART["sda"]; s.ExitStatus != 4 || s.Pending == nil || *s.Pending != 2 || s.Health() != "warning" {
		t.Fatalf("ata reading: %+v health=%s", s, s.Health())
	}
	if len(res.PVS) == 0 || len(res.LVS) == 0 {
		t.Fatal("LVM reports missing")
	}
	b, _ := os.ReadFile(argLog)
	got := string(b)
	for _, want := range []string{
		"lsblk " + strings.Join(storagetools.LsblkArgs(), " "),
		"smartctl " + strings.Join(storagetools.SmartctlArgs("sda"), " "),
		"smartctl " + strings.Join(storagetools.SmartctlArgs("nvme0n1"), " "),
		"pvs " + strings.Join(storagetools.PVSArgs(), " "),
		"lvs " + strings.Join(storagetools.LVSArgs(), " "),
	} {
		if !strings.Contains(got, want+"\n") {
			t.Errorf("missing invocation %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "/dev/vda") || strings.Contains(got, "/dev/sda1") {
		t.Fatalf("smartctl ran on a non-SMART device:\n%s", got)
	}
	if !strings.Contains(got, "--nocheck=standby") || !strings.Contains(got, "pvs --readonly") {
		t.Fatalf("standby/readonly flags missing:\n%s", got)
	}
}

func TestRunOptionalToolsMissing(t *testing.T) {
	fixture(t)
	resolved["smartctl"], resolved["pvs"], resolved["lvs"] = false, false, false
	res := Run(context.Background(), Request{Version: storagetools.ProtocolVersion})
	if !res.Partial || len(res.SMART) != 0 || res.PVS != nil || res.Tools["smartctl"] {
		t.Fatalf("result: %+v", res)
	}
	if !strings.Contains(strings.Join(res.Errors, "|"), "smartmontools") {
		t.Fatalf("missing smartctl not explained: %v", res.Errors)
	}
}

func TestRunRefusesWithoutDeviceClasses(t *testing.T) {
	argLog := fixture(t)
	procDevicesPath = fakeProcDevices(t, t.TempDir(), "Block devices:\n  7 loop\n  253 device-mapper\n")
	res := Run(context.Background(), Request{Version: storagetools.ProtocolVersion})
	if res.Code != CodeDevicePolicyUnresolved || !res.Partial {
		t.Fatalf("result: %+v", res)
	}
	if b, _ := os.ReadFile(argLog); len(b) != 0 {
		t.Fatalf("a tool ran despite the refusal: %s", b)
	}
	var buf strings.Builder
	if err := WriteResponse(&buf, res, 1<<20); err != nil || !strings.Contains(buf.String(), `"code":"device_policy_unresolved"`) {
		t.Fatalf("wire result: %v %s", err, buf.String())
	}
}

func TestRunNotesMissingDeviceClass(t *testing.T) {
	fixture(t)
	procDevicesPath = fakeProcDevices(t, t.TempDir(), "Block devices:\n  8 sd\n  252 virtblk\n  259 blkext\n")
	res := Run(context.Background(), Request{Version: storagetools.ProtocolVersion})
	if res.Code != "" || len(res.Blocks) == 0 || !strings.Contains(strings.Join(res.Errors, "|"), "md") {
		t.Fatalf("a host without md must still be probed, with a note: %+v", res)
	}
}

func TestRunBadToolOutputIsPartial(t *testing.T) {
	fixture(t)
	writeScript(t, binaries["lsblk"], "#!/bin/sh\necho not-json\n")
	res := Run(context.Background(), Request{Version: storagetools.ProtocolVersion})
	if !res.Partial || len(res.Blocks) != 0 || len(res.SMART) != 0 {
		t.Fatalf("result: %+v", res)
	}
	fixture(t)
	writeScript(t, binaries["lvs"], "#!/bin/sh\necho '{\"report\":'\n")
	res = Run(context.Background(), Request{Version: storagetools.ProtocolVersion})
	if !res.Partial || res.LVS != nil || res.PVS == nil {
		t.Fatalf("truncated lvs output forwarded: %+v", res)
	}
}

func TestRunTimesOut(t *testing.T) {
	fixture(t)
	writeScript(t, binaries["lsblk"], "#!/bin/sh\nsleep 30\n")
	old := budget
	budget = func(string) time.Duration { return 300 * time.Millisecond }
	t.Cleanup(func() { budget = old })
	start := time.Now()
	res := Run(context.Background(), Request{Version: storagetools.ProtocolVersion})
	if !res.Partial || time.Since(start) > 5*time.Second {
		t.Fatalf("timeout not enforced: %v %+v", time.Since(start), res)
	}
}

func TestRunOutputCap(t *testing.T) {
	fixture(t)
	writeScript(t, binaries["lsblk"], "#!/bin/sh\nhead -c 400000 /dev/zero | tr '\\0' 'x'\n")
	res := Run(context.Background(), Request{Version: storagetools.ProtocolVersion})
	if !res.Partial || !strings.Contains(strings.Join(res.Errors, "|"), "exceeded") {
		t.Fatalf("oversized output accepted: %+v", res.Errors)
	}
}

func TestUnmarshalRequestStrict(t *testing.T) {
	if _, err := UnmarshalRequest([]byte(`{"version":2}`)); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	for _, bad := range []string{
		`{"version":2,"devices":["sda"]}`, // callers name no devices
		`{"version":2,"version":2}`,
		`{"version":2} {"version":2}`,
		`{"version":1}`,
		`{"version":2,"scope":"all-the-things"}`, // unknown scope
		`{"version":2,"scope":"md/../zfs"}`,      // unknown scope
		`[1,2]`,
	} {
		if _, err := UnmarshalRequest([]byte(bad)); err == nil {
			t.Fatalf("expected rejection of %q", bad)
		}
	}
}

func TestWriteResponseCap(t *testing.T) {
	res := Result{Version: storagetools.ProtocolVersion, Errors: []string{strings.Repeat("x", 2000)}}
	var buf strings.Builder
	if err := WriteResponse(&buf, res, 1000); err == nil || buf.Len() != 0 {
		t.Fatalf("oversized frame written: %v %d", err, buf.Len())
	}
	if err := WriteResponse(&buf, res, 1<<20); err != nil {
		t.Fatal(err)
	}
	var back Result
	if err := json.Unmarshal([]byte(buf.String()), &back); err != nil || strings.HasSuffix(buf.String(), "\n") {
		t.Fatalf("frame not one JSON object: %v", err)
	}
}

func writeScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func fakeProcDevices(t *testing.T, base, content string) string {
	t.Helper()
	p := filepath.Join(base, "proc_devices")
	writeFile(t, p, content)
	return p
}

func TestCheckDeviceGroups(t *testing.T) {
	base := t.TempDir()
	old := procDevicesPath
	t.Cleanup(func() { procDevicesPath = old })

	procDevicesPath = fakeProcDevices(t, base, allGroupsProcDevices)
	if missing, err := checkDeviceGroups(); err != nil || len(missing) != 0 {
		t.Fatalf("all groups should pass: %v %v", missing, err)
	}
	// A host without software RAID has no md group: not a refusal (Debian 13 VM finding).
	procDevicesPath = fakeProcDevices(t, base, "Block devices:\n  8 sd\n  252 virtblk\n  259 blkext\n")
	if missing, err := checkDeviceGroups(); err != nil || len(missing) != 1 || missing[0] != "md" {
		t.Fatalf("missing md: %v %v", missing, err)
	}
	procDevicesPath = fakeProcDevices(t, base, "Character devices:\n  1 mem\n")
	if _, err := checkDeviceGroups(); err == nil {
		t.Fatal("no block groups should fail")
	}
}

func TestAdmitRateLimit(t *testing.T) {
	oldMin, oldLast := minRunInterval, lastRun
	defer func() { minRunInterval, lastRun = oldMin, oldLast }()
	minRunInterval = time.Hour
	lastRun = time.Time{}
	if !admit() {
		t.Fatal("first admit should pass")
	}
	if admit() {
		t.Fatal("second admit within window should fail")
	}
	release()
}

func TestSecureToolPath(t *testing.T) {
	dir := t.TempDir()
	w := filepath.Join(dir, "w")
	if err := os.WriteFile(w, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(w, 0o777); err != nil { // force writable despite umask
		t.Fatal(err)
	}
	sw := filepath.Join(dir, "sw")
	if err := os.Symlink(w, sw); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "nope")

	if _, err := secureToolPath([]string{w}); err == nil {
		t.Fatal("group/other-writable binary accepted")
	}
	// A symlink is resolved and its target is checked: a symlink to a writable
	// file is rejected on the target, not the symlink itself.
	if _, err := secureToolPath([]string{sw}); err == nil {
		t.Fatal("symlink-to-writable binary accepted")
	}
	if _, err := secureToolPath([]string{missing}); err == nil {
		t.Fatal("missing binary accepted")
	}
	// The accept path needs a root-owned, non-writable directory chain (e.g.
	// /usr/sbin) and is exercised only in production; no host probe here.
}

func TestCheckSecureFile(t *testing.T) {
	dir := t.TempDir()
	writable := filepath.Join(dir, "w")
	if err := os.WriteFile(writable, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(writable, 0o777); err != nil { // force writable despite umask
		t.Fatal(err)
	}
	if err := checkSecureFile(writable); err == nil || !strings.Contains(err.Error(), "writable") {
		t.Fatalf("writable file: %v", err)
	}

	regular := filepath.Join(dir, "r")
	if err := os.WriteFile(regular, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if os.Getuid() == 0 {
		if err := checkSecureFile(regular); err != nil {
			t.Fatalf("root-owned secure file rejected: %v", err)
		}
	} else {
		if err := checkSecureFile(regular); err == nil || !strings.Contains(err.Error(), "root-owned") {
			t.Fatalf("non-root-owned file: %v", err)
		}
	}

	adir := filepath.Join(dir, "d")
	if err := os.Mkdir(adir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := checkSecureFile(adir); err == nil || !strings.Contains(err.Error(), "regular") {
		t.Fatalf("directory: %v", err)
	}
}
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunZFSScopeRunsOnlyZFSTemplates(t *testing.T) {
	argLog := fixture(t)
	res := Run(context.Background(), Request{Version: storagetools.ProtocolVersion, Scope: storagetools.ScopeZFS})
	if res.Partial || res.ZFS == nil || len(res.ZFS.Pools) != 1 || res.ZFS.Pools[0].Vdevs[1].Name != "vdb" || len(res.ZFS.Datasets) != 1 {
		t.Fatalf("%+v %+v", res, res.ZFS)
	}
	if len(res.Blocks) != 0 || len(res.SMART) != 0 || res.PVS != nil {
		t.Fatalf("zfs scope ran the storage probe: %+v", res)
	}
	b, _ := os.ReadFile(argLog)
	want := "zpool " + strings.Join(storagetools.ZpoolListArgs(), " ") + "\n" +
		"zpool " + strings.Join(storagetools.ZpoolStatusArgs(), " ") + "\n" +
		"zfs " + strings.Join(storagetools.ZfsListArgs(), " ") + "\n"
	if string(b) != want {
		t.Fatalf("invocations:\n%s\nwant:\n%s", b, want)
	}
}

func TestRunZFSScopeWithoutTools(t *testing.T) {
	fixture(t)
	resolved["zpool"] = false
	res := Run(context.Background(), Request{Version: storagetools.ProtocolVersion, Scope: storagetools.ScopeZFS})
	if !res.Partial || res.ZFS != nil || !strings.Contains(strings.Join(res.Errors, "|"), "zfsutils-linux") {
		t.Fatalf("%+v", res)
	}
}

// The device-class self-check guards block-device reads (lsblk, smartctl,
// LVM). The ZFS scope only talks to /dev/zfs, so a host without the reviewed
// block classes still gets its pool report, and no block-class note.
func TestRunZFSScopeDoesNotNeedBlockDeviceClasses(t *testing.T) {
	fixture(t)
	procDevicesPath = fakeProcDevices(t, t.TempDir(), "Block devices:\n  7 loop\n  253 device-mapper\n")
	res := Run(context.Background(), Request{Version: storagetools.ProtocolVersion, Scope: storagetools.ScopeZFS})
	if res.Code != "" || res.Partial || res.ZFS == nil || len(res.ZFS.Pools) != 1 || len(res.Errors) != 0 {
		t.Fatalf("code=%q partial=%v errors=%v zfs=%+v", res.Code, res.Partial, res.Errors, res.ZFS)
	}
}

// Two subvolume mounts of one filesystem, one filesystem mounted only under
// /home (hidden by ProtectHome), one more filesystem, and unrelated mounts.
const fakeMountinfo = `24 1 252:1 / / rw - ext4 /dev/vda1 rw
40 24 0:50 /@data /srv/a rw - btrfs /dev/vdd rw,subvol=/@data
41 24 0:50 /@snap /srv/a-snap rw - btrfs /dev/vdd rw,subvol=/@snap
42 24 0:51 /@home /home rw - btrfs /dev/vde rw,subvol=/@home
43 24 0:52 / /srv/b rw - btrfs /dev/vdf rw
44 24 0:53 / /srv/../x rw - btrfs /dev/vdg rw
`

func TestRunBtrfsScopeNamesItsOwnMountpoints(t *testing.T) {
	argLog := fixture(t)
	res := Run(context.Background(), Request{Version: storagetools.ProtocolVersion, Scope: storagetools.ScopeBtrfs})
	rep := res.Btrfs
	if rep == nil || len(rep.Filesystems) != 2 || len(rep.Skipped) != 1 || rep.Truncated {
		t.Fatalf("%+v %+v", res, rep)
	}
	a, b := rep.Filesystems[0], rep.Filesystems[1]
	if a.Mountpoint != "/srv/a" || strings.Join(a.Mounts, ",") != "/srv/a,/srv/a-snap" || a.UUID != "u-1" || *a.Overall.DeviceSize != 1000 ||
		a.Profiles[0].Profile != "single" || *a.Devices[0].CorruptionErrs != 2 || a.Scrub.Status != "finished" || len(a.Subvolumes) != 1 || len(a.Errors) != 0 {
		t.Fatalf("a: %+v", a)
	}
	// A failing command is recorded on its filesystem; the rest is reported.
	if b.Mountpoint != "/srv/b" || len(b.Errors) != 1 || !strings.HasPrefix(b.Errors[0], "subvolume list:") || b.Subvolumes == nil || b.UUID != "u-1" || !res.Partial {
		t.Fatalf("b: %+v partial=%v", b, res.Partial)
	}
	if rep.Skipped[0].Mountpoint != "/home" || !strings.Contains(rep.Skipped[0].Reason, "cannot open") {
		t.Fatalf("skipped: %+v", rep.Skipped)
	}
	if len(res.Blocks) != 0 || res.ZFS != nil {
		t.Fatalf("btrfs scope ran other probes: %+v", res)
	}
	got, _ := os.ReadFile(argLog)
	var want strings.Builder
	for _, m := range []string{"/srv/a", "/srv/b"} {
		for _, args := range [][]string{storagetools.BtrfsUsageArgs(m), storagetools.BtrfsDeviceStatsArgs(m), storagetools.BtrfsScrubStatusArgs(m), storagetools.BtrfsSubvolumeListArgs(m)} {
			want.WriteString("btrfs " + strings.Join(args, " ") + "\n")
		}
	}
	if string(got) != want.String() {
		t.Fatalf("invocations:\n%s\nwant:\n%s", got, want.String())
	}
}

func TestBtrfsTargetLimit(t *testing.T) {
	var recs []mountinfo.Record
	for i := 0; i < storagetools.MaxBtrfsFilesystems+2; i++ {
		recs = append(recs, mountinfo.Record{FSType: "btrfs", DeviceNumber: fmt.Sprintf("0:%d", 60+i), Mountpoint: fmt.Sprintf("/m%d", i)})
	}
	targets, skipped, truncated := btrfsTargets(recs)
	if len(targets) != storagetools.MaxBtrfsFilesystems || len(skipped) != 2 || !truncated {
		t.Fatalf("targets=%d skipped=%d truncated=%v", len(targets), len(skipped), truncated)
	}
}

func TestRunBtrfsScopeWithoutTool(t *testing.T) {
	fixture(t)
	resolved["btrfs"] = false
	res := Run(context.Background(), Request{Version: storagetools.ProtocolVersion, Scope: storagetools.ScopeBtrfs})
	if !res.Partial || res.Btrfs != nil || !strings.Contains(strings.Join(res.Errors, "|"), "btrfs-progs") {
		t.Fatalf("%+v", res)
	}
}

// As for ZFS: btrfs-progs works through mountpoint ioctls and opens no block
// device, so the block-class self-check must not stop the Btrfs scope.
func TestRunBtrfsScopeDoesNotNeedBlockDeviceClasses(t *testing.T) {
	fixture(t)
	procDevicesPath = fakeProcDevices(t, t.TempDir(), "Block devices:\n  7 loop\n  253 device-mapper\n")
	res := Run(context.Background(), Request{Version: storagetools.ProtocolVersion, Scope: storagetools.ScopeBtrfs})
	if res.Code != "" || res.Btrfs == nil || len(res.Btrfs.Filesystems) != 2 {
		t.Fatalf("code=%q errors=%v btrfs=%+v", res.Code, res.Errors, res.Btrfs)
	}
	for _, e := range res.Errors {
		if strings.Contains(e, "block device") {
			t.Fatalf("block-class note on a Btrfs report: %v", res.Errors)
		}
	}
}

const fakeMdstat = `Personalities : [raid1]
md127 : active raid1 sdc1[1](F) sdb1[0]
      1046528 blocks super 1.2 [2/1] [U_]

md126 : active raid1 sdd1[1] sde1[0]
      1046528 blocks super 1.2 [2/2] [UU]

unused devices: <none>
`

func TestRunMdScopeDescribesEachArray(t *testing.T) {
	argLog := fixture(t)
	res := Run(context.Background(), Request{Version: storagetools.ProtocolVersion, Scope: storagetools.ScopeMd})
	rep := res.Md
	if rep == nil || len(rep.Arrays) != 2 || len(res.Blocks) != 0 {
		t.Fatalf("%+v %+v", res, rep)
	}
	a, b := rep.Arrays[0], rep.Arrays[1]
	if a.Device != "/dev/md127" || a.State != "clean, degraded" || *a.Failed != 1 || a.Members[1].State != "faulty" || a.Error != "" {
		t.Fatalf("md127: %+v", a)
	}
	// mdadm failed for md126: it keeps its name and mdstat state, with the error.
	if b.Device != "/dev/md126" || b.State != "active" || b.Level != "raid1" || !strings.HasPrefix(b.Error, "mdadm --detail:") || !res.Partial {
		t.Fatalf("md126: %+v partial=%v", b, res.Partial)
	}
	got, _ := os.ReadFile(argLog)
	want := "mdadm " + strings.Join(storagetools.MdDetailArgs("md127"), " ") + "\n" + "mdadm " + strings.Join(storagetools.MdDetailArgs("md126"), " ") + "\n"
	if string(got) != want {
		t.Fatalf("invocations:\n%s\nwant:\n%s", got, want)
	}
}

func TestRunMdScopeWithoutTool(t *testing.T) {
	fixture(t)
	resolved["mdadm"] = false
	res := Run(context.Background(), Request{Version: storagetools.ProtocolVersion, Scope: storagetools.ScopeMd})
	if !res.Partial || res.Md != nil || !strings.Contains(strings.Join(res.Errors, "|"), "package mdadm") {
		t.Fatalf("%+v", res)
	}
}

// mdadm --detail opens /dev/mdN, a block device, so the md scope stays
// behind the block-class self-check (unlike the ZFS and Btrfs scopes).
func TestRunMdScopeNeedsBlockDeviceClasses(t *testing.T) {
	fixture(t)
	procDevicesPath = fakeProcDevices(t, t.TempDir(), "Block devices:\n  7 loop\n  253 device-mapper\n")
	res := Run(context.Background(), Request{Version: storagetools.ProtocolVersion, Scope: storagetools.ScopeMd})
	if res.Code != CodeDevicePolicyUnresolved || res.Md != nil {
		t.Fatalf("%+v", res)
	}
}
