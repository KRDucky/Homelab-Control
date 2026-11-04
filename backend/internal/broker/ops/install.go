// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Reading a unit's [Install] section (M2-3, docs/m2/M2-3-DESIGN-REVIEW.md
// §3.5). systemd exposes no API for it, and drop-ins (which systemd honours
// for [Install]) may be root-only and contain secrets, so the broker reads the
// fragment and drop-ins in-process as root and keeps ONLY the [Install]
// directives whose values are unit names. File content never leaves this
// file; the token digest covers paths and parsed directives, not content.

// installDirs are the directories a fragment may come from. Runtime (/run)
// and generator directories are not listed: generated, transient and
// runtime units are refused for enablement.
var fragmentDirs = []string{"/etc/systemd/system/", "/usr/lib/systemd/system/", "/lib/systemd/system/", "/usr/local/lib/systemd/system/"}

// dropInDirs additionally allow systemd's runtime and control directories:
// drop-ins there are read (their [Install] would be honoured), never written.
var dropInDirs = append(append([]string{}, fragmentDirs...),
	"/run/systemd/system/", "/etc/systemd/system.control/", "/run/systemd/system.control/")

const maxUnitFileBytes = 64 << 10

// maxDropIns bounds the drop-ins read for one unit.
const maxDropIns = 32

// InstallInfo is the parsed [Install] section of a unit (fragment plus
// drop-ins, in systemd's order).
type InstallInfo struct {
	WantedBy        []string `json:"wanted_by,omitempty"`
	RequiredBy      []string `json:"required_by,omitempty"`
	UpheldBy        []string `json:"upheld_by,omitempty"`
	Alias           []string `json:"alias,omitempty"`
	Also            []string `json:"also,omitempty"`
	DefaultInstance []string `json:"default_instance,omitempty"`
}

// Digest identifies the install information together with where it came from.
func (i InstallInfo) Digest(fragment string, dropIns []string) string {
	b, _ := json.Marshal(struct {
		F string
		D []string
		I InstallInfo
	}{fragment, dropIns, i})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:12])
}

// errUnsupported marks an [Install] construct M2-3 refuses rather than
// interpreting (the message is user-facing).
type errUnsupported struct{ msg string }

func (e errUnsupported) Error() string { return e.msg }

func unsupported(format string, a ...any) error { return errUnsupported{fmt.Sprintf(format, a...)} }

// IsUnsupported reports whether err is a refusal of an unsupported construct.
func IsUnsupported(err error) bool {
	var u errUnsupported
	return errors.As(err, &u)
}

// ParseInstall parses one unit file and appends its [Install] directives to
// into. Specifiers, line continuations, empty (reset) assignments, unknown
// keys and values that are not plain unit names are refused, not guessed.
func ParseInstall(b []byte, into *InstallInfo) error {
	section := ""
	for n, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return unsupported("line %d: malformed section header", n+1)
			}
			section = line[1 : len(line)-1]
			continue
		}
		if section != "Install" {
			continue // other sections are never interpreted (or kept)
		}
		if strings.HasSuffix(line, "\\") {
			return unsupported("the [Install] section uses a line continuation (line %d)", n+1)
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return unsupported("line %d of the [Install] section is not a key=value assignment", n+1)
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		var dst *[]string
		switch k {
		case "WantedBy":
			dst = &into.WantedBy
		case "RequiredBy":
			dst = &into.RequiredBy
		case "UpheldBy":
			dst = &into.UpheldBy
		case "Alias":
			dst = &into.Alias
		case "Also":
			dst = &into.Also
		case "DefaultInstance":
			dst = &into.DefaultInstance
		default:
			return unsupported("the [Install] section contains %q, which is not supported here", clean(k, 40))
		}
		if v == "" {
			return unsupported("the [Install] section resets %s with an empty assignment", k)
		}
		if strings.ContainsAny(v, "%\\\"'") {
			return unsupported("%s= uses specifiers, escapes or quotes, which are not supported here", k)
		}
		for _, f := range strings.Fields(v) {
			if k == "DefaultInstance" {
				*dst = append(*dst, clean(f, 100))
				continue
			}
			if !inspectable(f) {
				return unsupported("%s= names %q, which is not a plain unit name", k, clean(f, 120))
			}
			if len(*dst) < 64 && !contains(*dst, f) {
				*dst = append(*dst, f)
			}
		}
	}
	return nil
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// safeUnitPath checks that p is an absolute, clean path below one of dirs
// whose components (other than a merged-usr /lib link) are not symlinks.
func safeUnitPath(root Roots, p string, dirs []string, suffix string) error {
	if p == "" || !filepath.IsAbs(p) || filepath.Clean(p) != p || strings.Contains(p, "..") || !strings.HasSuffix(p, suffix) {
		return unsupported("%q is not a unit file path this version reads", clean(p, 200))
	}
	in := false
	for _, d := range dirs {
		if strings.HasPrefix(p, d) && len(p) > len(d) {
			in = true
		}
	}
	if !in {
		return unsupported("the unit is defined in %q, outside the unit directories this version manages", clean(filepath.Dir(p), 200))
	}
	cur := ""
	for i, part := range strings.Split(strings.TrimPrefix(p, "/"), "/") {
		cur += "/" + part
		fi, err := os.Lstat(root.Path(cur))
		if err != nil {
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			if i == 0 && cur == "/lib" {
				if t, err := os.Readlink(root.Path(cur)); err == nil && (t == "usr/lib" || t == "/usr/lib") {
					continue
				}
			}
			return unsupported("%q is reached through a symbolic link, which this version does not follow", clean(cur, 200))
		}
	}
	return nil
}

// ReadInstall reads the fragment and the drop-ins (as root, in-process) and
// returns their combined [Install] section.
func ReadInstall(root Roots, fragment string, dropIns []string) (InstallInfo, error) {
	var info InstallInfo
	if len(dropIns) > maxDropIns {
		return info, unsupported("the unit has more than %d drop-in files", maxDropIns)
	}
	if err := safeUnitPath(root, fragment, fragmentDirs, ".service"); err != nil {
		return info, err
	}
	files := append([]string{fragment}, dropIns...)
	for i, f := range files {
		if i > 0 {
			if err := safeUnitPath(root, f, dropInDirs, ".conf"); err != nil {
				return info, err
			}
		}
		b, err := readFixed(root.Path(f), maxUnitFileBytes)
		if err != nil {
			if IsUnsupported(err) {
				return info, err
			}
			return info, unsupported("a unit file could not be read safely (%s)", clean(filepath.Base(f), 120))
		}
		if err := ParseInstall(b, &info); err != nil {
			return info, err
		}
	}
	for _, l := range [][]string{info.WantedBy, info.Alias} {
		sort.Strings(l)
	}
	return info, nil
}
