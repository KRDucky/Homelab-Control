// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package pkgcenter

import (
	"reflect"
	"strings"
	"testing"
)

const suseSearchDoc = `<?xml version='1.0'?>
<stream>
<search-result version="0.0">
<solvable status="installed" name="gcc" kind="package" edition="14.2.1+r134-1" arch="x86_64" repository="repo-oss" summary="The GNU Compiler Collection"/>
<solvable status="not-installed" name="gcc-c++" kind="package" edition="14.2.1+r134-1" arch="x86_64" repository="repo-oss" summary="C++ frontend"/>
<solvable status="installed" name="tumbleweed" kind="pattern" edition="20240101-0" arch="x86_64" repository="repo-oss" summary="A pattern, not a package"/>
</search-result>
</stream>`

func TestSuseInstalled(t *testing.T) {
	a := suseAdapter{}
	got, err := a.ParseInstalled(suseSearchDoc)
	if err != nil {
		t.Fatal(err)
	}
	want := []Package{
		{Name: "gcc", Version: "14.2.1+r134-1", Arch: "x86_64", Origin: "repo-oss", Installed: true, Summary: "The GNU Compiler Collection"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestSuseSearch(t *testing.T) {
	a := suseAdapter{}
	got, err := a.ParseSearch(suseSearchDoc)
	if err != nil {
		t.Fatal(err)
	}
	want := []Package{
		{Name: "gcc", Version: "14.2.1+r134-1", Arch: "x86_64", Origin: "repo-oss", Installed: true, Summary: "The GNU Compiler Collection"},
		{Name: "gcc-c++", Version: "14.2.1+r134-1", Arch: "x86_64", Origin: "repo-oss", Summary: "C++ frontend"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestSuseUpdates(t *testing.T) {
	a := suseAdapter{}
	got, err := a.ParseUpdates(`<?xml version='1.0'?>
<stream>
<update-list>
<update name="gcc" kind="package" edition="14.2.1+r135-1" edition-old="14.2.1+r134-1" arch="x86_64" repository="repo-update"/>
<update name="zypper" kind="package" edition="1.14.78-1" edition-old="1.14.77-1" arch="x86_64" repository="repo-update"/>
</update-list>
</stream>`)
	if err != nil {
		t.Fatal(err)
	}
	want := []Update{
		{Name: "gcc", Arch: "x86_64", Origin: "repo-update", Current: "14.2.1+r134-1", New: "14.2.1+r135-1"},
		{Name: "zypper", Arch: "x86_64", Origin: "repo-update", Current: "1.14.77-1", New: "1.14.78-1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestSuseSources(t *testing.T) {
	a := suseAdapter{}
	got, err := a.ParseSources(`<?xml version='1.0'?>
<stream>
<repo-list>
<repo alias="repo-oss" name="Main Repository (OSS)" url="https://download.opensuse.org/tumbleweed/repo/oss/" gpgcheck="1" enabled="1"/>
<repo alias="example" name="Third Party" url="https://repo.example.com/tumbleweed" gpgcheck="0" enabled="1"/>
</repo-list>
</stream>`)
	if err != nil {
		t.Fatal(err)
	}
	want := []Source{
		{Name: "repo-oss", Origin: "https://download.opensuse.org/tumbleweed/repo/oss/", Enabled: true, Signed: true, ThirdParty: false},
		{Name: "example", Origin: "https://repo.example.com/tumbleweed", Enabled: true, Signed: false, ThirdParty: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestSuseHostile(t *testing.T) {
	a := suseAdapter{}
	for _, c := range []string{
		"<stream><search-result><solvable", // truncated
		"not xml at all",                   // malformed
		"<?xml version='1.0'?><stream><search-result><solvable name=\"x\x00y\" edition=\"1\"/></search-result></stream>", // NUL name
	} {
		if _, err := a.ParseInstalled(c); err == nil {
			t.Fatalf("expected hostile XML to be refused: %q", c)
		}
	}
	// Bidi character in a structural attribute is refused.
	if _, err := a.ParseSources("<?xml version='1.0'?><stream><repo-list><repo alias=\"a\u202eb\" url=\"https://x.example.org\"/></repo-list></stream>"); err == nil {
		t.Fatal("expected bidi alias to be refused")
	}
	// Oversized XML is refused.
	if _, err := a.ParseInstalled(makeXMLOver(maxInputBytes + 1)); err != ErrTooLarge {
		t.Fatalf("oversized XML: %v", err)
	}
}

func makeXMLOver(n int) string {
	// Content is irrelevant: the size cap fires before XML parsing.
	return strings.Repeat("x", n)
}
