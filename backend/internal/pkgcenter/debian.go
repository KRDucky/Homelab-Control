// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package pkgcenter

import (
	"errors"
	"strings"
)

// debianAdapter parses apt/dpkg output (Debian, Ubuntu).
//
// Inputs (LC_ALL=C; exact flags verified in M4-1b):
//   - installed: dpkg-query -W -f '${binary:Package}\t${Version}\t${Architecture}\n'
//   - search:    apt-cache search <query>          ("name - summary" lines)
//   - updates:   apt list --upgradable
//   - sources:   /etc/apt/sources.list(.d/*) one-line "deb" stanzas
type debianAdapter struct{}

func (debianAdapter) Family() string { return FamilyDebian }

// ParseInstalled parses "name\tversion\tarch" lines.
func (debianAdapter) ParseInstalled(text string) ([]Package, error) {
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

// ParseSearch parses "name - summary" lines (first " - " only).
func (debianAdapter) ParseSearch(text string) ([]Package, error) {
	lns, err := lines(text)
	if err != nil {
		return nil, err
	}
	// apt-cache search reports no arch or repo, so name is the only dedupe key
	// available (R3: key on name+arch/repo "when present").
	var out []Package
	seen := map[string]bool{}
	for i, ln := range lns {
		if ln == "" {
			continue
		}
		name, rest, ok := strings.Cut(ln, " - ")
		if !ok || name == "" {
			return nil, malformed(i, "expected \"name - summary\"")
		}
		name, err := cleanPackageName(name)
		if err != nil {
			return nil, malformed(i, "invalid package name")
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, Package{Name: name, Summary: summary(rest)})
	}
	if len(out) > maxPackages {
		return nil, ErrTooMany
	}
	return out, nil
}

// ParseUpdates parses "name[/origin] newversion arch [upgradable from: old]".
// The "Listing..." header is skipped.
func (debianAdapter) ParseUpdates(text string) ([]Update, error) {
	lns, err := lines(text)
	if err != nil {
		return nil, err
	}
	var out []Update
	seen := map[string]bool{}
	for i, ln := range lns {
		if ln == "" || strings.HasPrefix(ln, "Listing") {
			continue
		}
		f := strings.Fields(ln)
		if len(f) < 3 {
			return nil, malformed(i, "expected \"name[/origin] version arch [upgradable from: old]\"")
		}
		name, origin, _ := strings.Cut(f[0], "/")
		name, err := cleanPackageName(name)
		if err != nil {
			return nil, malformed(i, "invalid package name")
		}
		origin, err = cleanField(origin, maxOrigin)
		if err != nil {
			return nil, malformed(i, "invalid origin")
		}
		newVersion, err := cleanField(f[1], maxVersion)
		if err != nil {
			return nil, malformed(i, "invalid version")
		}
		arch, err := cleanField(f[2], maxArch)
		if err != nil {
			return nil, malformed(i, "invalid architecture")
		}
		current, err := upgradableFrom(strings.Join(f[3:], " "))
		if err != nil {
			return nil, malformed(i, "expected \"[upgradable from: old]\"")
		}
		if current, err = cleanField(current, maxVersion); err != nil {
			return nil, malformed(i, "invalid current version")
		}
		if seen[name+"\x00"+arch] {
			continue
		}
		seen[name+"\x00"+arch] = true
		out = append(out, Update{Name: name, Arch: arch, Origin: origin, Current: current, New: newVersion})
	}
	if len(out) > maxPackages {
		return nil, ErrTooMany
	}
	return out, nil
}

// upgradableFrom extracts the old version from "[upgradable from: 1.2.3]".
// apt list --upgradable always reports it for an upgradable package, so a
// missing bracket is malformed rather than an empty current version.
func upgradableFrom(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[upgradable from: ") || !strings.HasSuffix(s, "]") {
		return "", errors.New("missing upgradable-from")
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, "[upgradable from: "), "]")), nil
}

// debianVendorHosts are the distribution archives treated as vendor (not
// third-party) for the Debian/Ubuntu family. A source whose URI contains any
// of these is vendor; anything else is third-party. Heuristic for the
// fixture; refined against 07-DISTRO-PACKAGING in M4-1b.
var debianVendorHosts = []string{
	"archive.ubuntu.com", "security.ubuntu.com", "ports.ubuntu.com",
	"deb.debian.org", "security.debian.org",
}

// ParseSources handles both one-line "deb" stanzas and deb822 "*.sources"
// stanzas (the format Ubuntu 24.04+ and Debian 13 ship by default).
func (debianAdapter) ParseSources(text string) ([]Source, error) {
	if isDeb822(text) {
		return debianParseDeb822(text)
	}
	return debianParseOneLine(text)
}

// isDeb822 reports whether the text is deb822 "*.sources" style (Key: value
// stanzas) rather than one-line "deb" stanzas.
func isDeb822(text string) bool {
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		for _, key := range []string{"Types:", "URIs:", "Suites:", "Components:", "Signed-By:"} {
			if strings.HasPrefix(ln, key) {
				return true
			}
		}
		return false // first non-comment line decides the format
	}
	return false
}

// debianParseDeb822 parses deb822 stanzas (Types/URIs/Suites/Components/Signed-By/Trusted).
func debianParseDeb822(text string) ([]Source, error) {
	lns, err := lines(text)
	if err != nil {
		return nil, err
	}
	var out []Source
	seen := map[string]bool{}
	var types, uris, suites []string
	var trusted bool
	inStanza := false
	flush := func() error {
		if !inStanza {
			return nil
		}
		isDeb := false
		for _, t := range types {
			if t == "deb" {
				isDeb = true
			}
		}
		for _, uri := range uris {
			uri, err := cleanField(uri, maxOrigin)
			if err != nil || uri == "" {
				return errEmpty("URIs")
			}
			if !isDeb {
				continue
			}
			for _, suite := range suites {
				suite, err := cleanPackageName(suite)
				if err != nil {
					return errEmpty("Suites")
				}
				key := suite + "\x00" + uri
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, Source{
					Name:       suite,
					Origin:     stripCredentials(uri),
					Enabled:    true,
					Signed:     !trusted,
					ThirdParty: !vendorHost(hostnameOf(uri), debianVendorHosts),
				})
			}
		}
		types, uris, suites = nil, nil, nil
		trusted = false
		inStanza = false
		return nil
	}
	for i, ln := range lns {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			if err := flush(); err != nil {
				return nil, malformed(i, err.Error())
			}
			continue
		}
		k, v, ok := strings.Cut(ln, ":")
		if !ok {
			return nil, malformed(i, "expected \"Key: value\"")
		}
		inStanza = true
		switch strings.TrimSpace(k) {
		case "Types":
			types = append(types, strings.Fields(strings.TrimSpace(v))...)
		case "URIs":
			uris = append(uris, strings.Fields(strings.TrimSpace(v))...)
		case "Suites":
			suites = append(suites, strings.Fields(strings.TrimSpace(v))...)
		case "Trusted":
			trusted = truthy(strings.TrimSpace(v))
		}
	}
	if err := flush(); err != nil {
		return nil, malformed(len(lns), err.Error())
	}
	if len(out) > maxSources {
		return nil, ErrTooMany
	}
	return out, nil
}

// debianParseOneLine parses one-line "deb" stanzas. "deb-src" and commented
// lines are ignored. trusted= allow-insecure= allow-weak= mark unsigned; the
// default is signed.
func debianParseOneLine(text string) ([]Source, error) {
	lns, err := lines(text)
	if err != nil {
		return nil, err
	}
	var out []Source
	seen := map[string]bool{}
	for i, ln := range lns {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") || strings.HasPrefix(ln, "deb-src") {
			continue
		}
		f := strings.Fields(ln)
		if len(f) < 3 || f[0] != "deb" {
			return nil, malformed(i, "expected a one-line \"deb\" stanza")
		}
		unsigned := false
		idx := 1
		if strings.HasPrefix(f[idx], "[") {
			// The bracketed option group may span multiple whitespace-separated
			// tokens, e.g. "[ arch=amd64 signed-by=/key.gpg ]".
			var opts []string
			for idx < len(f) {
				opts = append(opts, f[idx])
				done := strings.HasSuffix(f[idx], "]")
				idx++
				if done {
					break
				}
			}
			unsigned = debianUnsigned(strings.Join(opts, " "))
		}
		if idx+1 >= len(f) {
			return nil, malformed(i, "deb stanza missing suite")
		}
		uri := f[idx]
		suite := f[idx+1]
		uri, err := cleanField(uri, maxOrigin)
		if err != nil || uri == "" {
			return nil, malformed(i, "invalid URI")
		}
		suite, err = cleanPackageName(suite)
		if err != nil {
			return nil, malformed(i, "invalid suite")
		}
		key := suite + "\x00" + uri
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Source{
			Name:       suite,
			Origin:     stripCredentials(uri),
			Enabled:    true,
			Signed:     !unsigned,
			ThirdParty: !vendorHost(hostnameOf(uri), debianVendorHosts),
		})
	}
	if len(out) > maxSources {
		return nil, ErrTooMany
	}
	return out, nil
}

// debianUnsigned reports whether a one-line deb stanza disables signature
// verification: trusted=, allow-insecure= or allow-weak= with a truthy value.
// The default is signed (apt verifies Release signatures unless told not to).
func debianUnsigned(opts string) bool {
	for _, tok := range strings.Fields(strings.TrimSuffix(strings.TrimPrefix(opts, "["), "]")) {
		k, v, ok := strings.Cut(tok, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "trusted", "allow-insecure", "allow-weak":
			if truthy(strings.TrimSpace(v)) {
				return true
			}
		}
	}
	return false
}
