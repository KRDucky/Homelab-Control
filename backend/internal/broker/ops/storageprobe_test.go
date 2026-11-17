// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"testing"
	"time"
)

func TestProbeRateLimit(t *testing.T) {
	oldMin, oldLast := minProbeInterval, probeLastRun
	defer func() { minProbeInterval, probeLastRun = oldMin, oldLast }()
	minProbeInterval = time.Hour
	probeLastRun = time.Time{}
	if err := probeRateLimit(time.Now()); err != nil {
		t.Fatalf("first probe should pass: %v", err)
	}
	if err := probeRateLimit(time.Now()); err == nil {
		t.Fatal("second probe within interval should be rate-limited")
	}
	probeLastRun = time.Now().Add(-2 * time.Hour)
	if err := probeRateLimit(time.Now()); err != nil {
		t.Fatalf("probe after interval should pass: %v", err)
	}
}

func TestValidateProbeResult(t *testing.T) {
	for _, ok := range []storageProbeResult{{Version: 1, Code: ""}, {Version: 1, Code: "device_policy_unresolved"}} {
		if err := validateProbeResult(ok); err != nil {
			t.Fatalf("valid result rejected: %v", err)
		}
	}
	for _, bad := range []storageProbeResult{{Version: 2, Code: ""}, {Version: 1, Code: "bogus"}} {
		if err := validateProbeResult(bad); err == nil {
			t.Fatalf("invalid result accepted: %+v", bad)
		}
	}
}
