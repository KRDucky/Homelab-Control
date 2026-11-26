// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsapply

import (
	"fmt"
	"strings"
)

const (
	FstabPath    = "/etc/fstab"
	FstabBackup  = "/etc/fstab.hc-mergerfs-bak"
	SnapraidPath = "/etc/snapraid.conf"
	SnapraidBak  = "/etc/snapraid.conf.hc-mergerfs-bak"
)

// FstabLine is the single mergerfs line for a pool. It does not format disks.
func (s Spec) FstabLine() (string, error) {
	if s.Action != ActionPersist && s.Action != ActionMount {
		return "", fmt.Errorf("no fstab line for %s", s.Action)
	}
	opt := "category.create=" + s.Policy + ",allow_other"
	if s.MinFree != "" {
		opt += ",minfreespace=" + s.MinFree
	}
	return strings.Join(s.Branches, ":") + " " + s.Mountpoint + " fuse.mergerfs " + opt + " 0 0", nil
}

// MergeFstab replaces an existing fuse.mergerfs line for the mountpoint, or
// appends one. A different filesystem already using that mountpoint is refused.
// Include lines are left untouched and are not opened.
func MergeFstab(existing, line, mountpoint string) (string, error) {
	if strings.Contains(line, "\n") || strings.TrimSpace(line) == "" || mountpoint == "" {
		return "", fmt.Errorf("refusing to write a multi-line fstab record")
	}
	var rawLines []string
	if existing != "" {
		rawLines = strings.Split(strings.TrimSuffix(existing, "\n"), "\n")
	}
	replaced := false
	lines := make([]string, 0, len(rawLines)+1)
	for _, raw := range rawLines {
		trim := strings.TrimSpace(raw)
		if trim == "" || strings.HasPrefix(trim, "#") {
			lines = append(lines, raw)
			continue
		}
		fields := strings.Fields(raw)
		if len(fields) >= 3 && fields[1] == mountpoint {
			if fields[2] != "fuse.mergerfs" {
				return "", fmt.Errorf("mountpoint is already used by a different filesystem")
			}
			if replaced {
				return "", fmt.Errorf("more than one fstab line uses this mountpoint")
			}
			lines = append(lines, line)
			replaced = true
			continue
		}
		lines = append(lines, raw)
	}
	if !replaced {
		lines = append(lines, line)
	}
	out := strings.Join(lines, "\n") + "\n"
	if strings.Contains(out, "include ") || strings.Contains(out, "..") {
		return "", fmt.Errorf("refusing to write an fstab that includes another file")
	}
	return out, nil
}

// SnapraidManagedHeader is the first line of every snapraid.conf this helper
// writes. A file without it was written by someone else and is not replaced.
const SnapraidManagedHeader = "# Written by Homelab Control (MergerFS array definition). Saving the array definition again replaces this file."

// SnapraidReplaceable reports whether persist may replace the existing
// snapraid.conf: it is absent or empty, or this helper wrote it. A hand-written
// file can carry other disks, disk names SnapRAID's content file is keyed on,
// exclude rules and settings, so it is never rewritten.
func SnapraidReplaceable(existing []byte) bool {
	if strings.TrimSpace(string(existing)) == "" {
		return true
	}
	first, _, _ := strings.Cut(string(existing), "\n")
	return strings.TrimRight(first, "\r") == SnapraidManagedHeader
}

// SnapraidConf is a complete snapraid.conf. Data names are d1, d2, ...
// Includes are not written and not followed.
func (s Spec) SnapraidConf() (string, error) {
	if s.Action != ActionPersist || len(s.Parity) == 0 || len(s.Content) < 2 {
		return "", fmt.Errorf("snapraid.conf requires parity and two content files")
	}
	var b strings.Builder
	b.WriteString(SnapraidManagedHeader + "\n")
	for i, p := range s.Parity {
		if i == 0 {
			fmt.Fprintf(&b, "parity %s\n", p)
		} else {
			fmt.Fprintf(&b, "%d-parity %s\n", i+1, p)
		}
	}
	for _, c := range s.Content {
		fmt.Fprintf(&b, "content %s\n", c)
	}
	for i, d := range s.Branches {
		fmt.Fprintf(&b, "data d%d %s\n", i+1, d)
	}
	out := b.String()
	if strings.Contains(out, "\ninclude") || strings.HasPrefix(out, "include") || strings.Contains(out, "..") {
		return "", fmt.Errorf("refusing to write a snapraid include")
	}
	return out, nil
}
