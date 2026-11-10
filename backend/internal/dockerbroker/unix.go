// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package dockerbroker

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

type peerKey struct{}

func peerUID(c net.Conn) (uint32, error) {
	u, ok := c.(*net.UnixConn)
	if !ok {
		return 0, ErrUnavailable
	}
	raw, e := u.SyscallConn()
	if e != nil {
		return 0, ErrUnavailable
	}
	var cred *syscall.Ucred
	var callErr error
	e = raw.Control(func(fd uintptr) {
		cred, callErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	})
	if e != nil || callErr != nil || cred == nil {
		return 0, ErrUnavailable
	}
	return cred.Uid, nil
}
func connContext(ctx context.Context, c net.Conn) context.Context {
	uid, e := peerUID(c)
	if e != nil {
		return ctx
	}
	return context.WithValue(ctx, peerKey{}, uid)
}

// Listen creates only a new socket in a pre-provisioned private trusted directory.
// It never overwrites/unlinks an existing socket or creates directories/ACLs.
func Listen(path string) (*net.UnixListener, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrUnavailable
	}
	st, e := os.Lstat(filepath.Dir(path))
	if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm()&0027 != 0 {
		return nil, ErrUnavailable
	}
	owner, ok := st.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		return nil, ErrUnavailable
	}
	if _, e = os.Lstat(path); !os.IsNotExist(e) {
		return nil, ErrUnavailable
	}
	l, e := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if e != nil {
		return nil, ErrUnavailable
	}
	if os.Chmod(path, 0600) != nil {
		l.Close()
		return nil, ErrUnavailable
	}
	return l, nil
}
func verifySocket(path string, owner uint32) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return ErrUnavailable
	}
	st, e := os.Lstat(path)
	if e != nil || st.Mode()&os.ModeSocket == 0 || st.Mode().Perm()&0007 != 0 {
		return ErrUnavailable
	}
	s, ok := st.Sys().(*syscall.Stat_t)
	if !ok || s.Uid != owner {
		return ErrUnavailable
	}
	return nil
}
