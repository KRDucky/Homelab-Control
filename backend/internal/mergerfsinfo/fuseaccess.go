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
// a fixture tree). Both the etc directory and fuse.conf are opened with
// openat and O_NOFOLLOW, so a symlink at either level (even one inside the
// root) is not followed. A FIFO is opened O_NONBLOCK and rejected unless it
// is a regular file. Unreadable, non-regular, or oversized input is
// present=false with fixed text that does not echo the file.
func ProbeFuseAccess(osRoot string) FuseAccess {
	if osRoot == "" {
		osRoot = "/"
	}
	root, err := os.OpenFile(osRoot, os.O_RDONLY|syscall.O_DIRECTORY, 0)
	if err != nil {
		return FuseAccess{Reason: reasonFuseConfUnread}
	}
	defer root.Close()
	// O_NOFOLLOW on the directory too: an "etc" that is a symlink (even one
	// inside the root) must not be followed to a different directory.
	etcFd, err := openatNoFollow(root.Fd(), "etc", syscall.O_RDONLY|syscall.O_DIRECTORY)
	if err != nil {
		if os.IsNotExist(err) {
			return FuseAccess{Reason: reasonFuseConfMissing}
		}
		return FuseAccess{Reason: reasonFuseConfUnread}
	}
	etc := os.NewFile(uintptr(etcFd), "etc")
	defer etc.Close()
	// root and etc are directories; Go opens directories in blocking mode,
	// so Fd() returns the descriptor without a non-blocking→blocking
	// transition.
	fd, err := openatNoFollow(etc.Fd(), "fuse.conf", syscall.O_RDONLY|syscall.O_NONBLOCK)
	if err != nil {
		if os.IsNotExist(err) {
			return FuseAccess{Reason: reasonFuseConfMissing}
		}
		return FuseAccess{Reason: reasonFuseConfUnread}
	}
	// os.NewFile takes ownership of fd; the deferred Close releases it on
	// every return below. Do not syscall.Close(fd) separately.
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

// parseUserAllowOther reports whether text contains a user_allow_other
// directive, matching fuser_conf.c: only complete newline-terminated lines
// are parsed, text after '#' is a comment, and only C isspace (ASCII space,
// tab, newline, vertical tab, form feed, carriage return) is trimmed. Go's
// strings.TrimSpace would also trim Unicode whitespace, so it is not used.
func parseUserAllowOther(text string) bool {
	lines := strings.Split(text, "\n")
	last := len(lines) - 1
	// A final segment without a trailing newline is an unterminated line
	// that fuser_conf.c ignores.
	if !strings.HasSuffix(text, "\n") {
		last--
	}
	for i := 0; i <= last; i++ {
		// fgets(line, 256) holds at most 255 bytes including the newline, so
		// content longer than 254 bytes is discarded by fusermount.
		if len(lines[i]) > 254 {
			continue
		}
		if matchUserAllowOtherLine(lines[i]) {
			return true
		}
	}
	return false
}

// matchUserAllowOtherLine applies strip_line (drop text from '#') and trims
// C isspace, then compares to the directive. An embedded NUL fails closed:
// C strlen would stop there and never see the newline, discarding the line.
func matchUserAllowOtherLine(line string) bool {
	if strings.IndexByte(line, 0) >= 0 {
		return false
	}
	if i := strings.IndexByte(line, '#'); i >= 0 {
		line = line[:i]
	}
	line = strings.Trim(line, " \t\n\v\f\r")
	return line == fuseConfUserAllowOther
}

// openatNoFollow opens name under dirfd with O_NOFOLLOW|O_CLOEXEC added to
// flags, retrying EINTR a bounded number of times.
func openatNoFollow(dirfd uintptr, name string, flags int) (int, error) {
	const maxEINTR = 8
	var fd int
	var err error
	for i := 0; i < maxEINTR; i++ {
		fd, err = rawOpenat(dirfd, name, flags)
		if err != syscall.EINTR {
			return fd, err
		}
	}
	return fd, err
}

// rawOpenat is a var so a test can inject a transient EINTR.
var rawOpenat = func(dirfd uintptr, name string, flags int) (int, error) {
	return syscall.Openat(int(dirfd), name, flags|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
}
