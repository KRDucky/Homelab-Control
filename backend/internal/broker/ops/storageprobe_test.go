// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/storagetools"
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
	for _, ok := range []storageProbeResult{{Version: 2, Code: ""}, {Version: 2, Code: "device_policy_unresolved"}} {
		if err := validateProbeResult(ok); err != nil {
			t.Fatalf("valid result rejected: %v", err)
		}
	}
	for _, bad := range []storageProbeResult{{Version: 1, Code: ""}, {Version: 2, Code: "bogus"}} {
		if err := validateProbeResult(bad); err == nil {
			t.Fatalf("invalid result accepted: %+v", bad)
		}
	}
}

// fakeHelper answers each connection with reply and records request scopes.
func fakeHelper(t *testing.T, reply storagetools.Result) (scopes *[]string, calls *atomic.Int32) {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "probe.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	old := storageProbeSocket
	storageProbeSocket = sock
	t.Cleanup(func() { storageProbeSocket = old; ln.Close() })
	scopes, calls = &[]string{}, &atomic.Int32{}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			var req storagetools.Request
			line, _ := bufio.NewReader(c).ReadBytes('\n')
			_ = json.Unmarshal(line, &req)
			*scopes = append(*scopes, req.Scope)
			calls.Add(1)
			_ = json.NewEncoder(c).Encode(reply)
			c.Close()
		}
	}()
	return scopes, calls
}

func TestBtrfsStatusAsksForItsScopeAndCaches(t *testing.T) {
	btrfsCache = scopeCache{}
	scopes, calls := fakeHelper(t, storagetools.Result{Version: storagetools.ProtocolVersion, Tools: map[string]bool{},
		Btrfs: &storagetools.BtrfsReport{Filesystems: []storagetools.BtrfsFilesystem{{Mountpoint: "/srv/a"}}}})
	for i := 0; i < 2; i++ {
		out, err := btrfsStatus(context.Background(), nil, nil)
		rep, ok := out.(*storagetools.BtrfsReport)
		if err != nil || !ok || rep.Filesystems[0].Mountpoint != "/srv/a" {
			t.Fatalf("%v %#v", err, out)
		}
	}
	if calls.Load() != 1 || (*scopes)[0] != storagetools.ScopeBtrfs {
		t.Fatalf("helper calls=%d scopes=%v (second call inside the interval must be cached)", calls.Load(), *scopes)
	}
}

func TestZFSStatusWithoutReportIsUnavailable(t *testing.T) {
	zfsCache = scopeCache{}
	fakeHelper(t, storagetools.Result{Version: storagetools.ProtocolVersion, Tools: map[string]bool{}, Partial: true,
		Errors: []string{"zpool/zfs are not installed (package zfsutils-linux); no ZFS data"}})
	_, err := zfsStatus(context.Background(), nil, nil)
	var pe *proto.Error
	if !errors.As(err, &pe) || pe.Code != proto.CodeUnavailable || pe.Message != "zpool/zfs are not installed (package zfsutils-linux); no ZFS data" {
		t.Fatalf("%v", err)
	}
}
