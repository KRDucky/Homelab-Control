// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package pkgcenter

import "strings"

// archAdapter parses pacman output (Arch Linux, and derivatives via ID_LIKE
// such as CachyOS).
//
// Inputs (LC_ALL=C; exact flags verified in M4-1b):
//   - installed: pacman -Q          ("name version" lines)
//   - search:    pacman -Ss <query> ("repo/name version [installed]" lines
//     followed by an indented description)
//   - updates:   pacman -Qu         ("name old -> new" lines)
//   - sources:   /etc/pacman.conf   ([repo] sections with Server/Include/SigLevel)
type archAdapter struct{}

func (archAdapter) Family() string { return FamilyArch }

// ParseInstalled parses "name version" lines.
func (archAdapter) ParseInstalled(text string) ([]Package, error) {
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
		f := strings.Fields(ln)
		if len(f) != 2 {
			return nil, malformed(i, "expected \"name version\"")
		}
		name, err := cleanField(f[0], maxName)
		if err != nil || name == "" {
			return nil, malformed(i, "invalid package name")
		}
		version, err := cleanField(f[1], maxVersion)
		if err != nil {
			return nil, malformed(i, "invalid version")
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, Package{Name: name, Version: version, Installed: true})
	}
	if len(out) > maxPackages {
		return nil, ErrTooMany
	}
	return out, nil
}

// ParseSearch parses "repo/name version [flags]" header lines with an
// indented description following each header.
func (archAdapter) ParseSearch(text string) ([]Package, error) {
	lns, err := lines(text)
	if err != nil {
		return nil, err
	}
	var out []Package
	seen := map[string]bool{}
	var cur *Package
	for i, ln := range lns {
		if ln == "" {
			continue
		}
		if strings.HasPrefix(ln, " ") || strings.HasPrefix(ln, "\t") {
			// Indented description of the current package.
			if cur == nil {
				return nil, malformed(i, "description without a preceding package")
			}
			cur.Summary = summary(cur.Summary + " " + strings.TrimSpace(ln))
			continue
		}
		f := strings.Fields(ln)
		if len(f) < 2 {
			return nil, malformed(i, "expected \"repo/name version [flags]\"")
		}
		repo, name, ok := strings.Cut(f[0], "/")
		if !ok {
			return nil, malformed(i, "expected repo/name in first field")
		}
		repo, err := cleanField(repo, maxOrigin)
		if err != nil {
			return nil, malformed(i, "invalid repository")
		}
		name, err = cleanField(name, maxName)
		if err != nil || name == "" {
			return nil, malformed(i, "invalid package name")
		}
		version, err := cleanField(f[1], maxVersion)
		if err != nil {
			return nil, malformed(i, "invalid version")
		}
		if key := name; seen[key] {
			cur = nil // a duplicate header ends the previous description
			continue
		}
		seen[name] = true
		installed := false
		for _, flag := range f[2:] {
			if flag == "[installed]" {
				installed = true
			}
		}
		out = append(out, Package{Name: name, Version: version, Origin: repo, Installed: installed})
		cur = &out[len(out)-1]
	}
	if len(out) > maxPackages {
		return nil, ErrTooMany
	}
	return out, nil
}

// ParseUpdates parses "name old -> new" lines.
func (archAdapter) ParseUpdates(text string) ([]Update, error) {
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
		name, rest, ok := strings.Cut(ln, " ")
		if !ok {
			return nil, malformed(i, "expected \"name old -> new\"")
		}
		name, err := cleanField(name, maxName)
		if err != nil || name == "" {
			return nil, malformed(i, "invalid package name")
		}
		cur, next, ok := strings.Cut(rest, " -> ")
		if !ok {
			return nil, malformed(i, "expected \" -> \" between versions")
		}
		cur, err = cleanField(cur, maxVersion)
		if err != nil {
			return nil, malformed(i, "invalid current version")
		}
		next, err = cleanField(next, maxVersion)
		if err != nil {
			return nil, malformed(i, "invalid new version")
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, Update{Name: name, Current: cur, New: next})
	}
	if len(out) > maxPackages {
		return nil, ErrTooMany
	}
	return out, nil
}

// archVendorRepos are the official repositories. A custom [section] is
// third-party. (AUR/foreign installed packages are labelled at the UI layer,
// not in pacman.conf.) Heuristic for the fixture; refined in M4-1b.
var archVendorRepos = map[string]bool{
	"core": true, "extra": true, "multilib": true, "community": true,
	"core-testing": true, "extra-testing": true, "multilib-testing": true, "community-testing": true,
}

// ParseSources parses /etc/pacman.conf [repo] sections. [options] is skipped;
// commented sections are skipped. SigLevel "Never" means unsigned.
func (archAdapter) ParseSources(text string) ([]Source, error) {
	lns, err := lines(text)
	if err != nil {
		return nil, err
	}
	var out []Source
	seen := map[string]bool{}
	var cur *archSourceBuilder
	skip := false
	for i, ln := range lns {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
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
			name := strings.TrimSuffix(strings.TrimPrefix(ln, "["), "]")
			if name == "options" {
				cur = nil
				skip = true
				continue
			}
			skip = false
			cur = &archSourceBuilder{name: name, signed: true}
			continue
		}
		if skip {
			continue
		}
		k, v, ok := strings.Cut(ln, "=")
		if !ok || cur == nil {
			return nil, malformed(i, "expected key=value inside a [section]")
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		switch k {
		case "Server":
			cur.origin = v
		case "Include":
			if cur.origin == "" {
				cur.origin = v
			}
		case "SigLevel":
			cur.signed = !strings.Contains(v, "Never")
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

type archSourceBuilder struct {
	name   string
	origin string
	signed bool
}

func (b *archSourceBuilder) finish() (Source, error) {
	name, err := cleanField(b.name, maxName)
	if err != nil || name == "" || name == "options" {
		return Source{}, errEmpty("repository name")
	}
	origin, err := cleanField(b.origin, maxOrigin)
	if err != nil || origin == "" {
		return Source{}, errEmpty("Server or Include")
	}
	return Source{
		Name:       name,
		Origin:     origin,
		Enabled:    true,
		Signed:     b.signed,
		ThirdParty: !archVendorRepos[name],
	}, nil
}
