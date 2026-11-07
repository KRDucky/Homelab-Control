// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	. "homelab-control/backend/internal/broker/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/runner"
	"homelab-control/backend/internal/pkgcenter"
)

// M4-1b read-only package queries (ADR 0008 Amendment 5, Proposed). Every op
// runs unprivileged with a closed argv template, cache-only (no network, no
// metadata refresh), bounded output and a replaced environment. Unknown
// families and missing tools fail closed with an explicit reason, never an
// empty success.

type packagesArgs struct {
	Family string `json:"family"`
	Q      string `json:"q,omitempty"`
}

func decodePackagesArgs(raw json.RawMessage, wantQuery bool) (packagesArgs, error) {
	var a packagesArgs
	if err := proto.DecodeArgs(raw, &a); err != nil {
		return a, &proto.Error{Code: proto.CodeBadRequest, Message: "arguments must be {\"family\": string" + map[bool]string{true: ", \"q\": string}", false: "}"}[wantQuery]}
	}
	if a.Family == "" {
		return a, &proto.Error{Code: proto.CodeBadRequest, Message: "family is required"}
	}
	if _, ok := pkgcenter.ForFamily(a.Family); !ok {
		return a, &proto.Error{Code: proto.CodeUnavailable, Message: "unsupported package family: " + clean(a.Family, 40)}
	}
	if wantQuery {
		if a.Q == "" || !runner.ValidPackageQuery(a.Q) {
			return a, &proto.Error{Code: proto.CodeBadRequest, Message: "query must be 1-64 characters of letters, digits, + . _ or - and must not start with -"}
		}
	} else if a.Q != "" {
		return a, &proto.Error{Code: proto.CodeBadRequest, Message: "this operation takes no query"}
	}
	return a, nil
}

// familyQuery maps (family, kind) to a closed runner.PackageQuery template.
func familyQuery(family, kind string) (runner.PackageQuery, bool) {
	m := map[string]map[string]runner.PackageQuery{
		pkgcenter.FamilyDebian: {"installed": runner.PkgDebianInstalled, "updates": runner.PkgDebianUpdates, "search": runner.PkgDebianSearch},
		pkgcenter.FamilyFedora: {"installed": runner.PkgFedoraInstalled, "updates": runner.PkgFedoraUpdates, "search": runner.PkgFedoraSearch},
		pkgcenter.FamilySUSE:   {"installed": runner.PkgSUSEInstalled, "updates": runner.PkgSUSEUpdates, "search": runner.PkgSUSESearch},
		pkgcenter.FamilyArch:   {"installed": runner.PkgArchInstalled, "updates": runner.PkgArchUpdates, "search": runner.PkgArchSearch},
	}
	k, ok := m[family]
	if !ok {
		return "", false
	}
	q, ok := k[kind]
	return q, ok
}

// cachePath returns the per-family local metadata cache directory (the source
// of the "as of last refresh" label). Empty when the family has no such path.
func cachePath(family string) string {
	switch family {
	case pkgcenter.FamilyDebian:
		return "/var/lib/apt/lists"
	case pkgcenter.FamilyFedora:
		return "/var/cache/dnf"
	case pkgcenter.FamilySUSE:
		return "/var/cache/zypp"
	case pkgcenter.FamilyArch:
		return "/var/lib/pacman/sync"
	default:
		return ""
	}
}

// cacheMtime returns the local cache mtime as RFC3339, or "" when absent.
func cacheMtime(env *Env, family string) string {
	p := cachePath(family)
	if p == "" {
		return ""
	}
	if fi, err := os.Stat(env.Roots.Path(p)); err == nil {
		return fi.ModTime().UTC().Format(time.RFC3339)
	}
	return ""
}

func runPackageErr(err error, binary string) error {
	switch {
	case errors.Is(err, runner.ErrTimeout):
		return &proto.Error{Code: proto.CodeTimeout, Message: binary + " did not answer in time"}
	case errors.Is(err, runner.ErrTooLarge):
		return &proto.Error{Code: proto.CodeInternal, Message: binary + " output exceeded the size limit"}
	case err != nil:
		msg := binary + " could not be run"
		var ex *runner.ExecError
		if errors.As(err, &ex) && ex.Class != "" {
			msg += " (" + ex.Class + ")"
		}
		return &proto.Error{Code: proto.CodeUnavailable, Message: msg}
	default:
		return nil
	}
}

func runPackageQuery(ctx context.Context, env *Env, q runner.PackageQuery, query, binary string) (runner.Result, error) {
	if env.Runner == nil || !env.Runner.Available(binary) {
		return runner.Result{}, &proto.Error{Code: proto.CodeUnavailable, Message: binary + " is not installed; this host does not provide a usable package manager"}
	}
	res, err := env.Runner.RunPackageQuery(ctx, q, query)
	if err != nil {
		return runner.Result{}, runPackageErr(err, binary)
	}
	if res.ExitCode != 0 {
		return runner.Result{}, &proto.Error{Code: proto.CodeUnavailable, Message: binary + " failed: " + clean(res.Stderr, 200)}
	}
	return res, nil
}

func packagesInstalled(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	a, err := decodePackagesArgs(raw, false)
	if err != nil {
		return nil, err
	}
	adapter, _ := pkgcenter.ForFamily(a.Family)
	q, _ := familyQuery(a.Family, "installed")
	res, err := runPackageQuery(ctx, env, q, "", packageBinary(q))
	if err != nil {
		return nil, err
	}
	pkgs, err := adapter.ParseInstalled(string(res.Stdout))
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeInternal, Message: "unexpected package-manager output: " + err.Error()}
	}
	return PackageList{Packages: pkgs, Truncated: len(pkgs) >= pkgcenterMaxPackages, Source: sourceLabel(q)}, nil
}

func packagesUpdates(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	a, err := decodePackagesArgs(raw, false)
	if err != nil {
		return nil, err
	}
	if cacheMtime(env, a.Family) == "" {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "package metadata cache is not available; refresh it first"}
	}
	adapter, _ := pkgcenter.ForFamily(a.Family)
	q, _ := familyQuery(a.Family, "updates")
	res, err := runPackageQuery(ctx, env, q, "", packageBinary(q))
	if err != nil {
		return nil, err
	}
	updates, err := adapter.ParseUpdates(string(res.Stdout))
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeInternal, Message: "unexpected package-manager output: " + err.Error()}
	}
	return UpdateList{Updates: updates, Truncated: len(updates) >= pkgcenterMaxPackages, Source: sourceLabel(q), AsOf: cacheMtime(env, a.Family)}, nil
}

func packagesSearch(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	a, err := decodePackagesArgs(raw, true)
	if err != nil {
		return nil, err
	}
	adapter, _ := pkgcenter.ForFamily(a.Family)
	q, _ := familyQuery(a.Family, "search")
	res, err := runPackageQuery(ctx, env, q, a.Q, packageBinary(q))
	if err != nil {
		return nil, err
	}
	pkgs, err := adapter.ParseSearch(string(res.Stdout))
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeInternal, Message: "unexpected package-manager output: " + err.Error()}
	}
	return PackageList{Packages: pkgs, Truncated: len(pkgs) >= pkgcenterMaxPackages, Source: sourceLabel(q)}, nil
}

// readSourcesText reads fixed paths and globs under env.Roots and joins them.
// Missing files are skipped; unreadable files fail the op (never a silent
// empty list).
func readSourcesText(env *Env, fixed []string, globs []string) (string, error) {
	var b strings.Builder
	for _, p := range fixed {
		data, err := readFixed(env.Roots.Path(p), 4<<20)
		if err == nil {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.Write(data)
		} else if !isNotExist(err) {
			return "", err
		}
	}
	for _, g := range globs {
		matches, err := filepath.Glob(env.Roots.Path(g))
		if err != nil {
			return "", err
		}
		sort.Strings(matches)
		for _, m := range matches {
			data, err := readFixed(m, 4<<20)
			if err == nil {
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
				b.Write(data)
			} else if !isNotExist(err) {
				return "", err
			}
		}
	}
	return b.String(), nil
}

func packagesSources(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	a, err := decodePackagesArgs(raw, false)
	if err != nil {
		return nil, err
	}
	adapter, _ := pkgcenter.ForFamily(a.Family)
	var text, label string
	switch a.Family {
	case pkgcenter.FamilySUSE:
		q, _ := familyQuery(a.Family, "sources")
		res, err := runPackageQuery(ctx, env, q, "", packageBinary(q))
		if err != nil {
			return nil, err
		}
		text, label = string(res.Stdout), sourceLabel(q)
	default:
		var fixed, globs []string
		switch a.Family {
		case pkgcenter.FamilyDebian:
			fixed, globs = []string{"/etc/apt/sources.list"}, []string{"/etc/apt/sources.list.d/*"}
		case pkgcenter.FamilyFedora:
			globs = []string{"/etc/yum.repos.d/*.repo"}
		case pkgcenter.FamilyArch:
			fixed = []string{"/etc/pacman.conf"}
		}
		text, err = readSourcesText(env, fixed, globs)
		if err != nil {
			return nil, &proto.Error{Code: proto.CodeInternal, Message: "could not read configured sources: " + err.Error()}
		}
		label = sourceFileLabel(a.Family)
	}
	sources, err := adapter.ParseSources(text)
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeInternal, Message: "unexpected source format: " + err.Error()}
	}
	return SourceList{Sources: sources, Truncated: len(sources) >= pkgcenterMaxSources, Source: label}, nil
}

func packageBinary(q runner.PackageQuery) string {
	switch q {
	case runner.PkgDebianInstalled:
		return "dpkg-query"
	case runner.PkgDebianSearch:
		return "apt-cache"
	case runner.PkgDebianUpdates:
		return "apt"
	case runner.PkgFedoraInstalled:
		return "rpm"
	case runner.PkgFedoraSearch, runner.PkgFedoraUpdates:
		return "dnf"
	case runner.PkgSUSEInstalled, runner.PkgSUSESearch, runner.PkgSUSEUpdates, runner.PkgSUSESources:
		return "zypper"
	default:
		return "pacman"
	}
}

func sourceLabel(q runner.PackageQuery) string {
	switch q {
	case runner.PkgDebianInstalled:
		return "dpkg-query -W (run as homelab-control)"
	case runner.PkgDebianSearch:
		return "apt-cache search (run as homelab-control, local cache)"
	case runner.PkgDebianUpdates:
		return "apt list --upgradable (run as homelab-control, local cache)"
	case runner.PkgFedoraInstalled:
		return "rpm -qa (run as homelab-control)"
	case runner.PkgFedoraSearch:
		return "dnf -C repoquery (run as homelab-control, cache-only)"
	case runner.PkgFedoraUpdates:
		return "dnf -C check-update (run as homelab-control, cache-only)"
	case runner.PkgSUSEInstalled:
		return "zypper --no-refresh -x search --installed-only (run as homelab-control)"
	case runner.PkgSUSESearch:
		return "zypper --no-refresh -x search (run as homelab-control, cache-only)"
	case runner.PkgSUSEUpdates:
		return "zypper --no-refresh -x list-updates (run as homelab-control, cache-only)"
	case runner.PkgSUSESources:
		return "zypper --no-refresh -x lr (run as homelab-control)"
	case runner.PkgArchInstalled:
		return "pacman -Q (run as homelab-control)"
	case runner.PkgArchSearch:
		return "pacman -Ss (run as homelab-control, local cache)"
	case runner.PkgArchUpdates:
		return "pacman -Qu (run as homelab-control, local cache)"
	default:
		return "package manager (run as homelab-control)"
	}
}

func sourceFileLabel(family string) string {
	switch family {
	case pkgcenter.FamilyDebian:
		return "/etc/apt/sources.list(.d/*)"
	case pkgcenter.FamilyFedora:
		return "/etc/yum.repos.d/*.repo"
	case pkgcenter.FamilyArch:
		return "/etc/pacman.conf"
	default:
		return "configured source files"
	}
}

// packagesAuditArgs summarises the search op for the broker audit record:
// family and query length only, never the query text (ADR 0008 Amendment 5).
func packagesAuditArgs(raw json.RawMessage) string {
	var a packagesArgs
	if proto.DecodeArgs(raw, &a) != nil {
		return "invalid args"
	}
	return fmt.Sprintf("family=%s q_len=%d", clean(a.Family, 16), len(a.Q))
}

// Bounds mirror pkgcenter; these are duplicated to keep the op package free of
// the parser's unexported constants.
const (
	pkgcenterMaxPackages = 65536
	pkgcenterMaxSources  = 4096
)
