// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package aptapply

import (
	"errors"
	"strings"
	"testing"
)

func bp(b bool) *bool { return &b }
func ip(i int) *int   { return &i }

// Fixtures below are test data shaped like the distributions' default files.

const ubuntuDeb822 = `Types: deb
URIs: http://us.archive.ubuntu.com/ubuntu/
Suites: noble noble-updates noble-backports
Components: main restricted universe multiverse
Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg

## Ubuntu security updates.
Types: deb
URIs: http://security.ubuntu.com/ubuntu/
Suites: noble-security
Components: main restricted universe multiverse
Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg
`

const debianDeb822 = `Types: deb deb-src
URIs: https://deb.debian.org/debian
Suites: trixie trixie-updates
Components: main non-free-firmware
Signed-By: /usr/share/keyrings/debian-archive-keyring.pgp

Types: deb deb-src
URIs: https://security.debian.org/debian-security
Suites: trixie-security
Components: main non-free-firmware
Signed-By: /usr/share/keyrings/debian-archive-keyring.pgp
`

const ubuntuOneLine = `# See http://help.ubuntu.com/community/UpgradeNotes
deb http://us.archive.ubuntu.com/ubuntu/ jammy main restricted
# deb-src http://us.archive.ubuntu.com/ubuntu/ jammy main restricted
deb http://us.archive.ubuntu.com/ubuntu/ jammy-updates main restricted
deb  [arch=amd64]  http://us.archive.ubuntu.com/ubuntu/   jammy-backports main  # keep spacing
deb http://security.ubuntu.com/ubuntu jammy-security main restricted
deb https://download.docker.com/linux/ubuntu jammy stable
`

var (
	noble  = Distro{ID: "ubuntu", Codename: "noble"}
	trixie = Distro{ID: "debian", Codename: "trixie"}
	jammy  = Distro{ID: "ubuntu", Codename: "jammy"}
)

func TestRenderIsTypedAndRoundTrips(t *testing.T) {
	in := Tunables{InstallRecommends: bp(false), InstallSuggests: bp(false), SkipTranslations: bp(true),
		ForceIPv4: bp(true), PDiffs: bp(false), KeepDownloaded: bp(true), Retries: ip(3), TimeoutSeconds: ip(30),
		QueueMode: "access", HTTPProxy: "http://apt-cache.lan:3142"}
	out, err := Render(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`APT::Install-Recommends "false";`, `Acquire::Languages "none";`, `Acquire::Retries "3";`,
		`Acquire::http::Timeout "30";`, `Acquire::https::Timeout "30";`, `Acquire::Queue-Mode "access";`,
		`Acquire::http::Proxy "http://apt-cache.lan:3142";`, `Binary::apt::APT::Keep-Downloaded-Packages "true";`,
	} {
		if !strings.Contains(out, want+"\n") {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	back, ok := ParseManaged(out)
	if !ok {
		t.Fatalf("own output not recognised:\n%s", out)
	}
	again, _ := Render(back)
	if again != out {
		t.Fatalf("round trip changed the file:\n%s\n---\n%s", out, again)
	}
	if empty, _ := Render(Tunables{}); strings.Count(empty, "\n") != 2 {
		t.Fatalf("empty tunables should render the header only: %q", empty)
	}
	if _, ok := ParseManaged(out + "APT::Get::Assume-Yes \"true\";\n"); ok {
		t.Fatal("a hand-added key must mark the file as changed outside Homelab Control")
	}
}

func TestTunablesRefuseOutOfRangeAndInjection(t *testing.T) {
	for name, tun := range map[string]Tunables{
		"retries":  {Retries: ip(11)},
		"negative": {Retries: ip(-1)},
		"timeout":  {TimeoutSeconds: ip(4)},
		"queue":    {QueueMode: "fast"},
	} {
		if _, err := Render(tun); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: accepted", name)
		}
	}
	for _, p := range []string{
		"http://user:pw@proxy:3142", "ftp://proxy", "http://proxy:3142/path", "http://proxy:99999",
		`http://proxy";APT::Get::Assume-Yes "true`, "http://proxy:3142\nAPT::X \"1\";", "http://proxy?x=1",
		"http://pro xy", "http://{proxy}", "proxy:3142", "http://", "http://proxy#frag",
	} {
		if err := ValidProxy(p); err == nil {
			t.Errorf("proxy %q accepted", p)
		}
	}
	for _, p := range []string{"http://apt-cache.lan:3142", "https://10.0.0.5:3142/", "http://[fd00::5]:3142"} {
		if err := ValidProxy(p); err != nil {
			t.Errorf("proxy %q refused: %v", p, err)
		}
	}
}

func TestParseDumpReadsRealFormat(t *testing.T) {
	// Fixture: apt-config dump lines (apt 3.3.3); other keys omitted.
	dump := `APT "";
APT::Install-Recommends "1";
APT::Install-Suggests "0";
Acquire::ForceIPv4 "true";
Acquire::Languages "";
Acquire::Languages:: "none";
Acquire::Retries "3";
Acquire::http::Proxy "http://apt-cache.lan:3142";
`
	e, err := ParseDump(dump)
	if err != nil {
		t.Fatal(err)
	}
	if !e.InstallRecommends || e.InstallSuggests || !e.ForceIPv4 || !e.SkipTranslations || e.Retries != 3 ||
		!e.HTTPProxy || e.TimeoutSeconds != 120 || e.QueueMode != "host" || !e.PDiffs {
		t.Fatalf("effective = %+v", e)
	}
	e2, _ := ParseDump("Acquire::Languages \"\";\nAcquire::Languages:: \"en\";\nAcquire::Languages:: \"none\";\n")
	if e2.SkipTranslations {
		t.Fatal("a list with en is not skipping translations")
	}
	if _, err := ParseDump(`Acquire::Retries "many";`); err == nil {
		t.Fatal("non-numeric retries accepted")
	}
}

func TestOverridesReportsLaterFiles(t *testing.T) {
	want := Tunables{InstallRecommends: bp(false), Retries: ip(3), SkipTranslations: bp(true)}
	e := defaultEffective()
	e.Retries = 3
	got := Overrides(want, e)
	if strings.Join(got, ",") != "install_recommends,skip_translations" {
		t.Fatalf("overrides = %v", got)
	}
	e.InstallRecommends, e.SkipTranslations = false, true
	if got := Overrides(want, e); len(got) != 0 {
		t.Fatalf("no override expected: %v", got)
	}
}

func TestParseSourcesClassifies(t *testing.T) {
	es, err := ParseSources(UbuntuSources, ubuntuDeb822, noble)
	if err != nil || len(es) != 2 {
		t.Fatalf("%v %+v", err, es)
	}
	if es[0].Role != RoleArchive || !es[0].Switchable || es[0].URI != "http://us.archive.ubuntu.com/ubuntu/" || es[0].Line != 2 {
		t.Fatalf("archive: %+v", es[0])
	}
	if es[1].Role != RoleSecurity || es[1].Switchable {
		t.Fatalf("security must not be switchable: %+v", es[1])
	}
	es, err = ParseSources(DebianSources, debianDeb822, trixie)
	if err != nil || es[0].Role != RoleArchive || !es[0].Switchable || es[1].Role != RoleSecurity {
		t.Fatalf("debian: %v %+v", err, es)
	}
	es, err = ParseSources(SourcesList, ubuntuOneLine, jammy)
	if err != nil || len(es) != 5 {
		t.Fatalf("one-line: %v %d", err, len(es))
	}
	roles := []string{}
	for _, e := range es {
		roles = append(roles, e.Role)
	}
	if strings.Join(roles, ",") != "archive,archive,archive,security,other" {
		t.Fatalf("roles = %v", roles)
	}
	if es[2].SignedBy != "" || es[2].Components[0] != "main" {
		t.Fatalf("options entry: %+v", es[2])
	}
	if _, err := ParseSources("/etc/apt/sources.list.d/docker.sources", ubuntuDeb822, noble); err == nil {
		t.Fatal("a non-vendor file was accepted")
	}
}

func TestSetMirrorChangesOnlyTheURI(t *testing.T) {
	const to = "http://mirror.example.edu/ubuntu/"
	out, n, err := SetMirror(UbuntuSources, ubuntuDeb822, noble, "http://us.archive.ubuntu.com/ubuntu/", to)
	if err != nil || n != 1 {
		t.Fatalf("%v %d", err, n)
	}
	if want := strings.Replace(ubuntuDeb822, "http://us.archive.ubuntu.com/ubuntu/", to, 1); out != want {
		t.Fatalf("deb822 edit changed more than the URI:\n%s", out)
	}
	out, n, err = SetMirror(SourcesList, ubuntuOneLine, jammy, "http://us.archive.ubuntu.com/ubuntu/", to)
	if err != nil || n != 3 {
		t.Fatalf("one-line: %v %d", err, n)
	}
	// The commented deb-src line, the security line and the third-party line keep their URI.
	want := strings.NewReplacer(
		"deb http://us.archive.ubuntu.com/ubuntu/ jammy main", "deb "+to+" jammy main",
		"deb http://us.archive.ubuntu.com/ubuntu/ jammy-updates", "deb "+to+" jammy-updates",
		"[arch=amd64]  http://us.archive.ubuntu.com/ubuntu/   jammy-backports", "[arch=amd64]  "+to+"   jammy-backports",
	).Replace(ubuntuOneLine)
	if out != want {
		t.Fatalf("one-line edit:\n%s\nwant\n%s", out, want)
	}
	if _, _, err := SetMirror(UbuntuSources, ubuntuDeb822, noble, "http://security.ubuntu.com/ubuntu/", to); err == nil {
		t.Fatal("security entry was switched")
	}
	if _, _, err := SetMirror(UbuntuSources, ubuntuDeb822, noble, "http://nowhere.example/ubuntu/", to); err == nil {
		t.Fatal("a URI that no entry uses was accepted")
	}
}

func TestSourcesRefuseAmbiguousContent(t *testing.T) {
	cases := map[string]string{
		"two URIs":     strings.Replace(ubuntuDeb822, "URIs: http://us.archive.ubuntu.com/ubuntu/", "URIs: http://a.example/ubuntu/ http://b.example/ubuntu/", 1),
		"folded URIs":  strings.Replace(ubuntuDeb822, "URIs: http://us.archive.ubuntu.com/ubuntu/", "URIs: http://us.archive.ubuntu.com/ubuntu/\n http://b.example/ubuntu/", 1),
		"mixed suites": strings.Replace(ubuntuDeb822, "Suites: noble noble-updates noble-backports", "Suites: noble noble-security", 1),
		"foreign key":  strings.Replace(ubuntuDeb822, "/usr/share/keyrings/ubuntu-archive-keyring.gpg\n\n", "/etc/apt/keyrings/other.gpg\n\n", 1),
		"disabled":     strings.Replace(ubuntuDeb822, "Types: deb\nURIs: http://us", "Enabled: no\nTypes: deb\nURIs: http://us", 1),
	}
	for name, text := range cases {
		es, err := ParseSources(UbuntuSources, text, noble)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if es[0].Switchable {
			t.Errorf("%s: switchable", name)
		}
		if es[0].Reason == "" {
			t.Errorf("%s: no reason given", name)
		}
	}
	for name, text := range map[string]string{
		"repeated field": strings.Replace(ubuntuDeb822, "Types: deb\nURIs:", "Types: deb\nTypes: deb\nURIs:", 1),
		"CR":             strings.ReplaceAll(ubuntuDeb822, "\n", "\r\n"),
		"no URIs":        "Types: deb\nSuites: noble\n",
	} {
		if _, err := ParseSources(UbuntuSources, text, noble); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
	if _, err := ParseSources(SourcesList, "deb\n", jammy); err == nil {
		t.Error("short one-line entry parsed")
	}
	for _, u := range []string{"http://u:p@m.example/ubuntu/", "ftp://m.example/ubuntu", "http://m.example/ubuntu?x", "http://m.example/a b", "file:/srv/mirror"} {
		if ValidMirrorURI(u) == nil {
			t.Errorf("mirror %q accepted", u)
		}
	}
}

func TestValidateRequest(t *testing.T) {
	d := strings.Repeat("a", 64)
	ok := []Request{
		{Version: 1, Action: ActionConfWrite, Tunables: Tunables{Retries: ip(2)}},
		{Version: 1, Action: ActionConfRemove, Digest: d},
		{Version: 1, Action: ActionMirrorSet, File: UbuntuSources, From: "http://a.example/ubuntu/", To: "http://b.example/ubuntu/", Digest: d},
	}
	for _, r := range ok {
		if err := Validate(r); err != nil {
			t.Errorf("%+v: %v", r, err)
		}
	}
	bad := []Request{
		{Version: 2, Action: ActionConfWrite},
		{Version: 1, Action: "conf.chmod"},
		{Version: 1, Action: ActionConfWrite, File: "/etc/shadow"},
		{Version: 1, Action: ActionConfRemove, Tunables: Tunables{Retries: ip(1)}},
		{Version: 1, Action: ActionMirrorSet, File: "/etc/apt/sources.list.d/docker.sources", From: "http://a.example/", To: "http://b.example/", Digest: d},
		{Version: 1, Action: ActionMirrorSet, File: UbuntuSources, From: "http://a.example/", To: "http://a.example/", Digest: d},
		{Version: 1, Action: ActionMirrorSet, File: UbuntuSources, From: "http://a.example/", To: "http://b.example/"},
		{Version: 1, Action: ActionConfWrite, Digest: "XYZ"},
	}
	for _, r := range bad {
		if err := Validate(r); err == nil {
			t.Errorf("accepted %+v", r)
		}
	}
}

func TestParseOSRelease(t *testing.T) {
	d := ParseOSRelease("PRETTY_NAME=\"Ubuntu 24.04\"\nID=ubuntu\nVERSION_CODENAME=noble\n")
	if d != noble {
		t.Fatalf("%+v", d)
	}
	if d := ParseOSRelease("ID=debian\nVERSION_CODENAME=\"tri;xie\"\n"); d.Codename != "" {
		t.Fatalf("bad codename accepted: %+v", d)
	}
}
