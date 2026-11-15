// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsinfo

import (
	"io"
	"os"
	"strings"
	"syscall"
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
// a fixture tree). os.Root follows a final symlink that stays inside the
// root, so fuse.conf is opened with openat and O_NOFOLLOW and that error is
// not resolved. A FIFO is opened O_NONBLOCK and rejected unless it is a
// regular file. Unreadable, non-regular, or oversized input is present=false
// with fixed text that does not echo the file.
func ProbeFuseAccess(osRoot string) FuseAccess {
	if osRoot == "" {
		osRoot = "/"
	}
	root, err := os.OpenRoot(osRoot)
	if err != nil {
		return FuseAccess{Reason: reasonFuseConfUnread}
	}
	defer root.Close()
	etc, err := root.OpenFile("etc", os.O_RDONLY|syscall.O_DIRECTORY, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return FuseAccess{Reason: reasonFuseConfMissing}
		}
		return FuseAccess{Reason: reasonFuseConfUnread}
	}
	defer etc.Close()
	fd, err := syscall.Openat(int(etc.Fd()), "fuse.conf", syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return FuseAccess{Reason: reasonFuseConfMissing}
		}
		return FuseAccess{Reason: reasonFuseConfUnread}
	}
	f := os.NewFile(uintptr(fd), "fuse.conf")
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > fuseConfMax {
		return FuseAccess{Reason: reasonFuseConfUnread}
	}
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
