// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package pkgcenter

import (
	"reflect"
	"testing"
)

func TestFedoraInstalled(t *testing.T) {
	a := fedoraAdapter{}
	got, err := a.ParseInstalled("bash\t5.2.26-1.fc40\tx86_64\ngcc\t14.2.1-1.fc40\tx86_64\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []Package{
		{Name: "bash", Version: "5.2.26-1.fc40", Arch: "x86_64", Installed: true},
		{Name: "gcc", Version: "14.2.1-1.fc40", Arch: "x86_64", Installed: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestFedoraInstalledHostile(t *testing.T) {
	a := fedoraAdapter{}
	for _, c := range []string{
		"bash\t5.2.26-1.fc40\n",                // truncated
		"bash\t5.2.26-1.fc40\tx86_64\textra\n", // too many fields
		"bash\t\x005.2.26\tx86_64\n",           // NUL
		"bash\t5.2\u202e26\tx86_64\n",          // bidi
	} {
		if _, err := a.ParseInstalled(c); err == nil {
			t.Fatalf("expected hostile installed input to be refused: %q", c)
		}
	}
}

func TestFedoraSearch(t *testing.T) {
	a := fedoraAdapter{}
	got, err := a.ParseSearch("golang\t1.22.5-1.fc40\tx86_64\tupdates\tThe Go Programming Language\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []Package{{Name: "golang", Version: "1.22.5-1.fc40", Arch: "x86_64", Origin: "updates", Summary: "The Go Programming Language"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestFedoraSearchHostile(t *testing.T) {
	a := fedoraAdapter{}
	for _, c := range []string{
		"golang\t1.22.5\tx86_64\tupdates\n",        // missing summary field
		"golang\t1.22.5\tx86_64\t\x00updates\ts\n", // NUL repo
	} {
		if _, err := a.ParseSearch(c); err == nil {
			t.Fatalf("expected hostile search input to be refused: %q", c)
		}
	}
}

func TestFedoraUpdates(t *testing.T) {
	a := fedoraAdapter{}
	got, err := a.ParseUpdates("golang.x86_64 1.22.5-1.fc40 updates\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []Update{{Name: "golang", Arch: "x86_64", Origin: "updates", New: "1.22.5-1.fc40"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestFedoraUpdatesHostile(t *testing.T) {
	a := fedoraAdapter{}
	for _, c := range []string{
		"golang.x86_64 1.22.5-1.fc40\n",      // missing repo
		"golang 1.22.5 updates\n",            // missing arch
		"golang.x86_64 \x001.22.5 updates\n", // NUL
	} {
		if _, err := a.ParseUpdates(c); err == nil {
			t.Fatalf("expected hostile updates input to be refused: %q", c)
		}
	}
}

func TestFedoraSources(t *testing.T) {
	a := fedoraAdapter{}
	got, err := a.ParseSources("[fedora]\n" +
		"name=Fedora $releasever - $basearch\n" +
		"baseurl=https://dl.fedoraproject.org/pub/fedora/linux/releases/$releasever/Everything/$basearch/os/\n" +
		"enabled=1\n" +
		"gpgcheck=1\n" +
		"\n" +
		"[example]\n" +
		"baseurl=https://repo.example.com/custom\n" +
		"enabled=0\n" +
		"gpgcheck=0\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []Source{
		{Name: "fedora", Origin: "https://dl.fedoraproject.org/pub/fedora/linux/releases/$releasever/Everything/$basearch/os/", Enabled: true, Signed: true, ThirdParty: false},
		{Name: "example", Origin: "https://repo.example.com/custom", Enabled: false, Signed: false, ThirdParty: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestFedoraSourcesDefaultsSignedEnabled(t *testing.T) {
	a := fedoraAdapter{}
	got, err := a.ParseSources("[example]\nbaseurl=https://repo.example.com/custom\n")
	if err != nil {
		t.Fatal(err)
	}
	if !got[0].Enabled || !got[0].Signed || !got[0].ThirdParty {
		t.Fatalf("defaults wrong: %+v", got[0])
	}
}

func TestFedoraSourcesHostile(t *testing.T) {
	a := fedoraAdapter{}
	for _, c := range []string{
		"[example]\nbaseurl=https://repo.example.com/custom\n", // valid (no error)
		"baseurl=https://repo.example.com/custom\n",            // key outside section
		"[example]\n", // missing baseurl
	} {
		_, err := a.ParseSources(c)
		if c == "[example]\nbaseurl=https://repo.example.com/custom\n" {
			if err != nil {
				t.Fatalf("valid sources refused: %v", err)
			}
			continue
		}
		if err == nil {
			t.Fatalf("expected hostile sources input to be refused: %q", c)
		}
	}
}
