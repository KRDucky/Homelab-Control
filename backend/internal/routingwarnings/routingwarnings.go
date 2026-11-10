// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

// Package routingwarnings consolidates per-router warnings of a Compose routing
// preview before CORE returns it.
//
// composediscovery.Build reports one identical warning for every live Traefik
// router without a rule (host_unverified) or with a rule it cannot compare
// (complex_rule). This package replaces each repeated group with one warning
// that names the affected routers. It lives outside composediscovery on
// purpose: hc-docker-metadata links composediscovery, and editing that package
// changes the reviewed broker binary. Only hc-api links this package.
package routingwarnings

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"homelab-control/backend/internal/composediscovery"
	"homelab-control/backend/internal/traefikclient"
)

const (
	CodeHostUnverified = "host_unverified"
	CodeComplexRule    = "complex_rule"
	// MaxNamedRouters bounds how many router names one warning lists.
	MaxNamedRouters = 5
)

// singleHost mirrors the single-host rule composediscovery.Build compares.
var singleHost = regexp.MustCompile("^Host\\(`([a-z0-9.-]+)`\\)$")

// Consolidate returns warnings with each host_unverified and complex_rule group
// reduced to one entry at the position of its first occurrence, naming the
// routers from inventory. Other warnings keep their order; exact duplicates of
// any other (code, message) pair are also dropped. The input is not modified.
func Consolidate(warnings []composediscovery.Issue, inventory traefikclient.Inventory) []composediscovery.Issue {
	var ruleless, complexRules []string
	for _, r := range inventory.Routers {
		switch {
		case r.Rule == "":
			ruleless = append(ruleless, r.Name)
		case !singleHost.MatchString(r.Rule):
			complexRules = append(complexRules, r.Name)
		}
	}
	out := make([]composediscovery.Issue, 0, len(warnings))
	seen := map[composediscovery.Issue]bool{}
	grouped := map[string]bool{}
	for _, w := range warnings {
		switch w.Code {
		case CodeHostUnverified, CodeComplexRule:
			if grouped[w.Code] {
				continue
			}
			grouped[w.Code] = true
			out = append(out, group(w, ruleless, complexRules))
		default:
			if seen[w] {
				continue
			}
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}

func group(w composediscovery.Issue, ruleless, complexRules []string) composediscovery.Issue {
	if w.Code == CodeHostUnverified && len(ruleless) > 0 {
		w.Message = List(ruleless, "lacks a routing rule", "lack routing rules") + "; hostname conflicts cannot be fully verified."
	}
	if w.Code == CodeComplexRule && len(complexRules) > 0 {
		w.Message = List(complexRules, "has a routing rule", "have routing rules") + " that this single-host model cannot compare; review conflicts manually."
	}
	// Without matching inventory names the original message is kept once.
	return w
}

// List renders "1 live router (a@file) <one>" or "N live routers (a@file,
// b@file and 3 more) <many>", with names sorted and deduplicated.
func List(names []string, one, many string) string {
	names = slices.Clone(names)
	slices.Sort(names)
	names = slices.Compact(names)
	shown := names
	if len(shown) > MaxNamedRouters {
		shown = shown[:MaxNamedRouters]
	}
	list := strings.Join(shown, ", ")
	if extra := len(names) - len(shown); extra > 0 {
		list += fmt.Sprintf(" and %d more", extra)
	}
	if len(names) == 1 {
		return fmt.Sprintf("1 live router (%s) %s", list, one)
	}
	return fmt.Sprintf("%d live routers (%s) %s", len(names), list, many)
}
