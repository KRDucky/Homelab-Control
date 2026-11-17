// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"strings"
	"testing"

	"homelab-control/backend/internal/storageprobe"
)

func TestReadRequestBounds(t *testing.T) {
	// Exactly at the cap is accepted.
	exact := bytes.Repeat([]byte{'a'}, maxRequestBytes)
	if _, err := readRequest(bytes.NewReader(exact)); err != nil {
		t.Fatalf("exact-size request rejected: %v", err)
	}

	// One byte over is rejected as oversized (truncation detected).
	over := bytes.Repeat([]byte{'a'}, maxRequestBytes+1)
	if _, err := readRequest(bytes.NewReader(over)); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("oversized request not rejected: %v", err)
	}

	// A small request passes.
	if _, err := readRequest(strings.NewReader(`{"version":1}`)); err != nil {
		t.Fatalf("small request rejected: %v", err)
	}
}

func TestWriteResponseBounded(t *testing.T) {
	res := storageprobe.Result{Version: 1, Blkid: map[string]string{"sda": strings.Repeat("a", 100)}}
	var buf bytes.Buffer
	if err := storageprobe.WriteResponse(&buf, res, 1<<20); err != nil {
		t.Fatalf("small response: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("no output")
	}

	// A tiny frame cap must fail without writing partial output.
	var buf2 bytes.Buffer
	if err := storageprobe.WriteResponse(&buf2, res, 8); err == nil {
		t.Fatal("expected frame-limit error")
	}
	if buf2.Len() != 0 {
		t.Fatalf("partial output written on overflow: %q", buf2.String())
	}
}

func TestWriteResponsePreflightExpansion(t *testing.T) {
	// Each quote byte expands to two (\"), so the preflight must reject a value
	// whose escaped size exceeds the cap before any marshal writes bytes.
	res := storageprobe.Result{Version: 1, Errors: []string{strings.Repeat("\"", 100)}}
	var buf bytes.Buffer
	if err := storageprobe.WriteResponse(&buf, res, 300); err == nil {
		t.Fatal("expected preflight rejection for expanding string")
	}
	if buf.Len() != 0 {
		t.Fatalf("bytes written on rejection: %d", buf.Len())
	}
}

func TestWriteResponseControlBytes(t *testing.T) {
	// Control bytes encode to six-byte \u00XX sequences, so the preflight must
	// count them as 6 (not 2) and reject before marshal, emitting no bytes.
	res := storageprobe.Result{Version: 1, Errors: []string{strings.Repeat("\x01", 20)}}
	var buf bytes.Buffer
	if err := storageprobe.WriteResponse(&buf, res, 200); err == nil {
		t.Fatal("expected control-heavy preflight rejection")
	}
	if buf.Len() != 0 {
		t.Fatalf("bytes written on rejection: %d", buf.Len())
	}
}
