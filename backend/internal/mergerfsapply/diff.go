// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsapply

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
)

// Block reasons. They are closed tokens, not snapraid prose.
const (
	BlockMissingDisk    = "missing-disk"
	BlockRemovedFiles   = "removed-files"
	BlockDiffUnreadable = "diff-unreadable"
	// BlockLocked: another SnapRAID command holds the array lock, for
	// example a sync that is still running.
	BlockLocked = "locked"
)

var (
	removedLead = regexp.MustCompile(`(?i)(?:^|\n)[ \t]*([0-9]+)[ \t]+removed\b`)
	removedTail = regexp.MustCompile(`(?i)(?:^|\n)[ \t]*removed[ \t]+([0-9]+)\b`)
)

// BlockFromDiff refuses sync and scrub when a disk is missing or files were
// removed. There is no override. Fix does not call this: recovering a missing
// file is what snapraid fix -m is for.
//
// A first sync has no content file yet. SnapRAID says that file was "not
// found" and then assumes an empty array. That sentence is not a missing
// data disk, so it is not a block. A missing data disk is "not present",
// "not mounted", or "error opening directory" (the mountpoint is gone).
func BlockFromDiff(text string) string {
	low := strings.ToLower(text)
	if strings.Contains(low, "is already in use") {
		return BlockLocked
	}
	if strings.Contains(low, "not present") || strings.Contains(low, "missing disk") || strings.Contains(low, "not mounted") || strings.Contains(low, "error opening directory") {
		return BlockMissingDisk
	}
	if strings.Contains(low, "all the files") && strings.Contains(low, "removed") {
		return BlockRemovedFiles
	}
	if removedCount(removedLead, text) > 0 || removedCount(removedTail, text) > 0 {
		return BlockRemovedFiles
	}
	return ""
}

// DiffGate is the sync and scrub pre-check. snapraid diff exits 0 when
// nothing changed and 2 when files were added or removed. Any other status
// means the array was not read, so sync and scrub do not start.
func DiffGate(code int, text string) string {
	if code < 0 {
		return BlockDiffUnreadable
	}
	if reason := BlockFromDiff(text); reason != "" {
		return reason
	}
	if code != 0 && code != 2 {
		return BlockDiffUnreadable
	}
	return ""
}

func removedCount(re *regexp.Regexp, text string) int {
	n := 0
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		v, err := strconv.Atoi(m[1])
		if err == nil && v > n {
			n = v
		}
	}
	return n
}

// HeadTail keeps the first and the last Keep bytes written to it. snapraid
// diff prints its disk checks first and its summary last; the lines between
// are one per changed file, which the gate does not read.
type HeadTail struct {
	Keep       int
	head, tail []byte
	cut        bool
}

func (h *HeadTail) Write(p []byte) (int, error) {
	n := len(p)
	if room := h.Keep - len(h.head); room > 0 {
		k := min(room, len(p))
		h.head = append(h.head, p[:k]...)
		p = p[k:]
	}
	if len(p) == 0 {
		return n, nil
	}
	h.tail = append(h.tail, p...)
	if over := len(h.tail) - h.Keep; over > 0 {
		h.cut = true
		copy(h.tail, h.tail[over:])
		h.tail = h.tail[:h.Keep]
	}
	return n, nil
}

// String is the head and the tail. When bytes between them were dropped, the
// tail starts at its first whole line.
func (h *HeadTail) String() string {
	tail := h.tail
	if h.cut {
		if i := bytes.IndexByte(tail, '\n'); i >= 0 {
			tail = tail[i+1:]
		}
		return string(h.head) + "\n" + string(tail)
	}
	return string(h.head) + string(tail)
}
