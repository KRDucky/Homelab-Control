// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package pkgcenter

import (
	"reflect"
	"strings"
	"testing"
)

func TestDebianInstalled(t *testing.T) {
	a := debianAdapter{}
	got, err := a.ParseInstalled("adduser\t3.137ubuntu1\tall\napt\t2.7.14build2\tamd64\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []Package{
		{Name: "adduser", Version: "3.137ubuntu1", Arch: "all", Installed: true},
		{Name: "apt", Version: "2.7.14build2", Arch: "amd64", Installed: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestDebianInstalledHostile(t *testing.T) {
	a := debianAdapter{}
	if got, err := a.ParseInstalled(""); err != nil || len(got) != 0 {
		t.Fatalf("empty installed output: %v, %v", got, err)
	}
	cases := []string{
		"apt\t2.7.14build2\n",                                     // truncated (missing arch)
		"apt\t2.7.14build2\tamd64\textra\n",                       // too many fields
		"apt\t\x002.7\tamd64\n",                                   // NUL in version
		"apt\t2.7\u202e14\tamd64\n",                               // bidi in version
		"apt\t" + strings.Repeat("v", maxVersion+1) + "\tamd64\n", // enormous version
		"apt\t2.7\t<amd64>\n",                                     // angle brackets in arch
	}
	for _, c := range cases {
		if _, err := a.ParseInstalled(c); err == nil {
			t.Fatalf("expected hostile installed input to be refused: %q", c)
		}
	}
}

func TestDebianInstalledKeepsMultipleVersions(t *testing.T) {
	a := debianAdapter{}
	got, err := a.ParseInstalled("apt\t2.7\tamd64\napt\t2.7\tamd64\napt\t2.8\tamd64\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Version != "2.7" || got[1].Version != "2.8" {
		t.Fatalf("multi-version rows lost: %+v", got)
	}
}

func TestDebianSearch(t *testing.T) {
	a := debianAdapter{}
	got, err := a.ParseSearch("golang-go - Go programming language compiler\nfoo - one - with - dashes\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []Package{
		{Name: "golang-go", Summary: "Go programming language compiler"},
		{Name: "foo", Summary: "one - with - dashes"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestDebianSearchHostile(t *testing.T) {
	a := debianAdapter{}
	for _, c := range []string{
		"no-separator\n",
		" - leading separator\n",
		"bad\x00name - summary\n", // NUL in name
	} {
		if _, err := a.ParseSearch(c); err == nil {
			t.Fatalf("expected hostile search input to be refused: %q", c)
		}
	}
	// An over-long summary is bounded, not refused.
	got, err := a.ParseSearch("golang-go - " + strings.Repeat("x", 6000) + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got[0].Summary) != maxSummaryRunes {
		t.Fatalf("summary not bounded: %d runes", len(got[0].Summary))
	}
}

func TestDebianUpdates(t *testing.T) {
	a := debianAdapter{}
	got, err := a.ParseUpdates("Listing... Done\n" +
		"golang-1.22-go/noble-updates 1.22.5-1ubuntu1 amd64 [upgradable from: 1.22.2-1ubuntu1]\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []Update{{Name: "golang-1.22-go", Arch: "amd64", Origin: "noble-updates", Current: "1.22.2-1ubuntu1", New: "1.22.5-1ubuntu1"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestDebianUpdatesHostile(t *testing.T) {
	a := debianAdapter{}
	// A localized header is refused rather than misparsed.
	if _, err := a.ParseUpdates("Auflistung... Fertig\n"); err == nil {
		t.Fatal("expected localized header to be refused")
	}
	for _, c := range []string{
		"golang/updates 1.2 amd64\n",                              // missing upgradable-from
		"golang/updates 1.2\n",                                    // missing arch
		"golang/updates \x001.2 amd64 [upgradable from: 1.0]\n",   // NUL version
		"golang/updates 1.2 amd64 [upgradable from: \u202e1.0]\n", // bidi current
	} {
		if _, err := a.ParseUpdates(c); err == nil {
			t.Fatalf("expected hostile updates input to be refused: %q", c)
		}
	}
}

func TestDebianSources(t *testing.T) {
	a := debianAdapter{}
	got, err := a.ParseSources("# deb https://archive.ubuntu.com/ubuntu noble main\n" +
		"deb https://archive.ubuntu.com/ubuntu noble main restricted\n" +
		"deb [trusted=yes] https://example.com/ppa noble main\n" +
		"deb-src https://archive.ubuntu.com/ubuntu noble universe\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []Source{
		{Name: "noble", Origin: "https://archive.ubuntu.com/ubuntu", Enabled: true, Signed: true, ThirdParty: false},
		{Name: "noble", Origin: "https://example.com/ppa", Enabled: true, Signed: false, ThirdParty: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestDebianSourcesMultiTokenOptions(t *testing.T) {
	a := debianAdapter{}
	got, err := a.ParseSources("deb [ arch=amd64 signed-by=/usr/share/keyrings/ubuntu-archive-keyring.gpg ] https://archive.ubuntu.com/ubuntu noble main\n")
	if err != nil {
		t.Fatal(err)
	}
	want := Source{Name: "noble", Origin: "https://archive.ubuntu.com/ubuntu", Enabled: true, Signed: true, ThirdParty: false}
	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestDebianSourcesHostile(t *testing.T) {
	a := debianAdapter{}
	for _, c := range []string{
		"deb https://archive.ubuntu.com/ubuntu\n",                // missing suite
		"rpm https://x.example.org/repo suite\n",                 // not a deb stanza
		"deb https://archive.ubuntu.com/\x00ubuntu noble main\n", // NUL URI
	} {
		if _, err := a.ParseSources(c); err == nil {
			t.Fatalf("expected hostile sources input to be refused: %q", c)
		}
	}
}
