// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package pkgcenter

import (
	"reflect"
	"testing"
)

func TestArchInstalled(t *testing.T) {
	a := archAdapter{}
	got, err := a.ParseInstalled("gcc 14.2.1+r134+gab884fffe3-1\nfirefox 130.0-1\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []Package{
		{Name: "gcc", Version: "14.2.1+r134+gab884fffe3-1", Installed: true},
		{Name: "firefox", Version: "130.0-1", Installed: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestArchInstalledHostile(t *testing.T) {
	a := archAdapter{}
	for _, c := range []string{
		"gcc\n",           // truncated
		"gcc 1.0 extra\n", // too many fields
		"gcc\x001.0\n",    // NUL
		"gcc 1\u202e0\n",  // bidi
	} {
		if _, err := a.ParseInstalled(c); err == nil {
			t.Fatalf("expected hostile installed input to be refused: %q", c)
		}
	}
}

func TestArchSearch(t *testing.T) {
	a := archAdapter{}
	got, err := a.ParseSearch("core/gcc 14.2.1+r134+gab884fffe3-1 [installed]\n" +
		"    The GNU Compiler Collection - C and C++ frontends\n" +
		"extra/firefox 130.0-1\n" +
		"    Standalone web browser from mozilla.org\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []Package{
		{Name: "gcc", Version: "14.2.1+r134+gab884fffe3-1", Origin: "core", Installed: true, Summary: "The GNU Compiler Collection - C and C++ frontends"},
		{Name: "firefox", Version: "130.0-1", Origin: "extra", Summary: "Standalone web browser from mozilla.org"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestArchSearchHostile(t *testing.T) {
	a := archAdapter{}
	for _, c := range []string{
		"gcc 14.2.1\n",             // missing repo/name slash
		"core/ 14.2.1\n",           // empty name
		"core/gcc\x0014.2.1\n",     // NUL
		"    orphan description\n", // description without header
	} {
		if _, err := a.ParseSearch(c); err == nil {
			t.Fatalf("expected hostile search input to be refused: %q", c)
		}
	}
}

func TestArchUpdates(t *testing.T) {
	a := archAdapter{}
	got, err := a.ParseUpdates("gcc 14.2.1+r134 -> 14.2.1+r135\nfirefox 130.0-1 -> 131.0-1\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []Update{
		{Name: "gcc", Current: "14.2.1+r134", New: "14.2.1+r135"},
		{Name: "firefox", Current: "130.0-1", New: "131.0-1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestArchUpdatesHostile(t *testing.T) {
	a := archAdapter{}
	for _, c := range []string{
		"gcc 14.2.1+r134 14.2.1+r135\n", // missing arrow
		"gcc -> 1.0\n",                  // missing current
		"gcc 1.0 ->\n",                  // missing new
	} {
		if _, err := a.ParseUpdates(c); err == nil {
			t.Fatalf("expected hostile updates input to be refused: %q", c)
		}
	}
}

func TestArchSources(t *testing.T) {
	a := archAdapter{}
	got, err := a.ParseSources("[options]\nArchitecture = auto\n\n" +
		"[core]\nInclude = /etc/pacman.d/mirrorlist\n\n" +
		"[extra]\nInclude = /etc/pacman.d/mirrorlist\n\n" +
		"[custom]\nSigLevel = Never\nServer = https://repo.example.com/$arch\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []Source{
		{Name: "core", Origin: "/etc/pacman.d/mirrorlist", Enabled: true, Signed: true, ThirdParty: false},
		{Name: "extra", Origin: "/etc/pacman.d/mirrorlist", Enabled: true, Signed: true, ThirdParty: false},
		{Name: "custom", Origin: "https://repo.example.com/$arch", Enabled: true, Signed: false, ThirdParty: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestArchSourcesHostile(t *testing.T) {
	a := archAdapter{}
	for _, c := range []string{
		"Server = https://repo.example.com\n", // outside section
		"[custom]\n",                          // missing Server/Include
	} {
		if _, err := a.ParseSources(c); err == nil {
			t.Fatalf("expected hostile sources input to be refused: %q", c)
		}
	}
}
