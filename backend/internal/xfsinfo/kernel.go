// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package xfsinfo

import (
	"io"
	"os"
	"strings"
)

// KernelXfs is the read-only kernel XFS probe. It never loads a module
// and never opens a device node.
type KernelXfs struct {
	Present bool   `json:"present"`
	Reason  string `json:"reason"`
}

const (
	kernelXfsMaxBytes = 64 << 10
	reasonXfsMissing  = "The kernel XFS filesystem is not currently listed in /proc/filesystems (this page does not load modules)."
	reasonXfsUnread   = "Could not read /proc/filesystems."
	reasonXfsShort    = "Kernel XFS is not currently listed."
)

// ProbeKernelXfs reports whether the kernel lists the xfs filesystem type.
// osRoot is the collect root (tests pass a fixture tree). Fail-closed:
// unreadable or oversized input is present=false with fixed text.
func ProbeKernelXfs(osRoot string) KernelXfs {
	if osRoot == "" {
		osRoot = "/"
	}
	root, err := os.OpenRoot(osRoot)
	if err != nil {
		return KernelXfs{Present: false, Reason: reasonXfsUnread}
	}
	defer root.Close()
	f, err := root.Open("proc/filesystems")
	if err != nil {
		return KernelXfs{Present: false, Reason: reasonXfsUnread}
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, kernelXfsMaxBytes+1))
	if err != nil || len(b) > kernelXfsMaxBytes {
		return KernelXfs{Present: false, Reason: reasonXfsUnread}
	}
	if parseKernelXfs(string(b)) {
		return KernelXfs{Present: true}
	}
	return KernelXfs{Present: false, Reason: reasonXfsMissing}
}

// parseKernelXfs is true when a /proc/filesystems line's last field is
// exactly "xfs".
func parseKernelXfs(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[len(fields)-1] == "xfs" {
			return true
		}
	}
	return false
}
