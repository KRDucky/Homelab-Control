// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package hostinfo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureRoots points at the labelled synthetic tree testdata/hostsettings.
func fixtureRoots(t *testing.T) Roots {
	root, err := filepath.Abs("testdata/hostsettings")
	if err != nil {
		t.Fatal(err)
	}
	return Roots{Proc: root + "/proc", Sys: root + "/sys", Etc: root + "/etc"}
}

func TestReadHostSettingsFromFixture(t *testing.T) {
	hs := ReadHostSettings(fixtureRoots(t))
	for name, c := range map[string]struct {
		got  Setting
		want string
	}{
		"kernel": {hs.KernelHostname, "fixture-kernel-name"},
		"static": {hs.StaticHostname, "fixture-static"},
		"pretty": {hs.PrettyHostname, "Fixture Lab (example)"},
		"tz":     {hs.Timezone, "Europe/Berlin"},
	} {
		if c.got.Status != SettingObserved || c.got.Value != c.want || c.got.Source == "" {
			t.Errorf("%s: %+v, want observed %q", name, c.got, c.want)
		}
	}
	if !strings.Contains(hs.KernelHostname.Note, "ProtectHostname") {
		t.Error("kernel host name must say it is the service's view")
	}
}

// Missing files are "not set" (a real state) and unreadable values are
// "unavailable" with no value: nothing is defaulted or invented.
func TestReadHostSettingsMissingAndUnreadable(t *testing.T) {
	root := t.TempDir()
	r := Roots{Proc: root + "/proc", Sys: root + "/sys", Etc: root + "/etc"}
	if err := os.MkdirAll(r.Etc, 0o755); err != nil {
		t.Fatal(err)
	}
	hs := ReadHostSettings(r)
	if hs.KernelHostname.Status != SettingUnavailable || hs.KernelHostname.Value != "" {
		t.Errorf("kernel: %+v", hs.KernelHostname)
	}
	for name, s := range map[string]Setting{"static": hs.StaticHostname, "pretty": hs.PrettyHostname, "tz": hs.Timezone} {
		if s.Status != SettingNotSet || s.Value != "" {
			t.Errorf("%s: %+v", name, s)
		}
	}
	// A directory where a file is expected cannot be read.
	if err := os.Mkdir(filepath.Join(r.Etc, "hostname"), 0o755); err != nil {
		t.Fatal(err)
	}
	// /etc/localtime as a copied file without /etc/timezone: name unknown.
	if err := os.WriteFile(filepath.Join(r.Etc, "localtime"), []byte("TZif2 copy"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Control characters in PRETTY_HOSTNAME are not relayed.
	if err := os.WriteFile(filepath.Join(r.Etc, "machine-info"), []byte("PRETTY_HOSTNAME=\"bad\x1b[31m\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hs = ReadHostSettings(r)
	for name, s := range map[string]Setting{"static": hs.StaticHostname, "pretty": hs.PrettyHostname, "tz": hs.Timezone} {
		if s.Status != SettingUnavailable || s.Value != "" {
			t.Errorf("%s: %+v", name, s)
		}
	}
	// With /etc/timezone, a copied /etc/localtime is named from it.
	if err := os.WriteFile(filepath.Join(r.Etc, "timezone"), []byte("Etc/UTC\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if tz := ReadHostSettings(r).Timezone; tz.Status != SettingObserved || tz.Value != "Etc/UTC" || !strings.HasSuffix(tz.Source, "timezone") {
		t.Errorf("tz from /etc/timezone: %+v", tz)
	}
}

func TestLocaltimeLinkOutsideZoneinfoIsUnavailable(t *testing.T) {
	root := t.TempDir()
	r := Roots{Proc: root + "/proc", Etc: root + "/etc"}
	_ = os.MkdirAll(r.Etc, 0o755)
	if err := os.Symlink("/tmp/not-a-zone", filepath.Join(r.Etc, "localtime")); err != nil {
		t.Fatal(err)
	}
	if tz := ReadHostSettings(r).Timezone; tz.Status != SettingUnavailable || tz.Value != "" {
		t.Errorf("%+v", tz)
	}
}

func TestValidStaticHostname(t *testing.T) {
	for _, ok := range []string{"nas", "nas-01", "nas.home.arpa", "a", strings.Repeat("a", 63)} {
		if r := ValidStaticHostname(ok); r != "" {
			t.Errorf("%q rejected: %s", ok, r)
		}
	}
	for _, bad := range []string{"", "NAS", "-nas", "nas-", "nas..lan", "nas.", ".nas", "nas_01", "nas 01", "näs", "nas;reboot", "$(id)", strings.Repeat("a", 65), strings.Repeat("a", 64) + ".b"} {
		if ValidStaticHostname(bad) == "" {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestValidPrettyHostname(t *testing.T) {
	for _, ok := range []string{"", "Ian's NAS", "Büro-Server 🖥"} {
		if r := ValidPrettyHostname(ok); r != "" {
			t.Errorf("%q rejected: %s", ok, r)
		}
	}
	for _, bad := range []string{"line\nbreak", "tab\there", "esc\x1b", "\xff\xfe", strings.Repeat("x", 256)} {
		if ValidPrettyHostname(bad) == "" {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestTimezoneExistsIsConfinedAndChecksTZif(t *testing.T) {
	r := fixtureRoots(t)
	for name, want := range map[string]bool{
		"Europe/Berlin": true, "Etc/UTC": true,
		"Europe/Nowhere":  false, // absent
		"zone.tab":        false, // syntax: '.' is not allowed
		"Europe":          false, // a directory
		"../etc/hostname": false, "/etc/hostname": false, "Europe/../../../etc/hostname": false,
	} {
		got, err := TimezoneExists(r.Etc, name)
		if err != nil || got != want {
			t.Errorf("%q: %v %v, want %v", name, got, err, want)
		}
	}
	// A symbolic link inside the database that leaves it is refused by os.Root.
	root := t.TempDir()
	etc := filepath.Join(root, "etc")
	zi := ZoneinfoDir(etc)
	_ = os.MkdirAll(zi, 0o755)
	outside := filepath.Join(root, "outside")
	_ = os.WriteFile(outside, []byte("TZif2 outside"), 0o644)
	if err := os.Symlink(outside, filepath.Join(zi, "Escape")); err != nil {
		t.Fatal(err)
	}
	if ok, _ := TimezoneExists(etc, "Escape"); ok {
		t.Fatal("followed a link out of the time zone database")
	}
	// Without a database the name cannot be checked: an error, not "absent".
	if _, err := TimezoneExists(filepath.Join(t.TempDir(), "etc"), "Etc/UTC"); err == nil {
		t.Fatal("missing database reported as a definite answer")
	}
}
