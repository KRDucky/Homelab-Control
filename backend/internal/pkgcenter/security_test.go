// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package pkgcenter

import "testing"

// R1: ThirdParty must not be spoofable to "vendor" by a substring host match.
func TestThirdPartyHostIsNotSubstringMatch(t *testing.T) {
	d := debianAdapter{}
	deb := func(uri string) bool {
		s, err := d.ParseSources("deb " + uri + " noble main\n")
		if err != nil {
			t.Fatal(err)
		}
		return s[0].ThirdParty
	}
	if deb("http://archive.ubuntu.com/ubuntu") {
		t.Error("archive.ubuntu.com should be vendor")
	}
	if !deb("http://archive.ubuntu.com.evil.example/ubuntu") {
		t.Error("archive.ubuntu.com.evil.example must be third-party")
	}
	if !deb("http://evil.example/archive.ubuntu.com/") {
		t.Error("evil.example/archive.ubuntu.com/ must be third-party")
	}

	f := fedoraAdapter{}
	fed := func(base string) bool {
		s, err := f.ParseSources("[example]\nbaseurl=" + base + "\n")
		if err != nil {
			t.Fatal(err)
		}
		return s[0].ThirdParty
	}
	if fed("https://download.fedoraproject.org/pub/fedora") {
		t.Error("fedoraproject.org should be vendor")
	}
	if !fed("https://evilredhat.com/") {
		t.Error("evilredhat.com must be third-party")
	}

	s := suseAdapter{}
	sus := func(url string) bool {
		out, err := s.ParseSources(`<?xml version='1.0'?><stream><repo-list><repo alias="r" url="` + url + `" gpgcheck="1" enabled="1"/></repo-list></stream>`)
		if err != nil {
			t.Fatal(err)
		}
		return out[0].ThirdParty
	}
	if sus("https://download.opensuse.org/tumbleweed/oss/") {
		t.Error("opensuse.org should be vendor")
	}
	if !sus("https://mysuse.com/oss/") {
		t.Error("mysuse.com must be third-party")
	}

	a := archAdapter{}
	arch := func(server string) bool {
		out, err := a.ParseSources("[core]\nServer=" + server + "\n")
		if err != nil {
			t.Fatal(err)
		}
		return out[0].ThirdParty
	}
	if arch("https://geo.mirror.pkgbuild.com/$repo/os/$arch") {
		t.Error("pkgbuild.com mirror should be vendor")
	}
	if !arch("http://evil.example/$repo/os/$arch") {
		t.Error("[core] with an evil Server must be third-party")
	}
}

// R2: Signed fails closed — unsigned unless positively established.
func TestSignedFailsClosed(t *testing.T) {
	d := debianAdapter{}
	deb := func(opts string) bool {
		uri := "http://archive.ubuntu.com/ubuntu"
		line := "deb " + opts + uri + " noble main\n"
		if opts == "" {
			line = "deb " + uri + " noble main\n"
		}
		s, err := d.ParseSources(line)
		if err != nil {
			t.Fatal(err)
		}
		return s[0].Signed
	}
	if !deb("") {
		t.Error("debian default must be signed")
	}
	for _, o := range []string{"[trusted=yes] ", "[trusted=true] ", "[trusted=1] ", "[allow-insecure=yes] ", "[allow-weak=yes] "} {
		if deb(o) {
			t.Errorf("debian %q must be unsigned", o)
		}
	}
	if !deb("[trusted=no] ") {
		t.Error("debian trusted=no must be signed")
	}

	f := fedoraAdapter{}
	fed := func(body string) bool {
		s, err := f.ParseSources(body)
		if err != nil {
			t.Fatal(err)
		}
		return s[0].Signed
	}
	if !fed("[example]\nbaseurl=https://x.example.com/\ngpgcheck=1\n") {
		t.Error("fedora gpgcheck=1 must be signed")
	}
	if fed("[example]\nbaseurl=https://x.example.com/\ngpgcheck=0\n") {
		t.Error("fedora gpgcheck=0 must be unsigned")
	}
	if fed("[example]\nbaseurl=https://x.example.com/\n") {
		t.Error("fedora missing gpgcheck must be unsigned")
	}

	s := suseAdapter{}
	sus := func(gpg string) bool {
		out, err := s.ParseSources(`<?xml version='1.0'?><stream><repo-list><repo alias="r" url="https://x.example.com/" gpgcheck="` + gpg + `" enabled="1"/></repo-list></stream>`)
		if err != nil {
			t.Fatal(err)
		}
		return out[0].Signed
	}
	if !sus("1") {
		t.Error("suse gpgcheck=1 must be signed")
	}
	if sus("") {
		t.Error("suse missing gpgcheck must be unsigned")
	}

	a := archAdapter{}
	arch := func(text string) bool {
		out, err := a.ParseSources(text)
		if err != nil {
			t.Fatal(err)
		}
		return out[0].Signed
	}
	if !arch("[core]\nServer=https://geo.mirror.pkgbuild.com/$repo/os/$arch\n") {
		t.Error("arch default SigLevel must be signed")
	}
	if arch("[options]\nSigLevel=Never\n[core]\nServer=https://geo.mirror.pkgbuild.com/$repo/os/$arch\n") {
		t.Error("arch SigLevel=Never must be unsigned")
	}
	if arch("[core]\nSigLevel=Optional TrustAll\nServer=https://geo.mirror.pkgbuild.com/$repo/os/$arch\n") {
		t.Error("arch SigLevel=Optional must be unsigned")
	}
	for _, sig := range []string{"PackageNever", "DatabaseNever"} {
		if arch("[core]\nSigLevel=" + sig + "\nServer=https://geo.mirror.pkgbuild.com/$repo/os/$arch\n") {
			t.Errorf("arch SigLevel=%s must be unsigned", sig)
		}
	}
	if !arch("[options]\nSigLevel=Required DatabaseOptional\n[core]\nServer=https://geo.mirror.pkgbuild.com/$repo/os/$arch\n") {
		t.Error("arch Required DatabaseOptional must be signed")
	}
}

// R3: real rows must not be silently dropped by a too-coarse dedupe key.
func TestNoSilentDedupe(t *testing.T) {
	f := fedoraAdapter{}
	got, err := f.ParseInstalled("kernel-core\t6.1\tx86_64\nkernel-core\t6.2\tx86_64\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("multi-version installed rows lost: %+v", got)
	}
	upd, err := f.ParseUpdates("glibc.x86_64 2.38 fedora\nglibc.i686 2.38 fedora\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(upd) != 2 {
		t.Fatalf("multilib update rows lost: %+v", upd)
	}

	a := archAdapter{}
	search, err := a.ParseSearch("extra/foo 1.0\nextra-testing/foo 2.0\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(search) != 2 || search[0].Origin != "extra" || search[1].Origin != "extra-testing" {
		t.Fatalf("arch cross-repo rows lost: %+v", search)
	}
}

// R4: package names must be validated as names (charset, no leading dash, no
// whitespace) so they can never smuggle argv into a broker template.
func TestPackageNameCharset(t *testing.T) {
	d := debianAdapter{}
	for _, good := range []string{"apt", "libssl3", "gcc-12", "c++", "libfoo~beta", "a:b"} {
		if _, err := d.ParseInstalled(good + "\t1.0\tamd64\n"); err != nil {
			t.Errorf("valid name %q refused: %v", good, err)
		}
	}
	for _, bad := range []string{"-oAPT", "--allow-unauthenticated", "a b", "a/b", "a;b", "a$(id)", "a`id`", "a*b"} {
		if _, err := d.ParseInstalled(bad + "\t1.0\tamd64\n"); err == nil {
			t.Errorf("hostile name %q accepted", bad)
		}
	}
}

// Q2: repository URLs must not leak userinfo or query tokens.
func TestStripCredentials(t *testing.T) {
	for in, want := range map[string]string{
		"https://user:pass@host/path?token=x": "https://host/path",
		"https://user:pass@host/path":         "https://host/path",
		"https://host/path?token=x":           "https://host/path",
		"https://host/path":                   "https://host/path",
		"/etc/pacman.d/mirrorlist":            "/etc/pacman.d/mirrorlist",
	} {
		if got := stripCredentials(in); got != want {
			t.Errorf("stripCredentials(%q) = %q, want %q", in, got, want)
		}
	}
}

// F1: deb822 .sources must parse (or at least not fail the whole list) and
// must apply the same signed/third-party/credential rules.
func TestDebianDeb822Sources(t *testing.T) {
	a := debianAdapter{}
	got, err := a.ParseSources("Types: deb\nURIs: https://deb.debian.org/debian\nSuites: bookworm\nComponents: main\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "bookworm" || got[0].Origin != "https://deb.debian.org/debian" || !got[0].Signed || got[0].ThirdParty {
		t.Fatalf("deb822: %+v", got)
	}
	u, err := a.ParseSources("Types: deb\nURIs: https://deb.debian.org/debian\nSuites: bookworm\nTrusted: yes\n")
	if err != nil {
		t.Fatal(err)
	}
	if u[0].Signed {
		t.Fatalf("Trusted: yes must be unsigned: %+v", u[0])
	}
	// Allow-Insecure / Allow-Weak and a case-folded allow-insecure are unsigned.
	for _, field := range []string{"Allow-Insecure: yes", "Allow-Weak: yes", "allow-insecure: yes"} {
		w, err := a.ParseSources("Types: deb\nURIs: https://deb.debian.org/debian\nSuites: bookworm\n" + field + "\n")
		if err != nil {
			t.Fatal(err)
		}
		if w[0].Signed {
			t.Fatalf("deb822 %q must be unsigned: %+v", field, w[0])
		}
	}
	c, err := a.ParseSources("Types: deb\nURIs: https://user:pass@archive.ubuntu.com/ubuntu?token=x\nSuites: noble\n")
	if err != nil {
		t.Fatal(err)
	}
	if c[0].Origin != "https://archive.ubuntu.com/ubuntu" || c[0].ThirdParty {
		t.Fatalf("deb822 credentials not stripped: %+v", c[0])
	}
	// Multi-suite URI produces one row per suite.
	m, err := a.ParseSources("Types: deb\nURIs: https://deb.debian.org/debian\nSuites: bookworm bookworm-updates\nComponents: main\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 2 || m[0].Name != "bookworm" || m[1].Name != "bookworm-updates" {
		t.Fatalf("multi-suite deb822: %+v", m)
	}
}
