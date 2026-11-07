// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package pkgcenter

import (
	"strings"
	"testing"

	"homelab-control/backend/internal/hostinfo"
)

func TestForFamily(t *testing.T) {
	for _, f := range []string{FamilyDebian, FamilyFedora, FamilySUSE, FamilyArch} {
		a, ok := ForFamily(f)
		if !ok || a == nil || a.Family() != f {
			t.Fatalf("ForFamily(%q) = %v, %v; want adapter", f, a, ok)
		}
	}
	for _, f := range []string{FamilyUnknown, "", "rhel", "gentoo", "debian\n", "Debian"} {
		if a, ok := ForFamily(f); ok || a != nil {
			t.Fatalf("ForFamily(%q) = %v, %v; want no adapter", f, a, ok)
		}
	}
}

// TestOSReleaseFamilySelection drives hostinfo.ParseOSRelease +
// hostinfo.DistroFamily + ForFamily for the os-release table: Ubuntu, Debian,
// Fedora, RHEL-like, openSUSE Leap/Tumbleweed, Arch, CachyOS via ID_LIKE, and
// unknown (adapter absent).
func TestOSReleaseFamilySelection(t *testing.T) {
	cases := []struct {
		release string
		want    string
		ok      bool
	}{
		{"ID=ubuntu\nID_LIKE=debian\n", FamilyDebian, true},
		{"ID=debian\n", FamilyDebian, true},
		{"ID=fedora\n", FamilyFedora, true},
		{"ID=rhel\nID_LIKE=\"fedora\"\n", FamilyFedora, true},
		{"ID=centos\nID_LIKE=\"rhel fedora\"\n", FamilyFedora, true},
		{"ID=opensuse-leap\nID_LIKE=\"suse opensuse\"\n", FamilySUSE, true},
		{"ID=opensuse-tumbleweed\nID_LIKE=\"opensuse suse\"\n", FamilySUSE, true},
		{"ID=arch\n", FamilyArch, true},
		{"ID=cachyos\nID_LIKE=arch\n", FamilyArch, true},
		{"ID=alpine\n", FamilyUnknown, false},
		{"", FamilyUnknown, false},
	}
	for _, c := range cases {
		family := hostinfo.DistroFamily(hostinfo.ParseOSRelease([]byte(c.release)))
		a, ok := ForFamily(family)
		if family != c.want {
			t.Fatalf("DistroFamily(%q) = %q, want %q", c.release, family, c.want)
		}
		if ok != c.ok || (ok && a == nil) || (ok && a.Family() != c.want) {
			t.Fatalf("ForFamily(%q) = %v, %v; want ok=%v", family, a, ok, c.ok)
		}
	}
}

func TestSummarySanitization(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"The GNU Compiler Collection", "The GNU Compiler Collection"},
		{"  leading   and\tinternal   whitespace  ", "leading and internal whitespace"},
		{"a\x1bb", "ab"},       // ESC stripped
		{"a\x00b", "ab"},       // NUL stripped
		{"a\u202eb", "ab"},     // RLO stripped
		{"a\u2066b", "ab"},     // LRI stripped
		{"<script>", "script"}, // angle brackets stripped
		{"café ☕", "café ☕"},   // normal unicode kept
		{strings.Repeat("x", 600), strings.Repeat("x", maxSummaryRunes)}, // bounded
	}
	for _, c := range cases {
		if got := summary(c.in); got != c.want {
			t.Fatalf("summary(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCleanField(t *testing.T) {
	if _, err := cleanField("ok", 8); err != nil {
		t.Fatal(err)
	}
	if got, err := cleanField("", 8); err != nil || got != "" {
		t.Fatalf("empty field: %q, %v", got, err)
	}
	if _, err := cleanField("toolong", 3); err == nil {
		t.Fatal("expected over-long field to be refused")
	}
	for _, s := range []string{"a\x00b", "a\x1bb", "a\u202eb", "a\u2066b", "<x>", "\xff\xfe"} {
		if _, err := cleanField(s, 64); err == nil {
			t.Fatalf("expected unsafe field %q to be refused", s)
		}
	}
}

func TestLinesCaps(t *testing.T) {
	if _, err := lines(strings.Repeat("x", maxInputBytes+1)); err != ErrTooLarge {
		t.Fatalf("oversized input: %v", err)
	}
	if _, err := lines(strings.Repeat("x", maxLineBytes+1)); err == nil {
		t.Fatal("expected over-long line to be refused")
	}
	// A 10^6-char line is refused well before it can be misparsed.
	if _, err := lines(strings.Repeat("a", 1_000_000)); err == nil {
		t.Fatal("expected 10^6-char line to be refused")
	}
}
