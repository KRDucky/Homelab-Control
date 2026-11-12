// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsinfo

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// DefaultSnapRAIDPath is where hc-api looks for snapraid.conf unless
// -snapraid-config overrides it.
const DefaultSnapRAIDPath = "/etc/snapraid.conf"

// snapraidMaxBytes caps a single snapraid.conf (it is parsed whole).
const snapraidMaxBytes = 256 << 10

// SnapRAID is the optional SnapRAID half of the response. Absence of a
// config is normal (SnapRAID is optional), not an error.
type SnapRAID struct {
	Present    bool     `json:"present"`
	Reason     string   `json:"reason"`
	ConfigPath string   `json:"configPath"`
	Data       []string `json:"data"`
	Parity     []string `json:"parity"`
	Content    []string `json:"content"`
}

// ReadSnapRAID opens and parses one config path, following no includes and
// opening none of the listed paths. A missing or invalid file yields
// Present=false with a reason, never an error to the caller.
func ReadSnapRAID(path string) SnapRAID {
	if path == "" {
		path = DefaultSnapRAIDPath
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return SnapRAID{Present: false, Reason: "optional: no snapraid.conf"}
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return SnapRAID{Present: false, Reason: "optional: snapraid.conf is not a regular file"}
	}
	b, err := io.ReadAll(io.LimitReader(f, snapraidMaxBytes+1))
	if err != nil {
		return SnapRAID{Present: false, Reason: "optional: snapraid.conf is unreadable"}
	}
	if len(b) > snapraidMaxBytes {
		return SnapRAID{Present: false, Reason: "optional: snapraid.conf exceeds the size limit"}
	}
	out, err := ParseSnapRAIDConfig(string(b))
	if err != nil {
		return SnapRAID{Present: false, Reason: "optional: snapraid.conf is invalid"}
	}
	out.Present = true
	out.ConfigPath = path
	return out
}

// ParseSnapRAIDConfig parses the recognised subset of snapraid.conf: data,
// parity/2-parity/3-parity/..., and content keys with exactly one absolute
// path value. Comments and blank lines are skipped; unknown keys are ignored.
// Trailing garbage after a recognised key, a missing value, or a non-absolute
// path is an error.
func ParseSnapRAIDConfig(text string) (SnapRAID, error) {
	out := SnapRAID{Data: []string{}, Parity: []string{}, Content: []string{}}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		key := fields[0]
		if key == "data" || key == "parity" || key == "content" || isParityKey(key) {
			if len(fields) != 2 {
				return SnapRAID{}, fmt.Errorf("line %d: %s requires exactly one absolute path", i+1, key)
			}
			if !strings.HasPrefix(fields[1], "/") {
				return SnapRAID{}, fmt.Errorf("line %d: %s path must be absolute", i+1, key)
			}
			switch {
			case key == "data":
				out.Data = append(out.Data, fields[1])
			case key == "content":
				out.Content = append(out.Content, fields[1])
			default:
				out.Parity = append(out.Parity, fields[1])
			}
		}
		// Unknown keys are ignored; includes are never followed.
	}
	return out, nil
}

// isParityKey reports whether key is parity or N-parity (2-parity, 3-parity,
// ...). It never accepts a bare numeric prefix without "-parity".
func isParityKey(key string) bool {
	if key == "parity" {
		return true
	}
	before, after, ok := strings.Cut(key, "-")
	if !ok || after != "parity" {
		return false
	}
	n, err := strconv.Atoi(before)
	return err == nil && n >= 2
}
