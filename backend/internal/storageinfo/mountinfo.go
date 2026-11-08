// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storageinfo

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ParseMountInfo parses bounded kernel mountinfo without probing mountpoints or
// retaining source/options (which can contain network credentials).
func ParseMountInfo(r io.Reader) ([]Mount, error) {
	s := bufio.NewScanner(io.LimitReader(r, 4*1024*1024+1))
	s.Buffer(make([]byte, 4096), 65536)
	mounts := []Mount{}
	size := 0
	for s.Scan() {
		size += len(s.Bytes()) + 1
		if size > 4*1024*1024 {
			return nil, fmt.Errorf("mount inventory exceeds byte limit")
		}
		if len(mounts) >= 8192 {
			return nil, fmt.Errorf("mount inventory exceeds entry limit")
		}
		fields := strings.Fields(s.Text())
		sep := -1
		for i, f := range fields {
			if f == "-" {
				sep = i
				break
			}
		}
		if sep < 6 || len(fields) < sep+4 {
			return nil, fmt.Errorf("invalid mountinfo record")
		}
		if _, e := strconv.ParseUint(fields[0], 10, 64); e != nil {
			return nil, fmt.Errorf("invalid mount identifier")
		}
		if !validDeviceNumber(fields[2]) {
			return nil, fmt.Errorf("invalid mount device number")
		}
		root, e := unescapeMount(fields[3])
		if e != nil {
			return nil, e
		}
		point, e := unescapeMount(fields[4])
		if e != nil {
			return nil, e
		}
		if !strings.HasPrefix(root, "/") || !strings.HasPrefix(point, "/") {
			return nil, fmt.Errorf("non-absolute mount path")
		}
		mounts = append(mounts, Mount{ID: fields[0], DeviceNumber: fields[2], Root: root, Mountpoint: point, FSType: fields[sep+1], View: "service"})
	}
	if e := s.Err(); e != nil {
		return nil, fmt.Errorf("cannot read bounded mount inventory: %w", e)
	}
	return mounts, nil
}
func unescapeMount(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		if i+3 >= len(s) {
			return "", fmt.Errorf("invalid mount escape")
		}
		esc := s[i+1 : i+4]
		switch esc {
		case "040":
			b.WriteByte(' ')
		case "011":
			b.WriteByte('\t')
		case "012":
			b.WriteByte('\n')
		case "134":
			b.WriteByte('\\')
		default:
			return "", fmt.Errorf("invalid mount escape")
		}
		i += 3
	}
	return b.String(), nil
}
