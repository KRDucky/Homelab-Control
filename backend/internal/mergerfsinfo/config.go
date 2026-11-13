// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsinfo

import (
	"context"
	"io"
	"os"
	"sort"
	"strings"
	"syscall"
	"unicode/utf8"
)

// ConfiguredMount is one mergerfs pool declared in fstab or a systemd .mount
// unit. It is a read-only import: Collect never writes fstab or unit files
// and never mounts or unmounts.
type ConfiguredMount struct {
	Origin     string   `json:"origin"`
	Path       string   `json:"path"`
	Unit       string   `json:"unit,omitempty"`
	Mountpoint string   `json:"mountpoint"`
	Branches   []string `json:"branches"`
	Options    []string `json:"options"`
	Mounted    bool     `json:"mounted"`
}

const (
	originFstab         = "fstab"
	originSystemd       = "systemd"
	fstabDisplayPath    = "/etc/fstab"
	systemdDirDisplay   = "/etc/systemd/system"
	fstabRel            = "etc/fstab"
	systemdDirRel       = "etc/systemd/system"
	fstabMaxBytes       = 256 << 10
	unitMaxBytes        = 64 << 10
	configuredMax       = 64
	systemdDirMaxNames  = 512
	configuredFieldMax  = 256
	configuredScanEvery = 32
)

// collectConfigured reads bounded /etc/fstab and /etc/systemd/system/*.mount
// beneath osRoot. Missing files are normal. Symlinks, oversize files, and
// non-regular files are skipped. Includes and drop-ins are never followed.
// Listed What/Where paths are never opened.
func collectConfigured(ctx context.Context, osRoot string, live []Pool) ([]ConfiguredMount, bool) {
	out := []ConfiguredMount{}
	if osRoot == "" {
		osRoot = "/"
	}
	root, err := os.OpenRoot(osRoot)
	if err != nil {
		return out, false
	}
	defer root.Close()

	liveSet := make(map[string]struct{}, len(live))
	for _, p := range live {
		liveSet[cleanMountpoint(p.Mountpoint)] = struct{}{}
	}

	truncated := false
	out, truncated = appendFstab(ctx, root, out, liveSet, truncated)
	out, truncated = appendSystemd(ctx, root, out, liveSet, truncated)
	return out, truncated
}

func markMounted(c ConfiguredMount, liveSet map[string]struct{}) ConfiguredMount {
	_, c.Mounted = liveSet[c.Mountpoint]
	return c
}

func appendFstab(ctx context.Context, root *os.Root, out []ConfiguredMount, liveSet map[string]struct{}, truncated bool) ([]ConfiguredMount, bool) {
	if truncated || len(out) >= configuredMax {
		return out, true
	}
	if ctx.Err() != nil {
		return out, truncated
	}
	f, err := root.OpenFile(fstabRel, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return out, truncated
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return out, truncated
	}
	b, err := io.ReadAll(io.LimitReader(f, fstabMaxBytes+1))
	if err != nil || len(b) > fstabMaxBytes {
		return out, truncated
	}
	for i, line := range strings.Split(string(b), "\n") {
		if i%configuredScanEvery == 0 && ctx.Err() != nil {
			return out, truncated
		}
		c, ok := parseFstabLine(line)
		if !ok {
			continue
		}
		if len(out) >= configuredMax {
			return out, true
		}
		out = append(out, markMounted(c, liveSet))
	}
	return out, truncated
}

func parseFstabLine(line string) (ConfiguredMount, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return ConfiguredMount{}, false
	}
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return ConfiguredMount{}, false
	}
	typ, err := unescape(fields[2])
	if err != nil || !isMergerFSType(typ) {
		return ConfiguredMount{}, false
	}
	spec, err := unescape(fields[0])
	if err != nil {
		return ConfiguredMount{}, false
	}
	file, err := unescape(fields[1])
	if err != nil {
		return ConfiguredMount{}, false
	}
	file = cleanMountpoint(file)
	if !fieldOK(file) || !strings.HasPrefix(file, "/") {
		return ConfiguredMount{}, false
	}
	branches, ok := parseBranches(spec)
	if !ok || !branchesOK(branches) {
		return ConfiguredMount{}, false
	}
	opts := []string{}
	if len(fields) >= 4 {
		opts = parseOptions(fields[3])
	}
	return ConfiguredMount{
		Origin:     originFstab,
		Path:       fstabDisplayPath,
		Mountpoint: file,
		Branches:   branches,
		Options:    opts,
	}, true
}

func appendSystemd(ctx context.Context, root *os.Root, out []ConfiguredMount, liveSet map[string]struct{}, truncated bool) ([]ConfiguredMount, bool) {
	if truncated || len(out) >= configuredMax {
		return out, true
	}
	if ctx.Err() != nil {
		return out, truncated
	}
	d, err := root.OpenFile(systemdDirRel, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_DIRECTORY, 0)
	if err != nil {
		return out, truncated
	}
	defer d.Close()
	names, err := d.Readdirnames(systemdDirMaxNames + 1)
	if err != nil {
		return out, truncated
	}
	if len(names) > systemdDirMaxNames {
		names = names[:systemdDirMaxNames]
		truncated = true
	}
	mounts := make([]string, 0, len(names))
	for _, name := range names {
		if validMountUnitName(name) {
			mounts = append(mounts, name)
		}
	}
	sort.Strings(mounts)
	for i, name := range mounts {
		if i%configuredScanEvery == 0 && ctx.Err() != nil {
			return out, truncated
		}
		if len(out) >= configuredMax {
			return out, true
		}
		c, ok := readSystemdUnit(root, name)
		if !ok {
			continue
		}
		out = append(out, markMounted(c, liveSet))
	}
	return out, truncated
}

func validMountUnitName(name string) bool {
	if !strings.HasSuffix(name, ".mount") || name == ".mount" || strings.Contains(name, "@") {
		return false
	}
	if len(name) > 255 || strings.ContainsAny(name, "/\x00") || strings.Contains(name, "..") {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == ':' || r == '-' || r == '_' || r == '.' || r == '\\' {
			continue
		}
		return false
	}
	return true
}

func readSystemdUnit(root *os.Root, name string) (ConfiguredMount, bool) {
	rel := systemdDirRel + "/" + name
	f, err := root.OpenFile(rel, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return ConfiguredMount{}, false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return ConfiguredMount{}, false
	}
	b, err := io.ReadAll(io.LimitReader(f, unitMaxBytes+1))
	if err != nil || len(b) > unitMaxBytes {
		return ConfiguredMount{}, false
	}
	what, where, typ, options, ok := parseSystemdMount(string(b))
	if !ok || !isMergerFSType(typ) {
		return ConfiguredMount{}, false
	}
	where = cleanMountpoint(where)
	if !fieldOK(where) || !strings.HasPrefix(where, "/") || strings.ContainsAny(where, "%") {
		return ConfiguredMount{}, false
	}
	branches, ok := parseBranches(what)
	if !ok || !branchesOK(branches) {
		return ConfiguredMount{}, false
	}
	return ConfiguredMount{
		Origin:     originSystemd,
		Path:       systemdDirDisplay + "/" + name,
		Unit:       name,
		Mountpoint: where,
		Branches:   branches,
		Options:    parseOptions(options),
	}, true
}

func parseSystemdMount(text string) (what, where, typ, options string, ok bool) {
	section := ""
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, ".include") {
			return "", "", "", "", false
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(line)
			continue
		}
		if section != "[mount]" {
			continue
		}
		key, val, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = unquote(strings.TrimSpace(val))
		switch key {
		case "what":
			what = val
		case "where":
			where = val
		case "type":
			typ = val
		case "options":
			options = val
		}
	}
	if what == "" || where == "" || typ == "" {
		return "", "", "", "", false
	}
	return what, where, typ, options, true
}

func isMergerFSType(typ string) bool {
	return typ == "fuse.mergerfs" || typ == "mergerfs"
}

func cleanMountpoint(p string) string {
	if p == "/" || p == "" {
		return p
	}
	return strings.TrimRight(p, "/")
}

func fieldOK(s string) bool {
	return s != "" && len(s) <= configuredFieldMax && utf8.ValidString(s)
}

func branchesOK(branches []string) bool {
	for _, b := range branches {
		if !fieldOK(b) {
			return false
		}
	}
	return len(branches) > 0
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
