// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package pkgcenter

import "strings"

// fedoraAdapter parses rpm/dnf output (Fedora, RHEL, CentOS and like).
//
// Inputs (LC_ALL=C; exact flags verified in M4-1b):
//   - installed: rpm -qa --queryformat '%{NAME}\t%{VERSION}-%{RELEASE}\t%{ARCH}\n'
//   - search:    dnf repoquery --queryformat '%{NAME}\t%{VERSION}-%{RELEASE}\t%{ARCH}\t%{REPONAME}\t%{SUMMARY}\n' <query>
//   - updates:   dnf check-update ("name.arch version repo" lines; the current
//     version is not reported, so Update.Current stays empty)
//   - sources:   /etc/yum.repos.d/*.repo INI sections
type fedoraAdapter struct{}

func (fedoraAdapter) Family() string { return FamilyFedora }

// ParseInstalled parses "name\tversion\tarch" lines.
func (fedoraAdapter) ParseInstalled(text string) ([]Package, error) {
	lns, err := lines(text)
	if err != nil {
		return nil, err
	}
	var out []Package
	seen := map[string]bool{}
	for i, ln := range lns {
		if ln == "" {
			continue
		}
		f := strings.Split(ln, "\t")
		if len(f) != 3 {
			return nil, malformed(i, "expected 3 tab-separated fields")
		}
		name, err := cleanPackageName(f[0])
		if err != nil {
			return nil, malformed(i, "invalid package name")
		}
		version, err := cleanField(f[1], maxVersion)
		if err != nil {
			return nil, malformed(i, "invalid version")
		}
		arch, err := cleanField(f[2], maxArch)
		if err != nil {
			return nil, malformed(i, "invalid architecture")
		}
		key := name + "\x00" + arch + "\x00" + version
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Package{Name: name, Version: version, Arch: arch, Installed: true})
	}
	if len(out) > maxPackages {
		return nil, ErrTooMany
	}
	return out, nil
}

// ParseSearch parses "name\tversion\tarch\trepo\tsummary" lines. The summary
// is the last field and may itself contain tabs, so only the first four tabs
// split.
func (fedoraAdapter) ParseSearch(text string) ([]Package, error) {
	lns, err := lines(text)
	if err != nil {
		return nil, err
	}
	var out []Package
	seen := map[string]bool{}
	for i, ln := range lns {
		if ln == "" {
			continue
		}
		f := strings.SplitN(ln, "\t", 5)
		if len(f) != 5 {
			return nil, malformed(i, "expected 5 tab-separated fields")
		}
		name, err := cleanPackageName(f[0])
		if err != nil {
			return nil, malformed(i, "invalid package name")
		}
		version, err := cleanField(f[1], maxVersion)
		if err != nil {
			return nil, malformed(i, "invalid version")
		}
		arch, err := cleanField(f[2], maxArch)
		if err != nil {
			return nil, malformed(i, "invalid architecture")
		}
		repo, err := cleanField(f[3], maxOrigin)
		if err != nil {
			return nil, malformed(i, "invalid repository")
		}
		key := name + "\x00" + arch + "\x00" + repo
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Package{Name: name, Version: version, Arch: arch, Origin: repo, Summary: summary(f[4])})
	}
	if len(out) > maxPackages {
		return nil, ErrTooMany
	}
	return out, nil
}

// ParseUpdates parses "name.arch version repo" lines (whitespace-separated).
// dnf check-update does not report the installed version, so Current is empty.
func (fedoraAdapter) ParseUpdates(text string) ([]Update, error) {
	lns, err := lines(text)
	if err != nil {
		return nil, err
	}
	var out []Update
	seen := map[string]bool{}
	for i, ln := range lns {
		if ln == "" {
			continue
		}
		f := strings.Fields(ln)
		if len(f) != 3 {
			return nil, malformed(i, "expected \"name.arch version repo\"")
		}
		name, arch, ok := strings.Cut(f[0], ".")
		if !ok {
			return nil, malformed(i, "expected name.arch in first field")
		}
		name, err := cleanPackageName(name)
		if err != nil {
			return nil, malformed(i, "invalid package name")
		}
		arch, err = cleanField(arch, maxArch)
		if err != nil {
			return nil, malformed(i, "invalid architecture")
		}
		newVersion, err := cleanField(f[1], maxVersion)
		if err != nil {
			return nil, malformed(i, "invalid version")
		}
		repo, err := cleanField(f[2], maxOrigin)
		if err != nil {
			return nil, malformed(i, "invalid repository")
		}
		if seen[name+"\x00"+arch] {
			continue
		}
		seen[name+"\x00"+arch] = true
		out = append(out, Update{Name: name, Arch: arch, Origin: repo, New: newVersion})
	}
	if len(out) > maxPackages {
		return nil, ErrTooMany
	}
	return out, nil
}

// fedoraVendorHosts are the vendor archives for the rpm family (Fedora,
// CentOS/Stream, RHEL, Alma, Rocky). Anything else is third-party. Heuristic
// for the fixture; refined against 07-DISTRO-PACKAGING in M4-1b.
var fedoraVendorHosts = []string{
	"fedoraproject.org", "centos.org", "redhat.com", "almalinux.org", "rocky.org",
}

// ParseSources parses .repo INI sections: [id], enabled=, gpgcheck=,
// baseurl=/metalink=. Missing gpgcheck means unsigned (fail closed).
func (fedoraAdapter) ParseSources(text string) ([]Source, error) {
	lns, err := lines(text)
	if err != nil {
		return nil, err
	}
	var out []Source
	seen := map[string]bool{}
	var cur *sourceBuilder
	for i, ln := range lns {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") || strings.HasPrefix(ln, ";") {
			continue
		}
		if strings.HasPrefix(ln, "[") && strings.HasSuffix(ln, "]") {
			if cur != nil {
				if src, err := cur.finish(); err != nil {
					return nil, malformed(i, err.Error())
				} else if key := src.Name + "\x00" + src.Origin; !seen[key] {
					seen[key] = true
					out = append(out, src)
				}
			}
			id := strings.TrimSuffix(strings.TrimPrefix(ln, "["), "]")
			cur = &sourceBuilder{name: id, enabled: true, signed: false}
			continue
		}
		k, v, ok := strings.Cut(ln, "=")
		if !ok || cur == nil {
			return nil, malformed(i, "expected key=value inside a [section]")
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		switch k {
		case "enabled":
			cur.enabled = v == "1" || v == "true" || v == "yes"
		case "gpgcheck":
			cur.signed = truthy(v)
		case "baseurl", "metalink":
			cur.origin = v
		}
	}
	if cur != nil {
		if src, err := cur.finish(); err != nil {
			return nil, malformed(len(lns), err.Error())
		} else if key := src.Name + "\x00" + src.Origin; !seen[key] {
			seen[key] = true
			out = append(out, src)
		}
	}
	if len(out) > maxSources {
		return nil, ErrTooMany
	}
	return out, nil
}

// sourceBuilder accumulates one .repo section.
type sourceBuilder struct {
	name    string
	origin  string
	enabled bool
	signed  bool
}

func (b *sourceBuilder) finish() (Source, error) {
	name, err := cleanPackageName(b.name)
	if err != nil {
		return Source{}, errEmpty("repository id")
	}
	origin, err := cleanField(b.origin, maxOrigin)
	if err != nil || origin == "" {
		return Source{}, errEmpty("baseurl")
	}
	return Source{
		Name:       name,
		Origin:     origin,
		Enabled:    b.enabled,
		Signed:     b.signed,
		ThirdParty: !vendorHost(hostnameOf(origin), fedoraVendorHosts),
	}, nil
}
