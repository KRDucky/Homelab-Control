// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsinfo

import (
	"io"
	"os"
	"strings"
)

// KernelRelease is the read-only kernel version string from
// /proc/sys/kernel/osrelease. It never runs uname or mergerfs --version.
type KernelRelease struct {
	Present bool   `json:"present"`
	Release string `json:"release"`
	Reason  string `json:"reason"`
}

const (
	kernelReleaseMaxBytes = 256
	kernelReleaseMax      = 64
	reasonReleaseUnread   = "Could not read /proc/sys/kernel/osrelease."
)

// ProbeKernelRelease reports the kernel release from the collect root.
// osRoot is "/" on the host; tests pass a fixture tree. Fail-closed:
// unreadable, oversized, or invalid content is present=false with fixed
// text. The string is never executed as a command.
func ProbeKernelRelease(osRoot string) KernelRelease {
	if osRoot == "" {
		osRoot = "/"
	}
	root, err := os.OpenRoot(osRoot)
	if err != nil {
		return KernelRelease{Reason: reasonReleaseUnread}
	}
	defer root.Close()
	f, err := root.Open("proc/sys/kernel/osrelease")
	if err != nil {
		return KernelRelease{Reason: reasonReleaseUnread}
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, kernelReleaseMaxBytes+1))
	if err != nil || len(b) > kernelReleaseMaxBytes {
		return KernelRelease{Reason: reasonReleaseUnread}
	}
	rel, ok := parseKernelRelease(string(b))
	if !ok {
		return KernelRelease{Reason: reasonReleaseUnread}
	}
	return KernelRelease{Present: true, Release: rel}
}

// parseKernelRelease accepts a single trimmed token of letters, digits,
// dots, underscores, hyphens, and plus. Anything else, including a path
// or space, fails closed so the UI never echoes junk.
func parseKernelRelease(text string) (string, bool) {
	s := strings.TrimSpace(text)
	if s == "" || len(s) > kernelReleaseMax {
		return "", false
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' || r == '+' {
			continue
		}
		return "", false
	}
	return s, true
}
