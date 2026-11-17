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
	"os"
	"sync"
	"time"

	"homelab-control/backend/internal/broker/proto"
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
	if res.Version != 1 {
		return fmt.Errorf("probe result version %d unsupported", res.Version)
	}
	switch res.Code {
	case "", "device_policy_unresolved":
		return nil
	default:
		return fmt.Errorf("probe result code %q unsupported", res.Code)
	}
}

// storageProbeMaxResponse bounds the helper's typed Result read. It is a
// provable bound: at most maxDevices(64) × maxBlkidBytes(64 KiB) = 4 MiB of
// blkid output (LVM is gated off, so no LVS/PVS payload), plus JSON framing
// and errors — comfortably under 8 MiB.
const storageProbeMaxResponse = 8 << 20

// storageProbeRequest is the closed helper request (protocol version 1).
type storageProbeRequest struct {
	Version int      `json:"version"`
	Devices []string `json:"devices"`
}

// storageProbeResult mirrors the helper's closed typed Result. It is returned
// to the API verbatim; callers classify it by Code, not free text.
type storageProbeResult struct {
	Version int               `json:"version"`
	Code    string            `json:"code,omitempty"`
	LVS     string            `json:"lvs,omitempty"`
	PVS     string            `json:"pvs,omitempty"`
	Blkid   map[string]string `json:"blkid,omitempty"`
	Errors  []string          `json:"errors,omitempty"`
	Partial bool              `json:"partial"`
}

// storageProbe enumerates /sys/class/block and asks the socket-activated
// helper to probe them. The broker itself never opens a device or runs
// lvs/pvs/blkid; it only relays a closed request and reads a capped response.
func storageProbe(ctx context.Context, _ *Env, raw json.RawMessage) (any, error) {
	if err := requireNoArgs(raw); err != nil {
		return nil, err
	}
	if err := probeRateLimit(time.Now()); err != nil {
		return nil, err
	}
	names, err := enumerateBlockDevices()
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: err.Error()}
	}
	res, err := callProbeHelper(ctx, storageProbeRequest{Version: 1, Devices: names})
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: err.Error()}
	}
	return res, nil
}

// enumerateBlockDevices lists kernel block-device names under /sys/class/block.
// The helper revalidates each name before any open, so a stale entry is never
// probed.
func enumerateBlockDevices() ([]string, error) {
	ents, err := os.ReadDir("/sys/class/block")
	if err != nil {
		return nil, fmt.Errorf("enumerating block devices: %w", err)
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names, nil
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
