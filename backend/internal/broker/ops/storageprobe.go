// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/storagetools"
)

// storageProbeSocket is the fixed root-only socket-activated helper path. The
// systemd unit owns this socket (root:root 0600); the op is only its client.
const storageProbeSocket = "/run/homelab-control/storage-probe.sock"

// Broker-side interval rate limit. The helper's own limiter resets on every
// socket activation (Accept=yes starts a fresh process), so the broker — a
// long-running process — must bound how often it wakes disks.
var (
	probeRateMu      sync.Mutex
	probeLastRun     time.Time
	minProbeInterval = 30 * time.Second
)

// probeRateLimit refuses a probe inside minProbeInterval of the last one.
func probeRateLimit(now time.Time) error {
	probeRateMu.Lock()
	defer probeRateMu.Unlock()
	if now.Sub(probeLastRun) < minProbeInterval {
		return &proto.Error{Code: proto.CodeRateLimited, Message: "storage probe rate-limited; retry shortly"}
	}
	probeLastRun = now
	return nil
}

// validateProbeResult rejects an unsupported helper protocol version or an
// unknown result code so the API never treats them as success.
func validateProbeResult(res storageProbeResult) error {
	if res.Version != storagetools.ProtocolVersion {
		return fmt.Errorf("probe result version %d unsupported", res.Version)
	}
	switch res.Code {
	case "", storagetools.CodeDevicePolicyUnresolved:
		return nil
	default:
		return fmt.Errorf("probe result code %q unsupported", res.Code)
	}
}

// storageProbeMaxResponse bounds the helper's typed Result read: lsblk, SMART
// for up to 64 disks and two LVM reports, under proto.MaxResponse (4 MiB).
const storageProbeMaxResponse = 3 << 20

type (
	storageProbeRequest = storagetools.Request
	storageProbeResult  = storagetools.Result
)

// storageProbe asks the socket-activated helper for its read-only tool
// output (lsblk, smartctl, pvs, lvs). The broker never opens a device or runs
// a storage tool itself; it relays a closed request and reads a capped result.
func storageProbe(ctx context.Context, _ *Env, raw json.RawMessage) (any, error) {
	if err := requireNoArgs(raw); err != nil {
		return nil, err
	}
	if err := probeRateLimit(time.Now()); err != nil {
		return nil, err
	}
	res, err := callProbeHelper(ctx, storageProbeRequest{Version: storagetools.ProtocolVersion})
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: err.Error()}
	}
	return res, nil
}

// callProbeHelper writes the closed request to the helper socket, half-closes
// the write side so the helper sees EOF on its stdin, and reads a size-capped
// typed Result.
func callProbeHelper(ctx context.Context, req storageProbeRequest) (storageProbeResult, error) {
	var zero storageProbeResult
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", storageProbeSocket)
	if err != nil {
		return zero, fmt.Errorf("storage probe helper unavailable: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return zero, err
	}
	body = append(body, '\n')
	if _, err := conn.Write(body); err != nil {
		return zero, fmt.Errorf("writing probe request: %w", err)
	}
	if uc, ok := conn.(*net.UnixConn); ok {
		_ = uc.CloseWrite() // half-close: helper's stdin reaches EOF
	}
	b, err := io.ReadAll(io.LimitReader(conn, storageProbeMaxResponse+1))
	if err != nil {
		return zero, fmt.Errorf("reading probe result: %w", err)
	}
	if len(b) > storageProbeMaxResponse {
		return zero, fmt.Errorf("probe result exceeded %d bytes", storageProbeMaxResponse)
	}
	var res storageProbeResult
	if err := json.Unmarshal(b, &res); err != nil {
		return zero, fmt.Errorf("decoding probe result: %w", err)
	}
	if err := validateProbeResult(res); err != nil {
		return zero, err
	}
	return res, nil
}

// zfsMinInterval bounds how often the broker asks the helper for the ZFS
// report; a page refresh inside it gets the previous answer.
var (
	zfsMu          sync.Mutex
	zfsLastRun     time.Time
	zfsLast        *storagetools.ZFSReport
	zfsLastErr     error
	zfsMinInterval = 3 * time.Second
)

// zfsStatus asks the helper for the OpenZFS report (scope zfs).
func zfsStatus(ctx context.Context, _ *Env, raw json.RawMessage) (any, error) {
	if err := requireNoArgs(raw); err != nil {
		return nil, err
	}
	zfsMu.Lock()
	defer zfsMu.Unlock()
	if time.Since(zfsLastRun) < zfsMinInterval {
		return zfsLast, zfsLastErr
	}
	zfsLastRun = time.Now()
	res, err := callProbeHelper(ctx, storageProbeRequest{Version: storagetools.ProtocolVersion, Scope: storagetools.ScopeZFS})
	switch {
	case err != nil:
		zfsLast, zfsLastErr = nil, &proto.Error{Code: proto.CodeUnavailable, Message: err.Error()}
	case res.ZFS == nil:
		zfsLast, zfsLastErr = nil, &proto.Error{Code: proto.CodeUnavailable, Message: strings.Join(res.Errors, "; ")}
	default:
		zfsLast, zfsLastErr = res.ZFS, nil
	}
	return zfsLast, zfsLastErr
}
