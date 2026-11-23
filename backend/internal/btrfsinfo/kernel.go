// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package btrfsinfo

import (
	"io"
	"os"
	"strings"
)

// KernelBtrfs is the read-only kernel Btrfs probe. It never loads a module
// and never opens a device node.
type KernelBtrfs struct {
	Present bool   `json:"present"`
	Reason  string `json:"reason"`
}

const (
	kernelBtrfsMaxBytes     = 64 << 10
	reasonBtrfsMissing      = "The kernel Btrfs filesystem is not currently listed in /proc/filesystems (this page does not load modules)."
	reasonBtrfsUnread       = "Could not read /proc/filesystems."
	reasonBtrfsMissingShort = "Kernel Btrfs is not currently listed."
)

// ProbeKernelBtrfs reports whether the kernel lists the btrfs filesystem type.
// osRoot is the collect root (tests pass a fixture tree). Fail-closed:
// unreadable or oversized input is present=false with fixed text.
func ProbeKernelBtrfs(osRoot string) KernelBtrfs {
	if osRoot == "" {
		osRoot = "/"
	}
	root, err := os.OpenRoot(osRoot)
	if err != nil {
		return KernelBtrfs{Present: false, Reason: reasonBtrfsUnread}
	}
	defer root.Close()
	f, err := root.Open("proc/filesystems")
	if err != nil {
		return KernelBtrfs{Present: false, Reason: reasonBtrfsUnread}
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, kernelBtrfsMaxBytes+1))
	if err != nil || len(b) > kernelBtrfsMaxBytes {
		return KernelBtrfs{Present: false, Reason: reasonBtrfsUnread}
	}
	if parseKernelBtrfs(string(b)) {
		return KernelBtrfs{Present: true}
	}
	return KernelBtrfs{Present: false, Reason: reasonBtrfsMissing}
}

// parseKernelBtrfs is true when a /proc/filesystems line's last field is
// exactly "btrfs".
func parseKernelBtrfs(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[len(fields)-1] == "btrfs" {
			return true
		}
	}
	return false
}
