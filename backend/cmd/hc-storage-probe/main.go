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

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "hc-storage-probe:", err)
		os.Exit(1)
	}
}

func run() error {
	b, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil {
		return fmt.Errorf("reading request: %w", err)
	}
	req, err := storageprobe.UnmarshalRequest(b)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	res := storageprobe.Run(context.Background(), req)
	out, err := storageprobe.MarshalResponse(res)
	if err != nil {
		return fmt.Errorf("response: %w", err)
	}
	if _, err := os.Stdout.Write(out); err != nil {
		return fmt.Errorf("writing response: %w", err)
	}
	return nil
}
