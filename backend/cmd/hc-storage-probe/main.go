// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Command hc-storage-probe is the M2-4 native read-only storage probe helper.
// It reads one closed typed Request from stdin, runs the fixed non-mutating
// LVM/blkid templates on enumerated, revalidated devices, and writes one
// closed typed Result to stdout. It is disabled by default (its unit has no
// [Install] enablement) and is not run by hc-api or hc-broker.
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"homelab-control/backend/internal/storageprobe"
)

// maxRequestBytes is the closed-protocol input cap. Reading one byte more than
// this proves the request is oversized, so a truncated request is rejected.
const maxRequestBytes = 1 << 20

// maxResponseBytes is the closed-protocol output frame cap. Result fields are
// individually bounded; this is the hard frame limit on the marshalled reply.
const maxResponseBytes = 24 << 20

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "hc-storage-probe:", err)
		os.Exit(1)
	}
}

// readRequest reads a bounded request and rejects any input over the cap.
func readRequest(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxRequestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading request: %w", err)
	}
	if len(b) > maxRequestBytes {
		return nil, fmt.Errorf("request too large: %d bytes, max %d", len(b), maxRequestBytes)
	}
	return b, nil
}

func run() error {
	b, err := readRequest(os.Stdin)
	if err != nil {
		return err
	}
	req, err := storageprobe.UnmarshalRequest(b)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	if err := storageprobe.ResolveToolPaths(); err != nil {
		return fmt.Errorf("tools: %w", err)
	}
	res := storageprobe.Run(context.Background(), req)
	out, err := storageprobe.MarshalResponse(res)
	if err != nil {
		return fmt.Errorf("response: %w", err)
	}
	if len(out) > maxResponseBytes {
		return fmt.Errorf("response too large: %d bytes, max %d", len(out), maxResponseBytes)
	}
	if _, err := os.Stdout.Write(out); err != nil {
		return fmt.Errorf("writing response: %w", err)
	}
	return nil
}
