// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"strings"
	"testing"
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
