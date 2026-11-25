// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package mountinfo is the one bounded parser for the kernel's
// /proc/<pid>/mountinfo (proc(5)). The storage modules used to carry seven
// copies of it with drifting rules (Graphify review, 2026-11-25); each module
// now filters these records and applies only its own field checks.
//
// Parse never opens a mountpoint or a source. A line that is too long or
// malformed is skipped and counted, never guessed at.
package mountinfo

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Bounds on what Parse reads. A real host stays far below them.
const (
	MaxBytes   = 4 << 20
	MaxRecords = 8192
	MaxLine    = 64 << 10
)

// Record is one mountinfo line (proc(5) field names). Mountpoint, Root and
// Source are unescaped.
type Record struct {
	ID           uint64
	ParentID     uint64
	DeviceNumber string // major:minor
	Root         string
	Mountpoint   string
	MountOptions string
	FSType       string
	Source       string
	SuperOptions string
}

// Info describes what Parse could not represent.
type Info struct {
	// Truncated: the byte or record bound was reached, or lines were skipped.
	Truncated bool
	// Skipped counts lines that were too long or malformed.
	Skipped int
}

// Parse reads bounded mountinfo text. It fails only when the reader fails.
func Parse(r io.Reader) ([]Record, Info, error) {
	br := bufio.NewReader(io.LimitReader(r, MaxBytes+1))
	var out []Record
	var info Info
	size := 0
	for {
		line, n, tooLong, err := nextLine(br)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, info, fmt.Errorf("cannot read bounded mount inventory: %w", err)
		}
		size += n
		if size > MaxBytes || len(out)+info.Skipped >= MaxRecords {
			info.Truncated = true
			break
		}
		if tooLong {
			info.Skipped++
			info.Truncated = true
			continue
		}
		rec, ok := parseLine(line)
		if !ok {
			info.Skipped++
			info.Truncated = true
			continue
		}
		out = append(out, rec)
	}
	return out, info, nil
}

// ReadFile parses a mountinfo file, normally <root>/proc/self/mountinfo.
func ReadFile(path string) ([]Record, Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, Info{}, err
	}
	defer f.Close()
	return Parse(f)
}

// parseLine splits one record:
// ID PARENT MAJ:MIN ROOT MOUNTPOINT OPTIONS [OPTIONAL...] - FSTYPE SOURCE SUPER-OPTIONS
func parseLine(line string) (Record, bool) {
	fields := strings.Fields(line)
	sep := -1
	for i, f := range fields {
		if f == "-" {
			sep = i
			break
		}
	}
	if sep < 6 || len(fields) < sep+4 {
		return Record{}, false
	}
	id, err1 := strconv.ParseUint(fields[0], 10, 64)
	parent, err2 := strconv.ParseUint(fields[1], 10, 64)
	root, err3 := Unescape(fields[3])
	point, err4 := Unescape(fields[4])
	source, err5 := Unescape(fields[sep+2])
	if errors.Join(err1, err2, err3, err4, err5) != nil || !strings.HasPrefix(root, "/") || !strings.HasPrefix(point, "/") {
		return Record{}, false
	}
	return Record{
		ID: id, ParentID: parent, DeviceNumber: fields[2], Root: root, Mountpoint: point,
		MountOptions: fields[5], FSType: fields[sep+1], Source: source, SuperOptions: fields[sep+3],
	}, true
}

// nextLine reads one line; n includes the newline. A line over MaxLine is
// reported as tooLong and the rest of it is discarded.
func nextLine(r *bufio.Reader) (line string, n int, tooLong bool, err error) {
	var b strings.Builder
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			if err == io.EOF && (b.Len() > 0 || n > 0) {
				return b.String(), n, tooLong, nil
			}
			return "", n, tooLong, err
		}
		n += len(chunk)
		if !tooLong {
			if b.Len()+len(chunk) > MaxLine {
				tooLong = true
				b.Reset()
			} else {
				b.Write(chunk)
			}
		}
		if !isPrefix {
			n++ // newline
			if tooLong {
				return "", n, true, nil
			}
			return b.String(), n, false, nil
		}
	}
}

// Unescape decodes the octal escapes the kernel writes in mountinfo paths
// (\040 space, \011 tab, \012 newline, \134 backslash). Anything else fails.
func Unescape(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		if i+4 > len(s) {
			return "", fmt.Errorf("invalid mount escape")
		}
		switch s[i+1 : i+4] {
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

// HasDotDot reports a ".." path component.
func HasDotDot(path string) bool {
	for _, c := range strings.Split(path, "/") {
		if c == ".." {
			return true
		}
	}
	return false
}

// DeviceMaxBytes bounds a device path kept from a mount source;
// MountpointMaxBytes matches the OpenAPI maxLength of a listed mountpoint.
const (
	DeviceMaxBytes     = 256
	MountpointMaxBytes = 256
)

// ValidDevice accepts a /dev/ path without "..", "//" or excess length.
func ValidDevice(name string) bool {
	return len(name) >= len("/dev/x") && len(name) <= DeviceMaxBytes && strings.HasPrefix(name, "/dev/") &&
		!HasDotDot(name) && !strings.Contains(name, "//")
}

// DeviceMount is a mount of a block-device filesystem (ext4, XFS, f2fs).
type DeviceMount struct {
	Mountpoint string `json:"mountpoint"`
	Device     string `json:"device"`
}

// DeviceMounts returns the mounts of one filesystem type whose source is a
// safe device path, at most max of them. Truncated is set when records were
// skipped by Parse, a mount of this type was dropped for an unsafe source or
// mountpoint, or max was reached.
func DeviceMounts(recs []Record, info Info, fstype string, max int) ([]DeviceMount, bool) {
	out := []DeviceMount{}
	truncated := info.Truncated
	for _, r := range recs {
		if r.FSType != fstype {
			continue
		}
		if !ValidDevice(r.Source) || HasDotDot(r.Mountpoint) || len(r.Mountpoint) > MountpointMaxBytes {
			truncated = true
			continue
		}
		if len(out) >= max {
			truncated = true
			continue
		}
		out = append(out, DeviceMount{Mountpoint: r.Mountpoint, Device: r.Source})
	}
	return out, truncated
}
