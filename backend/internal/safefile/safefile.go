// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package safefile reads and replaces small configuration files for the root
// helpers without following symlinks. Replacement writes a temporary file in
// the same directory, syncs it and renames it over the target, so a reader
// sees the old or the new content. It is not a transaction across files.
package safefile

import (
	"errors"
	"io"
	"os"
	"syscall"
)

// ErrNotRegular is returned for a symlink, directory or device.
var ErrNotRegular = errors.New("not a regular file")

// ErrTooLarge is returned when a file or data exceeds the caller's limit.
var ErrTooLarge = errors.New("file too large")

// Read returns the content of the regular file at path, refusing symlinks and
// anything larger than max bytes.
func Read(path string, max int64) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, ErrNotRegular
	}
	if fi.Size() > max {
		return nil, ErrTooLarge
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, ErrTooLarge
	}
	return b, nil
}

// Write replaces path with data, with exactly mode, through a temporary
// file in the same directory and a rename. The temporary file is never
// followed through a symlink.
func Write(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".hc-tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	// The open mode is filtered by the umask (the helpers run with 0077);
	// set it exactly so a world-readable config stays world-readable.
	if err := f.Chmod(mode); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
