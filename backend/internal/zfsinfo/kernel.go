// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package zfsinfo

import (
	"io"
	"os"
	"strings"
)

// KernelZfs is the read-only kernel ZFS probe. It never loads a module
// and never opens a device node (hc-api has PrivateDevices=yes).
type KernelZfs struct {
	Present bool   `json:"present"`
	Reason  string `json:"reason"`
}

const (
	kernelZfsMaxBytes     = 64 << 10
	reasonZfsMissing      = "The kernel ZFS filesystem is not currently listed in /proc/filesystems (this page does not load modules)."
	reasonZfsUnread       = "Could not read /proc/filesystems."
	reasonZfsMissingShort = "Kernel ZFS is not currently listed."
)

// ProbeKernelZfs reports whether the kernel lists the zfs filesystem type.
// osRoot is the collect root (tests pass a fixture tree). Fail-closed:
// unreadable or oversized input is present=false with fixed text.
func ProbeKernelZfs(osRoot string) KernelZfs {
	if osRoot == "" {
		osRoot = "/"
	}
	root, err := os.OpenRoot(osRoot)
	if err != nil {
		return KernelZfs{Present: false, Reason: reasonZfsUnread}
	}
	defer root.Close()
	f, err := root.Open("proc/filesystems")
	if err != nil {
		return KernelZfs{Present: false, Reason: reasonZfsUnread}
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, kernelZfsMaxBytes+1))
	if err != nil || len(b) > kernelZfsMaxBytes {
		return KernelZfs{Present: false, Reason: reasonZfsUnread}
	}
	if parseKernelZfs(string(b)) {
		return KernelZfs{Present: true}
	}
	return KernelZfs{Present: false, Reason: reasonZfsMissing}
}

// parseKernelZfs is true when a /proc/filesystems line's last field is
// exactly "zfs".
func parseKernelZfs(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[len(fields)-1] == "zfs" {
			return true
		}
	}
	return false
}
