// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsinfo

import (
	"io"
	"os"
	"strings"
)

// KernelFuse is the read-only kernel FUSE probe. It never loads a module
// and never opens /dev/fuse (hc-api has PrivateDevices=yes).
type KernelFuse struct {
	Present bool   `json:"present"`
	Reason  string `json:"reason"`
}

const (
	kernelFuseMaxBytes = 64 << 10
	reasonFuseMissing  = "The kernel FUSE filesystem is not currently listed in /proc/filesystems (the module may load on first mount)."
	reasonFuseUnread   = "Could not read /proc/filesystems."
)

// ProbeKernelFuse reports whether the kernel lists the fuse filesystem
// type. osRoot is the collect root (tests pass a fixture tree). Fail-closed:
// unreadable or oversized input is present=false with fixed text.
func ProbeKernelFuse(osRoot string) KernelFuse {
	if osRoot == "" {
		osRoot = "/"
	}
	root, err := os.OpenRoot(osRoot)
	if err != nil {
		return KernelFuse{Present: false, Reason: reasonFuseUnread}
	}
	defer root.Close()
	f, err := root.Open("proc/filesystems")
	if err != nil {
		return KernelFuse{Present: false, Reason: reasonFuseUnread}
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, kernelFuseMaxBytes+1))
	if err != nil || len(b) > kernelFuseMaxBytes {
		return KernelFuse{Present: false, Reason: reasonFuseUnread}
	}
	if parseKernelFuse(string(b)) {
		return KernelFuse{Present: true}
	}
	return KernelFuse{Present: false, Reason: reasonFuseMissing}
}

// parseKernelFuse is true when a /proc/filesystems line's last field is
// exactly "fuse". fusectl and fuseblk do not count.
func parseKernelFuse(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[len(fields)-1] == "fuse" {
			return true
		}
	}
	return false
}
