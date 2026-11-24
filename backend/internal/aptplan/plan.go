// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package aptplan parses APT's simulated action records into a bounded,
// display-only preview. It never executes APT and is not a trust decision or
// authorization to apply a transaction.
package aptplan

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	maxOutput  = 2 << 20
	maxLines   = 20000
	maxActions = 4096
)

var (
	ErrUnsafe = errors.New("APT simulation cannot be safely previewed")
	pkgNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]{0,127}(?::[a-z0-9][a-z0-9-]{0,31})?$`)
)

type Action string

const (
	Install   Action = "install"
	Remove    Action = "remove"
	Configure Action = "configure"
)

// Change is an action reported verbatim by APT's simulation. Version and
// detail are advisory display strings, not independently trusted identities.
type Change struct {
	Action           Action `json:"action"`
	Name             string `json:"name"`
	InstalledVersion string `json:"installed_version,omitempty"`
	CandidateVersion string `json:"candidate_version,omitempty"`
}

// Preview is only a parsed record of APT's output; it must never be used to
// authorize execution without a fresh broker revalidation and source check.
type Preview struct {
	Changes        []Change `json:"changes"`
	InstallCount   int      `json:"install_count"`
	RemoveCount    int      `json:"remove_count"`
	ConfigureCount int      `json:"configure_count"`
}

// ParseSimulation accepts stdout from apt-get -s for a caller-selected action.
// It requires an APT transaction summary and fails closed on unrecognized
// action records, APT warnings/errors or malformed package and version tokens.
// The simulation can change before execution: this parser is NOT a lock.
func ParseSimulation(stdout string) (Preview, error) {
	var p Preview
	if len(stdout) == 0 || len(stdout) > maxOutput || strings.ContainsRune(stdout, '\x00') {
		return p, ErrUnsafe
	}
	lines := strings.Split(strings.ReplaceAll(stdout, "\r\n", "\n"), "\n")
	if len(lines) > maxLines {
		return p, ErrUnsafe
	}
	sawSummary := false
	expectedInst, expectedRemove := 0, 0
	seen := make(map[string]bool)
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "E:") || strings.HasPrefix(line, "W:") || strings.HasPrefix(line, "N:") {
			return Preview{}, fmt.Errorf("%w: apt reported a diagnostic", ErrUnsafe)
		}
		if strings.HasPrefix(line, "Inst ") || strings.HasPrefix(line, "Remv ") || strings.HasPrefix(line, "Conf ") {
			if !sawSummary {
				return Preview{}, fmt.Errorf("%w: action before summary", ErrUnsafe)
			}
			if len(p.Changes) >= maxActions {
				return Preview{}, ErrUnsafe
			}
			c, err := parseAction(line)
			if err != nil {
				return Preview{}, err
			}
			key := string(c.Action) + "\x00" + c.Name
			if seen[key] {
				return Preview{}, fmt.Errorf("%w: duplicate action", ErrUnsafe)
			}
			seen[key] = true
			p.Changes = append(p.Changes, c)
			switch c.Action {
			case Install:
				p.InstallCount++
			case Remove:
				p.RemoveCount++
			case Configure:
				p.ConfigureCount++
			}
			continue
		}
		// apt-get -s prints the summary before the Inst/Remv/Conf records.
		// It may also print progress text, dependency explanations and headings.
		if m := regexpSummary.FindStringSubmatch(line); m != nil {
			if sawSummary {
				return Preview{}, fmt.Errorf("%w: repeated APT summary", ErrUnsafe)
			}
			upgraded, _ := strconv.Atoi(m[1])
			added, _ := strconv.Atoi(m[2])
			removed, _ := strconv.Atoi(m[3])
			expectedInst, expectedRemove = upgraded+added, removed
			if expectedInst > maxActions || expectedRemove > maxActions {
				return Preview{}, ErrUnsafe
			}
			sawSummary = true
			continue
		}
		if strings.HasPrefix(line, "Broken ") || strings.HasPrefix(line, "Purg ") || strings.HasPrefix(line, "Del ") {
			return Preview{}, fmt.Errorf("%w: unsupported APT action", ErrUnsafe)
		}
	}
	if !sawSummary || p.InstallCount != expectedInst || p.RemoveCount != expectedRemove {
		return Preview{}, fmt.Errorf("%w: missing or inconsistent APT summary", ErrUnsafe)
	}
	return p, nil
}

var regexpSummary = regexp.MustCompile(`^([0-9]+) upgraded, ([0-9]+) newly installed, ([0-9]+) to remove and ([0-9]+) not upgraded\.$`)
var verRE = regexp.MustCompile(`^[^\s()\[\]]{1,256}$`)

func parseAction(line string) (Change, error) {
	var c Change
	switch {
	case strings.HasPrefix(line, "Inst "):
		c.Action = Install
		line = strings.TrimPrefix(line, "Inst ")
	case strings.HasPrefix(line, "Remv "):
		c.Action = Remove
		line = strings.TrimPrefix(line, "Remv ")
	case strings.HasPrefix(line, "Conf "):
		c.Action = Configure
		line = strings.TrimPrefix(line, "Conf ")
	default:
		return c, ErrUnsafe
	}
	fields := strings.Fields(line)
	if len(fields) < 1 || !pkgNameRE.MatchString(fields[0]) {
		return c, fmt.Errorf("%w: invalid package name", ErrUnsafe)
	}
	c.Name = fields[0]
	rest := strings.TrimSpace(strings.TrimPrefix(line, c.Name))
	if strings.HasPrefix(rest, "[") {
		end := strings.Index(rest, "]")
		if end < 2 || !verRE.MatchString(rest[1:end]) {
			return c, ErrUnsafe
		}
		c.InstalledVersion = rest[1:end]
		rest = strings.TrimSpace(rest[end+1:])
	}
	if strings.HasPrefix(rest, "(") {
		end := strings.Index(rest, ")")
		if end < 2 {
			return c, ErrUnsafe
		}
		// Version is the first token inside (...); remaining text is APT's
		// origin/architecture description. It isn't trusted source evidence.
		parts := strings.Fields(rest[1:end])
		if len(parts) == 0 || !verRE.MatchString(parts[0]) {
			return c, ErrUnsafe
		}
		c.CandidateVersion = parts[0]
		rest = strings.TrimSpace(rest[end+1:])
	}
	// APT can print [oldversion] after candidate/origin rather than before.
	if strings.HasPrefix(rest, "[") && c.InstalledVersion == "" {
		end := strings.Index(rest, "]")
		if end < 2 || !verRE.MatchString(rest[1:end]) {
			return c, ErrUnsafe
		}
		c.InstalledVersion = rest[1:end]
		rest = strings.TrimSpace(rest[end+1:])
	}
	if rest != "" {
		return c, fmt.Errorf("%w: unexpected action suffix", ErrUnsafe)
	}
	if c.Action == Install && c.CandidateVersion == "" {
		return c, fmt.Errorf("%w: missing install candidate", ErrUnsafe)
	}
	if c.Action == Configure && c.CandidateVersion == "" {
		return c, fmt.Errorf("%w: missing configuration version", ErrUnsafe)
	}
	return c, nil
}
