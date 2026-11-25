// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storageprobe

import (
	"context"
	"encoding/json"
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

	oldBin, oldRes, oldMin, oldLast, oldProc := binaries, resolved, minRunInterval, lastRun, procDevicesPath
	binaries = map[string]string{}
	resolved = map[string]bool{}
	for _, tool := range []string{"lsblk", "smartctl", "pvs", "lvs"} {
		binaries[tool] = filepath.Join(bin, tool)
		resolved[tool] = true
	}
	minRunInterval, lastRun = 0, time.Time{}
	procDevicesPath = fakeProcDevices(t, base, allGroupsProcDevices)
	t.Cleanup(func() {
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
	procDevicesPath = fakeProcDevices(t, t.TempDir(), "Block devices:\n  8 sd\n  252 virtblk\n  259 blkext\n")
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
	old := totalTimeout
	totalTimeout = 300 * time.Millisecond
	t.Cleanup(func() { totalTimeout = old })
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
	if err := checkDeviceGroups(); err != nil {
		t.Fatalf("all groups should pass: %v", err)
	}

	procDevicesPath = fakeProcDevices(t, base, "Block devices:\n  8 sd\n  252 virtblk\n  259 blkext\n")
	if err := checkDeviceGroups(); err == nil || !strings.Contains(err.Error(), "md") {
		t.Fatalf("missing md should fail: %v", err)
	}

	procDevicesPath = fakeProcDevices(t, base, "Character devices:\n  1 mem\n")
	if err := checkDeviceGroups(); err == nil {
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
