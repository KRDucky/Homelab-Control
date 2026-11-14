// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsinfo

import (
	"io"
	"os"
	"strings"
)

// FuseAccess is the read-only /etc/fuse.conf probe. It never edits the file
// and never remounts. A missing file is not a capability failure.
type FuseAccess struct {
	Present        bool   `json:"present"`
	UserAllowOther bool   `json:"userAllowOther"`
	Reason         string `json:"reason"`
}

const (
	fuseConfMax            = 4096
	reasonFuseConfMissing  = "optional: no /etc/fuse.conf"
	reasonFuseConfUnread   = "Could not read /etc/fuse.conf."
	fuseConfUserAllowOther = "user_allow_other"
)

// ProbeFuseAccess reports whether /etc/fuse.conf exists and contains an
// uncommented user_allow_other line. osRoot is the collect root (tests pass
// a fixture tree). Symlinks are not followed. Unreadable, non-regular, or
// oversized input is present=false with fixed text that does not echo the file.
func ProbeFuseAccess(osRoot string) FuseAccess {
	if osRoot == "" {
		osRoot = "/"
	}
	root, err := os.OpenRoot(osRoot)
	if err != nil {
		return FuseAccess{Reason: reasonFuseConfUnread}
	}
	defer root.Close()
	info, err := root.Lstat("etc/fuse.conf")
	if err != nil {
		if os.IsNotExist(err) {
			return FuseAccess{Reason: reasonFuseConfMissing}
		}
		return FuseAccess{Reason: reasonFuseConfUnread}
	}
	if !info.Mode().IsRegular() || info.Size() > fuseConfMax {
		return FuseAccess{Reason: reasonFuseConfUnread}
	}
	f, err := root.Open("etc/fuse.conf")
	if err != nil {
		return FuseAccess{Reason: reasonFuseConfUnread}
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, fuseConfMax+1))
	if err != nil || len(b) > fuseConfMax {
		return FuseAccess{Reason: reasonFuseConfUnread}
	}
	return FuseAccess{Present: true, UserAllowOther: parseUserAllowOther(string(b))}
}

// parseUserAllowOther is true only for a non-comment line whose trimmed
// text is exactly user_allow_other.
func parseUserAllowOther(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == fuseConfUserAllowOther {
			return true
		}
	}
	return false
}
