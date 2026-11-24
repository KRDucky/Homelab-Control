// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package partplan

import (
	"strings"
	"testing"
)

func TestGPTScriptMatchesGuestLayout(t *testing.T) {
	script, err := GPTScript(Request{
		DiskBytes: 1 << 30,
		Parts: []Slice{
			{StartBytes: 16777216, SizeBytes: 8388608, FSType: "swap"},
			{StartBytes: 1048576, SizeBytes: 8388608, FSType: "EXT4"},
		},
	}, 512)
	if err != nil {
		t.Fatal(err)
	}
	want := "label: gpt\nunit: sectors\n" +
		"start=2048, size=16384, type=" + GPTLinux + "\n" +
		"start=32768, size=16384, type=" + GPTSwap + "\n"
	if script != want {
		t.Fatalf("script:\n%s\nwant:\n%s", script, want)
	}
	if strings.Contains(script, "mkfs") || strings.Contains(script, "/") {
		t.Fatal(script)
	}
}

func TestGPTScriptRefusesUnknownStartAndTail(t *testing.T) {
	base := Request{DiskBytes: 8 << 20, Parts: []Slice{{StartBytes: 1 << 20, SizeBytes: 1 << 20, FSType: "none"}}}
	if _, err := GPTScript(base, 512); err != nil {
		t.Fatal(err)
	}
	unknown := base
	unknown.Parts = []Slice{{StartBytes: 1 << 20, SizeBytes: 1 << 20, FSType: "unknown"}}
	if _, err := GPTScript(unknown, 512); err == nil || !strings.Contains(err.Error(), "not observed") {
		t.Fatal(err)
	}
	early := base
	early.Parts = []Slice{{StartBytes: 0, SizeBytes: 1 << 20, FSType: "xfs"}}
	if _, err := GPTScript(early, 512); err == nil || !strings.Contains(err.Error(), "1 MiB") {
		t.Fatal(err)
	}
	tail := base
	tail.Parts = []Slice{{StartBytes: 6 << 20, SizeBytes: 2 << 20, FSType: "btrfs"}}
	if _, err := GPTScript(tail, 512); err == nil || !strings.Contains(err.Error(), "backup") {
		t.Fatal(err)
	}
	if _, err := GPTScript(base, 520); err == nil {
		t.Fatal("odd sector size accepted")
	}
}
