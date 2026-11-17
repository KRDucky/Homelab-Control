// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storageprobe

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeScript writes an executable fake tool. It is test-only and never runs
// on the host; the tools are synthetic fixtures.
func writeScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// fakeEnv points the package at a fixture sysfs tree and fake lvs/pvs/blkid.
func fakeEnv(t *testing.T) (sys, bin string) {
	t.Helper()
	dir := t.TempDir()
	sys = filepath.Join(dir, "sys")
	bin = filepath.Join(dir, "bin")
	if err := os.MkdirAll(filepath.Join(sys, "sda"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sys, "nvme0n1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeScript(t, filepath.Join(bin, "lvs"), "#!/bin/sh\necho '{\"report\":[{\"lv\":[{\"lv_uuid\":\"u1\",\"lv_name\":\"lv\"}]}]}'\n")
	writeScript(t, filepath.Join(bin, "pvs"), "#!/bin/sh\necho '{\"report\":[{\"pv\":[{\"pv_uuid\":\"p1\",\"pv_name\":\"/dev/sda\"}]}]}'\n")
	writeScript(t, filepath.Join(bin, "blkid"), "#!/bin/sh\nfor a in \"$@\"; do last=$a; done; echo \"DEV=$last\"\necho 'ID_FS_TYPE=LVM2_member'\n")

	oldSys, oldBin := sysBlockRoot, binaries
	sysBlockRoot = sys
	binaries = map[string]string{
		"lvs":   filepath.Join(bin, "lvs"),
		"pvs":   filepath.Join(bin, "pvs"),
		"blkid": filepath.Join(bin, "blkid"),
	}
	t.Cleanup(func() { sysBlockRoot = oldSys; binaries = oldBin })
	return sys, bin
}

func TestRunCollectsAndRevalidates(t *testing.T) {
	fakeEnv(t)
	req := Request{Version: 1, Devices: []string{"sda", "nvme0n1", "../evil", "nonexistent", "a/b"}}
	res := Run(context.Background(), req)
	if res.LVS == "" || res.PVS == "" {
		t.Fatalf("missing LVM reports: %+v", res)
	}
	if len(res.Blkid) != 2 {
		t.Fatalf("blkid devices = %d, want 2: %+v", len(res.Blkid), res.Blkid)
	}
	if !strings.Contains(res.Blkid["sda"], "DEV=/dev/sda") {
		t.Fatalf("sda blkid argv wrong: %q", res.Blkid["sda"])
	}
	if !strings.Contains(res.Blkid["nvme0n1"], "DEV=/dev/nvme0n1") {
		t.Fatalf("nvme0n1 blkid argv wrong: %q", res.Blkid["nvme0n1"])
	}
	// Path traversal and missing devices must be dropped by revalidation.
	for _, bad := range []string{"../evil", "nonexistent", "a/b"} {
		if _, ok := res.Blkid[bad]; ok {
			t.Fatalf("invalid device %q was probed", bad)
		}
	}
	if res.Partial {
		t.Fatalf("unexpected partial: %+v", res)
	}
}

func TestRunOversizedOutputPartial(t *testing.T) {
	_, bin := fakeEnv(t)
	// A blkid that writes far more than the cap must be reported as an error
	// and mark the result partial, not crash or pass through unbounded output.
	writeScript(t, filepath.Join(bin, "blkid"), "#!/bin/sh\nhead -c 70000 /dev/zero | tr '\\0' 'a'\n")
	res := Run(context.Background(), Request{Version: 1, Devices: []string{"sda"}})
	if !res.Partial {
		t.Fatalf("oversized blkid not partial: %+v", res)
	}
	if len(res.Errors) == 0 || !strings.Contains(res.Errors[0], "exceeded") {
		t.Fatalf("oversized output not reported: %+v", res)
	}
}

func TestProtocolRoundTrip(t *testing.T) {
	req := Request{Version: 1, Devices: []string{"sda"}}
	b, err := MarshalResponse(Result{Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"version":1`) {
		t.Fatalf("marshal: %s", b)
	}
	got, err := UnmarshalRequest([]byte(`{"version":1,"devices":["sda"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || len(got.Devices) != 1 {
		t.Fatalf("%+v", got)
	}
	if _, err := UnmarshalRequest([]byte(`{"version":2,"devices":[]}`)); err == nil {
		t.Fatal("unsupported version accepted")
	}
	_ = req
}
