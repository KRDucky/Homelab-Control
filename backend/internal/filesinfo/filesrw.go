// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// File-manager read/write slice (owner authorization 2026-11-20, ADR 0012
// amendment). Every verb operates inside a confined root with openat +
// O_NOFOLLOW so last-component and intermediate symlinks are never followed
// out of the jail. hc-api still does not spawn and there is no broker file
// operation: these writes run as hc-api's own (unprivileged) uid, so a root
// that is not writable by hc-api fails closed with a permission error.
package filesinfo

import (
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

// Size caps for the read/write slice. Content read, download and content
// write share one cap; a multipart upload has its own. Both are enforced on
// the request body and again here so a bug in one layer cannot exceed them.
const (
	// MaxContentBytes bounds one file's content for read, download and
	// write/replace (4 MiB).
	MaxContentBytes = 4 << 20
	// MaxUploadBytes bounds a multipart upload (8 MiB).
	MaxUploadBytes = 8 << 20
	// MaxNameBytes bounds a single path component. NAME_MAX is 255 on the
	// supported filesystems.
	MaxNameBytes = 255
)

// Sentinel errors mapped by the API.
var (
	ErrNotRegular = errors.New("path is not a regular file")
	ErrBadName    = errors.New("name is not a valid single path segment")
	ErrExists     = errors.New("entry already exists")
	ErrNotEmpty   = errors.New("directory is not empty")
	ErrTooLarge   = errors.New("content exceeds the size limit")
)

// Target is a deletable entry's observable identity (kind, size, mtime). The
// delete plan binds its token to this so a post-plan change is refused as a
// conflict. It never carries contents or the host jail path.
type Target struct {
	Kind  string `json:"kind"`
	Size  int64  `json:"size"`
	Mtime string `json:"mtime"`
}

// Content is one capped read of a regular file.
type Content struct {
	Bytes     []byte
	Size      int64 // full size reported by stat, not the capped read length
	Truncated bool
}

// splitRel splits a validated relative path into its parent directory and
// base name. The empty directory is the jail root.
func splitRel(rel string) (string, string) {
	i := strings.LastIndex(rel, "/")
	if i < 0 {
		return "", rel
	}
	return rel[:i], rel[i+1:]
}

// openJail opens a confined root directory with O_NOFOLLOW|O_DIRECTORY.
func (s Set) openJail(id string) (*os.File, error) {
	r, ok := s.root(id)
	if !ok {
		if !s.BrowseAvailable() {
			return nil, ErrUnavailable
		}
		return nil, ErrUnknownRoot
	}
	jail, err := os.OpenFile(r.Path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_DIRECTORY, 0)
	if err != nil {
		return nil, mapOpenErr(err)
	}
	return jail, nil
}

// openDir opens the directory at rel (the jail root when rel is ".") with
// openat+O_NOFOLLOW. The returned close function releases the walked dir and
// the jail fd.
func (s Set) openDir(id, rel string) (*os.File, func(), error) {
	rel, err := RelPath(rel)
	if err != nil {
		return nil, nil, err
	}
	jail, err := s.openJail(id)
	if err != nil {
		return nil, nil, err
	}
	if rel == "." {
		return jail, func() { _ = jail.Close() }, nil
	}
	d, err := walkRel(jail, rel)
	if err != nil {
		_ = jail.Close()
		return nil, nil, mapOpenErr(err)
	}
	return d, func() { _ = d.Close(); _ = jail.Close() }, nil
}

// openParent opens the parent directory of rel (the jail root when rel is a
// single component) and returns it together with the base name and a close
// function. rel must name a file/dir inside the jail, not the root itself.
func (s Set) openParent(id, rel string) (*os.File, string, func(), error) {
	rel, err := RelPath(rel)
	if err != nil {
		return nil, "", nil, err
	}
	if rel == "." {
		return nil, "", nil, ErrNotRegular
	}
	dirPart, base := splitRel(rel)
	if dirPart == "" {
		jail, err := s.openJail(id)
		if err != nil {
			return nil, "", nil, err
		}
		return jail, base, func() { _ = jail.Close() }, nil
	}
	jail, err := s.openJail(id)
	if err != nil {
		return nil, "", nil, err
	}
	d, err := walkRel(jail, dirPart)
	if err != nil {
		_ = jail.Close()
		return nil, "", nil, mapOpenErr(err)
	}
	return d, base, func() { _ = d.Close(); _ = jail.Close() }, nil
}

// validName rejects a name that is not a single, safe, faithfully listable
// path component: no slash/backslash, NUL, dot segments, invalid UTF-8 or
// oversize. A name accepted here can never escape the jail or be omitted from
// a later listing.
func validName(name string) error {
	if name == "" || name == "." || name == ".." || len(name) > MaxNameBytes {
		return ErrBadName
	}
	if strings.ContainsAny(name, "/\\\x00") {
		return ErrBadName
	}
	if !utf8.ValidString(name) {
		return ErrBadName
	}
	return nil
}

func lstatAt(dirfd int, name string) (unix.Stat_t, error) {
	var st unix.Stat_t
	if err := unix.Fstatat(dirfd, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return st, err
	}
	return st, nil
}

func statKind(st *unix.Stat_t) string {
	switch st.Mode & syscall.S_IFMT {
	case syscall.S_IFDIR:
		return "dir"
	case syscall.S_IFREG:
		return "file"
	case syscall.S_IFLNK:
		return "symlink"
	case syscall.S_IFIFO:
		return "fifo"
	case syscall.S_IFSOCK:
		return "socket"
	case syscall.S_IFBLK:
		return "block"
	case syscall.S_IFCHR:
		return "char"
	default:
		return "other"
	}
}

func statMtime(st *unix.Stat_t) string {
	sec, nsec := st.Mtim.Unix()
	return time.Unix(sec, nsec).UTC().Format(time.RFC3339Nano)
}

// StatTarget describes one entry for the delete plan. It refuses the jail
// root and follows no symlink (AT_SYMLINK_NOFOLLOW reports the link itself).
func (s Set) StatTarget(id, rel string) (Target, error) {
	dir, base, closeFn, err := s.openParent(id, rel)
	if err != nil {
		return Target{}, err
	}
	defer closeFn()
	st, err := lstatAt(int(dir.Fd()), base)
	if err != nil {
		return Target{}, mapOpenErr(err)
	}
	return Target{Kind: statKind(&st), Size: int64(st.Size), Mtime: statMtime(&st)}, nil
}

// ReadContent reads a regular file with O_NOFOLLOW, capped at MaxContentBytes.
// A symlink is refused (never followed), a directory is not a regular file,
// and an oversize file is truncated at the cap with Truncated true.
func (s Set) ReadContent(id, rel string) (Content, error) {
	dir, base, closeFn, err := s.openParent(id, rel)
	if err != nil {
		return Content{}, err
	}
	defer closeFn()
	st, err := lstatAt(int(dir.Fd()), base)
	if err != nil {
		return Content{}, mapOpenErr(err)
	}
	if statKind(&st) != "file" {
		return Content{}, ErrNotRegular
	}
	fd, err := unix.Openat(int(dir.Fd()), base, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return Content{}, mapFileErr(err)
	}
	f := os.NewFile(uintptr(fd), base)
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, MaxContentBytes+1))
	if err != nil {
		return Content{}, err
	}
	truncated := len(buf) > MaxContentBytes
	if truncated {
		buf = buf[:MaxContentBytes]
	}
	return Content{Bytes: buf, Size: int64(st.Size), Truncated: truncated}, nil
}

// WriteContent replaces the content of an existing regular file (no create).
// O_NOFOLLOW refuses a symlink and the pre-stat refuses a directory or
// special file so a FIFO is never opened for writing (which would block).
func (s Set) WriteContent(id, rel string, data []byte) (Entry, error) {
	if len(data) > MaxContentBytes {
		return Entry{}, ErrTooLarge
	}
	dir, base, closeFn, err := s.openParent(id, rel)
	if err != nil {
		return Entry{}, err
	}
	defer closeFn()
	st, err := lstatAt(int(dir.Fd()), base)
	if err != nil {
		return Entry{}, mapOpenErr(err)
	}
	if statKind(&st) != "file" {
		return Entry{}, ErrNotRegular
	}
	fd, err := unix.Openat(int(dir.Fd()), base, unix.O_WRONLY|unix.O_TRUNC|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return Entry{}, mapFileErr(err)
	}
	f := os.NewFile(uintptr(fd), base)
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return Entry{}, mapWriteErr(err)
	}
	if err := f.Sync(); err != nil {
		return Entry{}, mapWriteErr(err)
	}
	var nst unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &nst); err != nil {
		return Entry{}, err
	}
	return entryFromStat(base, &nst), nil
}

// Create makes a new file or directory named name under parentRel (the jail
// root when parentRel is "."). O_CREAT|O_EXCL never overwrites and never
// follows a symlink.
func (s Set) Create(id, parentRel, name string, asDir bool) (Entry, error) {
	if err := validName(name); err != nil {
		return Entry{}, err
	}
	dir, closeFn, err := s.openDir(id, parentRel)
	if err != nil {
		return Entry{}, err
	}
	defer closeFn()
	if asDir {
		if err := unix.Mkdirat(int(dir.Fd()), name, 0o755); err != nil {
			return Entry{}, mapCreateErr(err)
		}
	} else {
		fd, err := unix.Openat(int(dir.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o644)
		if err != nil {
			return Entry{}, mapCreateErr(err)
		}
		_ = os.NewFile(uintptr(fd), name).Close()
	}
	st, err := lstatAt(int(dir.Fd()), name)
	if err != nil {
		return Entry{}, mapOpenErr(err)
	}
	return entryFromStat(name, &st), nil
}

// Rename moves srcRel to dstRel inside the same root. Both parents are opened
// with O_NOFOLLOW. Rename is destructive: it never replaces an existing
// destination. The destination is lstat'ed with AT_SYMLINK_NOFOLLOW first (a
// symlink, file, directory, FIFO or device there is refused as already_exists
// and the symlink is not followed), and the rename itself uses
// RENAME_NOREPLACE so a racing creator cannot slip past the check.
func (s Set) Rename(id, srcRel, dstRel string) (Entry, error) {
	srcDir, srcBase, srcClose, err := s.openParent(id, srcRel)
	if err != nil {
		return Entry{}, err
	}
	defer srcClose()
	dstDir, dstBase, dstClose, err := s.openParent(id, dstRel)
	if err != nil {
		return Entry{}, err
	}
	defer dstClose()
	// The destination must be absent: rename never overwrites. AT_SYMLINK_NOFOLLOW
	// reports the link itself, so a symlink destination is refused too.
	if _, err := lstatAt(int(dstDir.Fd()), dstBase); err == nil {
		return Entry{}, ErrExists
	} else if !errors.Is(err, syscall.ENOENT) {
		return Entry{}, mapOpenErr(err)
	}
	if err := unix.Renameat2(int(srcDir.Fd()), srcBase, int(dstDir.Fd()), dstBase, unix.RENAME_NOREPLACE); err != nil {
		return Entry{}, mapRenameErr(err)
	}
	st, err := lstatAt(int(dstDir.Fd()), dstBase)
	if err != nil {
		return Entry{}, mapOpenErr(err)
	}
	return entryFromStat(dstBase, &st), nil
}

// Delete removes a file, a symlink (the link itself) or an empty directory.
// Non-empty directories are refused (AT_REMOVEDIR returns ENOTEMPTY). Special
// files are refused to keep the slice conservative.
func (s Set) Delete(id, rel string) error {
	dir, base, closeFn, err := s.openParent(id, rel)
	if err != nil {
		return err
	}
	defer closeFn()
	st, err := lstatAt(int(dir.Fd()), base)
	if err != nil {
		return mapOpenErr(err)
	}
	var flags int
	switch statKind(&st) {
	case "dir":
		flags = unix.AT_REMOVEDIR
	case "file", "symlink":
		flags = 0
	default:
		return ErrNotRegular
	}
	if err := unix.Unlinkat(int(dir.Fd()), base, flags); err != nil {
		return mapDeleteErr(err)
	}
	return nil
}

// ValidName reports whether name is a safe single path segment for create,
// upload or rename destinations.
func ValidName(name string) error { return validName(name) }

func mapFileErr(err error) error {
	switch {
	case errors.Is(err, syscall.ELOOP), errors.Is(err, syscall.ENOTDIR), errors.Is(err, syscall.EISDIR):
		return ErrNotRegular
	case errors.Is(err, syscall.ENOENT):
		return ErrUnknownRoot
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return ErrPermission
	default:
		return err
	}
}

func mapWriteErr(err error) error {
	if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EROFS) {
		return ErrPermission
	}
	if errors.Is(err, syscall.ENOSPC) {
		return ErrTooLarge
	}
	return err
}

func mapCreateErr(err error) error {
	switch {
	case errors.Is(err, syscall.EEXIST):
		return ErrExists
	case errors.Is(err, syscall.ENOENT):
		return ErrUnknownRoot
	case errors.Is(err, syscall.ENOTDIR), errors.Is(err, syscall.ELOOP):
		return ErrNotDir
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM), errors.Is(err, syscall.EROFS):
		return ErrPermission
	case errors.Is(err, syscall.ENOSPC):
		return ErrTooLarge
	default:
		return err
	}
}

func mapRenameErr(err error) error {
	switch {
	case errors.Is(err, syscall.ENOENT):
		return ErrUnknownRoot
	case errors.Is(err, syscall.EEXIST):
		return ErrExists
	case errors.Is(err, syscall.ENOTEMPTY):
		return ErrNotEmpty
	case errors.Is(err, syscall.ENOTDIR), errors.Is(err, syscall.ELOOP):
		return ErrNotDir
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM), errors.Is(err, syscall.EROFS):
		return ErrPermission
	default:
		return err
	}
}

func mapDeleteErr(err error) error {
	switch {
	case errors.Is(err, syscall.ENOTEMPTY):
		return ErrNotEmpty
	case errors.Is(err, syscall.ENOENT):
		return ErrUnknownRoot
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM), errors.Is(err, syscall.EROFS):
		return ErrPermission
	default:
		return err
	}
}
