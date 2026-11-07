// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package pkgcenter

import "encoding/xml"

// suseAdapter parses zypper machine-readable XML (openSUSE Leap/Tumbleweed,
// SUSE Linux Enterprise).
//
// Inputs (zypper --xmlout / -x; exact flags verified in M4-1b):
//   - installed: zypper -x search --installed-only
//   - search:    zypper -x search <query>
//   - updates:   zypper -x list-updates
//   - sources:   zypper -x lr (repositories)
type suseAdapter struct{}

func (suseAdapter) Family() string { return FamilySUSE }

// suseSearchXML is the shape of "zypper -x search".
type suseSearchXML struct {
	Results []struct {
		Solvables []suseSolvable `xml:"solvable"`
	} `xml:"search-result"`
}

type suseSolvable struct {
	Status     string `xml:"status,attr"`
	Name       string `xml:"name,attr"`
	Kind       string `xml:"kind,attr"`
	Edition    string `xml:"edition,attr"`
	Arch       string `xml:"arch,attr"`
	Repository string `xml:"repository,attr"`
	Summary    string `xml:"summary,attr"`
}

// suseUpdateXML is the shape of "zypper -x list-updates".
type suseUpdateXML struct {
	Lists []struct {
		Updates []suseUpdate `xml:"update"`
	} `xml:"update-list"`
}

type suseUpdate struct {
	Name       string `xml:"name,attr"`
	Kind       string `xml:"kind,attr"`
	Edition    string `xml:"edition,attr"`
	EditionOld string `xml:"edition-old,attr"`
	Arch       string `xml:"arch,attr"`
	Repository string `xml:"repository,attr"`
}

// suseRepoXML is the shape of "zypper -x lr".
type suseRepoXML struct {
	Lists []struct {
		Repos []suseRepo `xml:"repo"`
	} `xml:"repo-list"`
}

type suseRepo struct {
	Alias    string `xml:"alias,attr"`
	Name     string `xml:"name,attr"`
	URL      string `xml:"url,attr"`
	GPGCheck string `xml:"gpgcheck,attr"`
	Enabled  string `xml:"enabled,attr"`
}

// ParseInstalled parses the solvable list; only "package" kind is kept and
// everything is marked installed (the command is --installed-only).
func (suseAdapter) ParseInstalled(text string) ([]Package, error) {
	var doc suseSearchXML
	if err := parseXML(text, &doc); err != nil {
		return nil, err
	}
	var out []Package
	seen := map[string]bool{}
	for _, r := range doc.Results {
		for _, s := range r.Solvables {
			if s.Status != "installed" {
				continue
			}
			p, ok, err := s.packageOf(true)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			key := p.Name + "\x00" + p.Arch + "\x00" + p.Version
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, p)
		}
	}
	if len(out) > maxPackages {
		return nil, ErrTooMany
	}
	return out, nil
}

// ParseSearch parses the solvable list; Installed reflects the status.
func (suseAdapter) ParseSearch(text string) ([]Package, error) {
	var doc suseSearchXML
	if err := parseXML(text, &doc); err != nil {
		return nil, err
	}
	var out []Package
	seen := map[string]bool{}
	for _, r := range doc.Results {
		for _, s := range r.Solvables {
			p, ok, err := s.packageOf(s.Status == "installed")
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			key := p.Name + "\x00" + p.Arch + "\x00" + p.Origin
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, p)
		}
	}
	if len(out) > maxPackages {
		return nil, ErrTooMany
	}
	return out, nil
}

// packageOf converts one solvable, skipping non-package kinds (patch,
// pattern, product, source).
func (s suseSolvable) packageOf(installed bool) (Package, bool, error) {
	if s.Kind != "" && s.Kind != "package" {
		return Package{}, false, nil
	}
	name, err := cleanPackageName(s.Name)
	if err != nil {
		return Package{}, false, errEmpty("name")
	}
	version, err := cleanField(s.Edition, maxVersion)
	if err != nil {
		return Package{}, false, err
	}
	arch, err := cleanField(s.Arch, maxArch)
	if err != nil {
		return Package{}, false, err
	}
	repo, err := cleanField(s.Repository, maxOrigin)
	if err != nil {
		return Package{}, false, err
	}
	return Package{
		Name:      name,
		Version:   version,
		Arch:      arch,
		Origin:    repo,
		Installed: installed,
		Summary:   summary(s.Summary),
	}, true, nil
}

// ParseUpdates parses "zypper -x list-updates" <update> elements.
func (suseAdapter) ParseUpdates(text string) ([]Update, error) {
	var doc suseUpdateXML
	if err := parseXML(text, &doc); err != nil {
		return nil, err
	}
	var out []Update
	seen := map[string]bool{}
	for _, l := range doc.Lists {
		for _, u := range l.Updates {
			if u.Kind != "" && u.Kind != "package" {
				continue
			}
			name, err := cleanPackageName(u.Name)
			if err != nil {
				return nil, errEmpty("name")
			}
			cur, err := cleanField(u.EditionOld, maxVersion)
			if err != nil {
				return nil, err
			}
			next, err := cleanField(u.Edition, maxVersion)
			if err != nil {
				return nil, err
			}
			arch, err := cleanField(u.Arch, maxArch)
			if err != nil {
				return nil, err
			}
			repo, err := cleanField(u.Repository, maxOrigin)
			if err != nil {
				return nil, err
			}
			if seen[name+"\x00"+arch] {
				continue
			}
			seen[name+"\x00"+arch] = true
			out = append(out, Update{Name: name, Arch: arch, Origin: repo, Current: cur, New: next})
		}
	}
	if len(out) > maxPackages {
		return nil, ErrTooMany
	}
	return out, nil
}

// suseVendorHosts are the openSUSE/SLE vendor archives. Anything else is
// third-party. Heuristic for the fixture; refined in M4-1b.
var suseVendorHosts = []string{"opensuse.org", "suse.com"}

// ParseSources parses "zypper -x lr" <repo> elements.
func (suseAdapter) ParseSources(text string) ([]Source, error) {
	var doc suseRepoXML
	if err := parseXML(text, &doc); err != nil {
		return nil, err
	}
	var out []Source
	seen := map[string]bool{}
	for _, l := range doc.Lists {
		for _, r := range l.Repos {
			name, err := cleanPackageName(r.Alias)
			if err != nil {
				return nil, errEmpty("alias")
			}
			origin, err := cleanField(r.URL, maxOrigin)
			if err != nil {
				return nil, err
			}
			key := name + "\x00" + origin
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, Source{
				Name:       name,
				Origin:     origin,
				Enabled:    r.Enabled == "1" || r.Enabled == "true",
				Signed:     truthy(r.GPGCheck),
				ThirdParty: !vendorHost(hostnameOf(origin), suseVendorHosts),
			})
		}
	}
	if len(out) > maxSources {
		return nil, ErrTooMany
	}
	return out, nil
}

// parseXML unmarshals text with the input size cap enforced.
func parseXML(text string, v any) error {
	if len(text) > maxInputBytes {
		return ErrTooLarge
	}
	if err := xml.Unmarshal([]byte(text), v); err != nil {
		return malformedf("XML: %v", err)
	}
	return nil
}
