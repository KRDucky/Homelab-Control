// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package pkgcenter parses distribution package-manager output into one
// normalized Software Center model (M4-1a). It is pure text parsing: no
// process is spawned, no broker operation, no route, no scope, no UI. The
// adapter for each distribution family is selected by hostinfo.DistroFamily;
// unknown families are reported, never guessed.
//
// Parsers are strict: oversized output or lines, NUL/control/bidi characters
// in structural fields, malformed or truncated lines, and localized non-C
// output are refused rather than misparsed. Free-text summaries have control/
// bidi characters stripped and are length-bounded, matching the plugin
// manifest UNSAFE_TEXT rule. Fixtures only; the exact per-release command
// flags are verified in M4-1b.
package pkgcenter

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Family values mirror hostinfo.DistroFamily. FamilyUnknown is reported,
// never guessed.
const (
	FamilyDebian  = "debian"
	FamilyFedora  = "fedora"
	FamilySUSE    = "suse"
	FamilyArch    = "arch"
	FamilyUnknown = "unknown"
)

// Bounds. A hostile input beyond any bound is refused, never truncated in a
// way that could look like a valid result.
const (
	maxInputBytes   = 4 << 20   // 4 MiB total package output
	maxLineBytes    = 256 << 10 // 256 KiB per line (10^6-char lines refused)
	maxName         = 256       // bytes
	maxVersion      = 512       // bytes; "enormous version" strings refused
	maxArch         = 64        // bytes
	maxOrigin       = 1024      // bytes
	maxSummaryRunes = 512       // characters of bounded plain-text summary
	maxPackages     = 65536
	maxSources      = 4096
)

// Sentinel errors. Malformed wraps per-line refusal details.
var (
	ErrTooLarge  = errors.New("package output exceeds the size cap")
	ErrMalformed = errors.New("malformed package output")
	ErrTooMany   = errors.New("too many entries")
)

// Package is one normalized software-center row.
type Package struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	Arch       string `json:"arch,omitempty"`
	Origin     string `json:"origin,omitempty"`
	Installed  bool   `json:"installed"`
	Upgradable bool   `json:"upgradable"`
	Summary    string `json:"summary,omitempty"`
}

// Update is one pending upgrade: the current installed version to the new
// available version. Current is empty when the source tool does not report
// the installed version in the same output (e.g. dnf check-update).
type Update struct {
	Name    string `json:"name"`
	Arch    string `json:"arch,omitempty"`
	Origin  string `json:"origin,omitempty"`
	Current string `json:"current"`
	New     string `json:"new"`
}

// Source is one configured repository or package source.
type Source struct {
	Name       string `json:"name"`
	Origin     string `json:"origin"`
	Enabled    bool   `json:"enabled"`
	Signed     bool   `json:"signed"`
	ThirdParty bool   `json:"third_party"`
}

// Adapter parses one distribution family's package-manager output. Parsers
// are pure: text in, typed structs out, no I/O.
type Adapter interface {
	Family() string
	ParseInstalled(string) ([]Package, error)
	ParseSearch(string) ([]Package, error)
	ParseUpdates(string) ([]Update, error)
	ParseSources(string) ([]Source, error)
}

// ForFamily returns the adapter for a hostinfo.DistroFamily value. Unknown
// families have no adapter (ok == false) and are reported, never guessed.
func ForFamily(family string) (Adapter, bool) {
	switch family {
	case FamilyDebian:
		return debianAdapter{}, true
	case FamilyFedora:
		return fedoraAdapter{}, true
	case FamilySUSE:
		return suseAdapter{}, true
	case FamilyArch:
		return archAdapter{}, true
	default:
		return nil, false
	}
}

// isUnsafeTextRune reports whether r is in the plugin manifest UNSAFE_TEXT
// set: '<', '>', C0 controls, DEL, and bidi embedding/override/isolate marks.
func isUnsafeTextRune(r rune) bool {
	switch {
	case r == '<' || r == '>':
		return true
	case r < 0x20 || r == 0x7f:
		return true
	case r >= 0x202a && r <= 0x202e:
		return true
	case r >= 0x2066 && r <= 0x2069:
		return true
	}
	return false
}

// cleanField validates one structural field: valid UTF-8, within max bytes,
// free of unsafe text runes and free of whitespace (structural fields are
// never prose). It refuses rather than strips, so hostile or localized output
// cannot be misparsed. An empty field is returned as "".
func cleanField(s string, max int) (string, error) {
	if s == "" {
		return "", nil
	}
	if !utf8.ValidString(s) {
		return "", errors.New("invalid UTF-8")
	}
	if len(s) > max {
		return "", fmt.Errorf("field exceeds %d bytes", max)
	}
	for _, r := range s {
		if isUnsafeTextRune(r) || unicode.IsSpace(r) {
			return "", errors.New("control, bidi or whitespace character")
		}
	}
	return s, nil
}

// packageNameRE is the per-family package/repo name grammar (ADR 0008 R4): a
// leading alphanumeric (never "-") and a bounded charset with no whitespace.
var packageNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9+._:~@-]*$`)

// cleanPackageName validates a package or repository name as a name, not
// arbitrary text: structural checks plus the closed charset. Empty is refused.
func cleanPackageName(s string) (string, error) {
	if s == "" {
		return "", errEmpty("name")
	}
	v, err := cleanField(s, maxName)
	if err != nil {
		return "", err
	}
	if !packageNameRE.MatchString(v) {
		return "", errors.New("invalid package name")
	}
	return v, nil
}

// hostnameOf extracts the lowercased hostname of a URI (scheme, userinfo, port
// and path are stripped). It is not a URL parser; input is already bounded.
func hostnameOf(uri string) string {
	s := uri
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, ":"); i >= 0 && strings.LastIndex(s, "]") < i {
		s = s[:i]
	}
	return strings.ToLower(strings.TrimSuffix(s, "."))
}

// vendorHost reports whether hostname is a vendor host: exact match or a
// dot-boundary suffix, so `archive.ubuntu.com.evil.example` is not vendor.
func vendorHost(hostname string, hosts []string) bool {
	for _, h := range hosts {
		if hostname == h || strings.HasSuffix(hostname, "."+h) {
			return true
		}
	}
	return false
}

// truthy recognises the boolean spellings apt/dnf/zypper/pacman accept.
func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// summary strips control/bidi characters from free text, normalizes runs of
// whitespace to a single space, and bounds it to maxSummaryRunes characters.
func summary(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\v' || r == '\f':
			b.WriteRune(' ')
		case isUnsafeTextRune(r):
			continue
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if utf8.RuneCountInString(out) <= maxSummaryRunes {
		return out
	}
	runes := []rune(out)
	return string(runes[:maxSummaryRunes])
}

// lines splits text into lines, enforcing the total and per-line caps. A
// trailing newline is dropped; interior empty lines are preserved and skipped
// by each parser.
func lines(text string) ([]string, error) {
	if len(text) > maxInputBytes {
		return nil, ErrTooLarge
	}
	raw := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	out := make([]string, 0, len(raw))
	for _, ln := range raw {
		ln = strings.TrimSuffix(ln, "\r")
		if len(ln) > maxLineBytes {
			return nil, fmt.Errorf("line %d: %w (exceeds %d bytes)", len(out)+1, ErrMalformed, maxLineBytes)
		}
		out = append(out, ln)
	}
	return out, nil
}

// malformed builds a line-numbered ErrMalformed error.
func malformed(line int, msg string) error {
	return fmt.Errorf("line %d: %w: %s", line, ErrMalformed, msg)
}

// errEmpty builds a "missing required field" error.
func errEmpty(field string) error {
	return fmt.Errorf("%s is empty", field)
}

// malformedf builds an ErrMalformed error without a line number.
func malformedf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrMalformed, fmt.Sprintf(format, args...))
}
