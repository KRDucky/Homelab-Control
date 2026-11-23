// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ext4info

import (
	"io"
	"os"
	"strings"
)

// KernelExt4 is the read-only kernel ext4 probe. It never loads a module
// and never opens a device node.
type KernelExt4 struct {
	Present bool   `json:"present"`
	Reason  string `json:"reason"`
}

const (
	kernelExt4MaxBytes = 64 << 10
	reasonExt4Missing  = "The kernel ext4 filesystem is not currently listed in /proc/filesystems (this page does not load modules)."
	reasonExt4Unread   = "Could not read /proc/filesystems."
	reasonExt4Short    = "Kernel ext4 is not currently listed."
)

// ProbeKernelExt4 reports whether the kernel lists the ext4 filesystem type.
// osRoot is the collect root (tests pass a fixture tree). Fail-closed:
// unreadable or oversized input is present=false with fixed text.
func ProbeKernelExt4(osRoot string) KernelExt4 {
	if osRoot == "" {
		osRoot = "/"
	}
	root, err := os.OpenRoot(osRoot)
	if err != nil {
		return KernelExt4{Present: false, Reason: reasonExt4Unread}
	}
	defer root.Close()
	f, err := root.Open("proc/filesystems")
	if err != nil {
		return KernelExt4{Present: false, Reason: reasonExt4Unread}
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, kernelExt4MaxBytes+1))
	if err != nil || len(b) > kernelExt4MaxBytes {
		return KernelExt4{Present: false, Reason: reasonExt4Unread}
	}
	if parseKernelExt4(string(b)) {
		return KernelExt4{Present: true}
	}
	return KernelExt4{Present: false, Reason: reasonExt4Missing}
}

// parseKernelExt4 is true when a /proc/filesystems line's last field is
// exactly "ext4".
func parseKernelExt4(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[len(fields)-1] == "ext4" {
			return true
		}
	}
	return false
}
