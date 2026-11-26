// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsinfo

import (
	"fmt"
	"io"
	"os"
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

// ParseSnapRAIDConfig parses the part of snapraid.conf this page shows, with
// SnapRAID's own grammar (cmdline/state.c, state_config): the keyword is the
// first token and the value is the rest of the line, so paths may contain
// spaces. "data NAME DIR" (and its deprecated alias "disk") names a data disk;
// a parity level ("parity", "1-parity" … "6-parity", and the aliases
// "q-parity", "r-parity", "z-parity") lists comma-separated split parity
// files; "content" is one file. Comments and blank lines are skipped; other
// keywords are ignored. A missing value, a non-absolute path, or a disk name
// SnapRAID would refuse is an error.
func ParseSnapRAIDConfig(text string) (SnapRAID, error) {
	out := SnapRAID{Data: []string{}, Parity: []string{}, Content: []string{}}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		key, value, _ := strings.Cut(line, " ")
		if k, v, ok := strings.Cut(line, "\t"); ok && len(k) < len(key) {
			key, value = k, v
		}
		value = strings.TrimSpace(value)
		switch {
		case key == "data" || key == "disk":
			name, dir, _ := strings.Cut(value, " ")
			if n, d, ok := strings.Cut(value, "\t"); ok && len(n) < len(name) {
				name, dir = n, d
			}
			dir = strings.TrimSpace(dir)
			if name == "" || strings.ContainsAny(name, "/\\:") || isParityKey(name) {
				return SnapRAID{}, fmt.Errorf("line %d: invalid data name", i+1)
			}
			if !strings.HasPrefix(dir, "/") {
				return SnapRAID{}, fmt.Errorf("line %d: data path must be absolute", i+1)
			}
			out.Data = append(out.Data, dir)
		case key == "content":
			if !strings.HasPrefix(value, "/") {
				return SnapRAID{}, fmt.Errorf("line %d: content path must be absolute", i+1)
			}
			out.Content = append(out.Content, value)
		case isParityKey(key):
			for _, part := range strings.Split(value, ",") {
				part = strings.Trim(part, " \t\r\n")
				if !strings.HasPrefix(part, "/") {
					return SnapRAID{}, fmt.Errorf("line %d: %s path must be absolute", i+1, key)
				}
				out.Parity = append(out.Parity, part)
			}
		}
		// Other keywords are ignored; includes are never followed.
	}
	return out, nil
}

// isParityKey reports whether key names a SnapRAID parity level
// (cmdline/state.c, lev_config_scan).
func isParityKey(key string) bool {
	switch key {
	case "parity", "1-parity", "q-parity", "2-parity", "r-parity", "3-parity", "z-parity", "4-parity", "5-parity", "6-parity":
		return true
	}
	return false
}
