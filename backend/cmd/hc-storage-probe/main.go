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

// maxResponseBytes is the closed-protocol output frame cap. It is a provable
// bound: 64 devices × 16 KiB blkid = 1 MiB raw (LVM is gated off), which
// worst-case JSON-escapes to ~2 MiB, plus errors/framing — comfortably under
// 3 MiB and the broker's 4 MiB proto.MaxResponse.
const maxResponseBytes = 3 << 20

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
		// Missing/insecure tools must surface as a typed Partial result through
		// the closed protocol, not only a nonzero exit.
		res := storageprobe.Result{Version: 1, Partial: true, Errors: []string{"tool unavailable: " + err.Error()}}
		return writeResult(res)
	}
	res := storageprobe.Run(context.Background(), req)
	return writeResult(res)
}

// writeResult encodes a Result through the bounded frame writer and writes one
// complete protocol frame to stdout.
func writeResult(res storageprobe.Result) error {
	if err := storageprobe.WriteResponse(os.Stdout, res, maxResponseBytes); err != nil {
		return fmt.Errorf("response: %w", err)
	}
	return nil
}
