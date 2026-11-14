// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package lvminfo

import (
	"io"
	"os"
	"strings"
)

// KernelDM is the read-only device-mapper probe. It never loads a module
// and never opens a device node.
type KernelDM struct {
	Present bool   `json:"present"`
	Reason  string `json:"reason"`
}

const (
	kernelDMMaxBytes = 64 << 10
	// reasonDMMissing is the stable wording for an unlisted device-mapper
	// target, including when dm_mod is not loaded. Do not replace it with a
	// "module missing" instruction. This page does not load modules.
	reasonDMMissing      = "The kernel device-mapper target is not currently listed in /proc/devices (this page does not load modules)."
	reasonDMUnread       = "Could not read /proc/devices."
	reasonDMMissingShort = "Kernel device-mapper is not currently listed."
)

// ProbeKernelDM reports whether /proc/devices lists device-mapper.
// Unreadable or oversized input is present=false with fixed text.
func ProbeKernelDM(osRoot string) KernelDM {
	if osRoot == "" {
		osRoot = "/"
	}
	root, err := os.OpenRoot(osRoot)
	if err != nil {
		return KernelDM{Present: false, Reason: reasonDMUnread}
	}
	defer root.Close()
	f, err := root.Open("proc/devices")
	if err != nil {
		return KernelDM{Present: false, Reason: reasonDMUnread}
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, kernelDMMaxBytes+1))
	if err != nil || len(b) > kernelDMMaxBytes {
		return KernelDM{Present: false, Reason: reasonDMUnread}
	}
	if parseKernelDM(string(b)) {
		return KernelDM{Present: true}
	}
	return KernelDM{Present: false, Reason: reasonDMMissing}
}

// parseKernelDM is true when a /proc/devices line's last field is exactly
// "device-mapper".
func parseKernelDM(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[len(fields)-1] == "device-mapper" {
			return true
		}
	}
	return false
}
