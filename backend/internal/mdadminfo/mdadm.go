// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package mdadminfo is the unprivileged, read-only Linux software RAID (md)
// inventory. It Stats the well-known mdadm path and reads bounded
// /proc/mdstat. It never executes mdadm, never creates, assembles, adds,
// removes, grows or reshapes an array, never parses a superblock, never loads
// a kernel module, and never opens a component or array device node.
package mdadminfo

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"regexp"
	"strings"
)

// Array is one md array line of /proc/mdstat, as the kernel printed it.
// Level is empty when the line prints no personality (for example an
// inactive array). MemberCount is the number of component entries listed on
// the array line and is null when the line lists none. Component names,
// sizes, sync progress and the [n/m] status are not returned.
type Array struct {
	Name        string `json:"name"`
	Level       string `json:"level"`
	State       string `json:"state"`
	MemberCount *int   `json:"memberCount"`
}

// Inventory is the /proc/mdstat half of the response. Personalities is the
// "Personalities :" line of the same file (loaded kernel RAID personalities).
type Inventory struct {
	Present         bool     `json:"present"`
	Reason          string   `json:"reason"`
	Personalities   []string `json:"personalities"`
	Arrays          []Array  `json:"arrays"`
	ArraysTruncated bool     `json:"arraysTruncated"`
}

// Snapshot is the full read-only md inventory.
type Snapshot struct {
	Tooling Tooling   `json:"tooling"`
	MD      Inventory `json:"md"`
}

const (
	mdstatMaxBytes  = 1 << 20
	mdstatMaxLine   = 64 << 10
	maxArrays       = 256
	maxPersonality  = 64
	reasonMissing   = "The kernel md driver has not created /proc/mdstat (this page does not load modules)."
	reasonUnread    = "/proc/mdstat could not be read safely; no arrays are shown."
	reasonTruncated = "too many arrays to list"
)

var (
	errBadMdstat     = errors.New("invalid /proc/mdstat")
	arrayName        = regexp.MustCompile(`^md[A-Za-z0-9_]{1,30}$`)
	levelToken       = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	personalityToken = regexp.MustCompile(`^\[[a-z][a-z0-9_-]{0,31}\]$`)
	// A component entry is "<kernel name>[<role>]" plus optional flags such
	// as (F), (S), (W), (J) or (R). Only the count is kept.
	componentToken = regexp.MustCompile(`^[A-Za-z0-9_.:!+-]{1,64}\[[0-9]{1,6}\](\([A-Z]\))*$`)
)

// ParseMdstat parses bounded /proc/mdstat text. Any array line it cannot
// read exactly, an oversized file or line, or a non-printable byte fails the
// whole parse: the caller shows no arrays rather than a partial guess.
// Status lines (blocks, [n/m], bitmap, resync) and "unused devices" are
// ignored. A missing trailing field on an array line never panics.
func ParseMdstat(text string) (personalities []string, arrays []Array, truncated bool, err error) {
	if len(text) > mdstatMaxBytes {
		return nil, nil, false, errBadMdstat
	}
	personalities, arrays = []string{}, []Array{}
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		if len(line) > mdstatMaxLine || !printable(line) {
			return nil, nil, false, errBadMdstat
		}
		if rest, ok := strings.CutPrefix(line, "Personalities :"); ok {
			for _, tok := range strings.Fields(rest) {
				if !personalityToken.MatchString(tok) || len(personalities) >= maxPersonality {
					return nil, nil, false, errBadMdstat
				}
				personalities = append(personalities, strings.Trim(tok, "[]"))
			}
			continue
		}
		if line == "" || line[0] == ' ' || line[0] == '\t' || strings.HasPrefix(line, "unused devices:") {
			continue
		}
		name, rest, ok := strings.Cut(line, " :")
		if !ok || !arrayName.MatchString(name) || seen[name] {
			return nil, nil, false, errBadMdstat
		}
		seen[name] = true
		a, ok := parseArray(name, rest)
		if !ok {
			return nil, nil, false, errBadMdstat
		}
		if len(arrays) >= maxArrays {
			truncated = true
			continue
		}
		arrays = append(arrays, a)
	}
	return personalities, arrays, truncated, nil
}

// parseArray reads "<state> [(read-only)|(auto-read-only)] [<level>]
// [components...]". Every index is bounds-checked: "md0 :" alone is a
// malformed line, "md0 : active" is an array with no level and no count.
func parseArray(name, rest string) (Array, bool) {
	f := strings.Fields(rest)
	if len(f) == 0 || (f[0] != "active" && f[0] != "inactive") {
		return Array{}, false
	}
	a := Array{Name: name, State: f[0]}
	i := 1
	for i < len(f) && strings.HasPrefix(f[i], "(") {
		if f[i] != "(read-only)" && f[i] != "(auto-read-only)" {
			return Array{}, false
		}
		a.State += " " + f[i]
		i++
	}
	if i < len(f) && !strings.Contains(f[i], "[") {
		if !levelToken.MatchString(f[i]) {
			return Array{}, false
		}
		a.Level = f[i]
		i++
	}
	n := 0
	for ; i < len(f); i++ {
		if !componentToken.MatchString(f[i]) {
			return Array{}, false
		}
		n++
	}
	if n > 0 {
		a.MemberCount = &n
	}
	return a, true
}

func printable(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < 0x20 && c != '\t') || c > 0x7e {
			return false
		}
	}
	return true
}

// readMdstat returns the bounded file, or the fixed reason it is unusable.
func readMdstat(osRoot string) (string, string) {
	if osRoot == "" {
		osRoot = "/"
	}
	root, err := os.OpenRoot(osRoot)
	if err != nil {
		return "", reasonUnread
	}
	defer root.Close()
	f, err := root.Open("proc/mdstat")
	if errors.Is(err, fs.ErrNotExist) {
		return "", reasonMissing
	}
	if err != nil {
		return "", reasonUnread
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, mdstatMaxBytes+1))
	if err != nil || len(b) > mdstatMaxBytes {
		return "", reasonUnread
	}
	return string(b), ""
}

// ReadMdstatFile parses the mdstat file at path, opened directly (the
// storage helper reads /proc/mdstat this way; its AppArmor profile does not
// let it open the root directory). Same bounds and fail-closed result as
// ProbeMdstat.
func ReadMdstatFile(path string) Inventory {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return parsedInventory("", reasonMissing)
	}
	if err != nil {
		return parsedInventory("", reasonUnread)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, mdstatMaxBytes+1))
	if err != nil || len(b) > mdstatMaxBytes {
		return parsedInventory("", reasonUnread)
	}
	return parsedInventory(string(b), "")
}

// ProbeMdstat reads and parses /proc/mdstat under osRoot (tests pass a
// fixture tree). It fails closed: missing, unreadable, oversized or
// malformed input is present=false with fixed text and no arrays.
func ProbeMdstat(osRoot string) Inventory {
	return parsedInventory(readMdstat(osRoot))
}

// parsedInventory turns mdstat text (or the reason it is unusable) into an
// Inventory.
func parsedInventory(text, reason string) Inventory {
	if reason != "" {
		return Inventory{Present: false, Reason: reason, Personalities: []string{}, Arrays: []Array{}}
	}
	pers, arrays, truncated, err := ParseMdstat(text)
	if err != nil {
		return Inventory{Present: false, Reason: reasonUnread, Personalities: []string{}, Arrays: []Array{}}
	}
	inv := Inventory{Present: true, Personalities: pers, Arrays: arrays, ArraysTruncated: truncated}
	if truncated {
		inv.Reason = reasonTruncated
	}
	return inv
}

// Collect probes the mdadm binary (Stat only) and reads /proc/mdstat. Only
// context cancellation returns an error.
func Collect(ctx context.Context, osRoot, family string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Tooling: ProbeTooling(family), MD: ProbeMdstat(osRoot)}, nil
}
