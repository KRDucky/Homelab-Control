// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

package traefikclient

import (
	"regexp"
	"strings"
)

// Matches the Traefik helper sanitizer: Host, Path, and PathPrefix only,
// joined by && / ||, never Headers, HostRegexp, Query, or "..".
var (
	ruleMatcher = regexp.MustCompile("^(?:Host\\(`[a-z0-9.-]{1,253}`\\)|Path(?:Prefix)?\\(`/[A-Za-z0-9._~/-]{0,200}`\\))$")
	ruleOp      = regexp.MustCompile(` (\|\||&&) `)
	hostCapture = regexp.MustCompile("Host\\(`([a-z0-9.-]{1,253})`\\)")
	serviceID   = regexp.MustCompile(`^[a-zA-Z0-9_.@-]{0,280}$`)
)

// SanitizeRule keeps helper-safe Host/Path/PathPrefix rules. Invalid rules
// become empty so inventory still loads.
func SanitizeRule(rule string) string {
	if len(rule) < 8 || len(rule) > 512 || strings.Contains(rule, "..") {
		return ""
	}
	parts := ruleOp.Split(rule, -1)
	ops := ruleOp.FindAllString(rule, -1)
	if len(parts) < 1 || len(parts) > 5 || len(ops) != len(parts)-1 {
		return ""
	}
	if n := len(parts) + len(ops); n > 9 || n%2 == 0 {
		return ""
	}
	for _, p := range parts {
		if !ruleMatcher.MatchString(p) {
			return ""
		}
	}
	for _, o := range ops {
		if o != " && " && o != " || " {
			return ""
		}
	}
	return rule
}

// ComparableHosts extracts Host() names from a sanitized rule. Path-only
// rules return nil, so hostname conflicts stay unverified.
func ComparableHosts(rule string) []string {
	if SanitizeRule(rule) == "" {
		return nil
	}
	matches := hostCapture.FindAllStringSubmatch(rule, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}
