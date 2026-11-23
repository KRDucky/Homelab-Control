// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package f2fsinfo

import (
	"io"
	"os"
	"strings"
)

// KernelF2fs is the read-only kernel f2fs probe. It never loads a module
// and never opens a device node.
type KernelF2fs struct {
	Present bool   `json:"present"`
	Reason  string `json:"reason"`
}

const (
	kernelF2fsMaxBytes = 64 << 10
	reasonF2fsMissing  = "The kernel f2fs filesystem is not currently listed in /proc/filesystems (this page does not load modules)."
	reasonF2fsUnread   = "Could not read /proc/filesystems."
	reasonF2fsShort    = "Kernel f2fs is not currently listed."
)

// ProbeKernelF2fs reports whether the kernel lists the f2fs filesystem type.
// osRoot is the collect root (tests pass a fixture tree). Fail-closed:
// unreadable or oversized input is present=false with fixed text.
func ProbeKernelF2fs(osRoot string) KernelF2fs {
	if osRoot == "" {
		osRoot = "/"
	}
	root, err := os.OpenRoot(osRoot)
	if err != nil {
		return KernelF2fs{Present: false, Reason: reasonF2fsUnread}
	}
	defer root.Close()
	f, err := root.Open("proc/filesystems")
	if err != nil {
		return KernelF2fs{Present: false, Reason: reasonF2fsUnread}
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, kernelF2fsMaxBytes+1))
	if err != nil || len(b) > kernelF2fsMaxBytes {
		return KernelF2fs{Present: false, Reason: reasonF2fsUnread}
	}
	if parseKernelF2fs(string(b)) {
		return KernelF2fs{Present: true}
	}
	return KernelF2fs{Present: false, Reason: reasonF2fsMissing}
}

// parseKernelF2fs is true when a /proc/filesystems line's last field is
// exactly "f2fs".
func parseKernelF2fs(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[len(fields)-1] == "f2fs" {
			return true
		}
	}
	return false
}
