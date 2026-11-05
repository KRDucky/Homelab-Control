// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	. "homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/servicepolicy"
)

// Unreloaded unit-file changes (PR #36 review, probe 8,
// docs/m2/M2-3-DESIGN-REVIEW.md §3.4 and §8).
//
// The boot-time checks of an enable (the start walk) use systemd's loaded
// dependency graph. A unit file or drop-in written, edited or removed on disk
// without a daemon-reload — the unit's own, a shared prefix/type-level
// drop-in, or ANOTHER unit's (for example a protected unit gaining
// Conflicts= on this one) — is not in that graph, but is used at the next
// boot or reload. Measured on systemd 249, 255 and 259: each such change let
// an enabled fixture stop a stand-in protected service at boot.
//
// The review cannot evaluate those changes from disk in general (the whole
// dependency graph would have to be rebuilt), so an enable FAILS CLOSED when
// anything in the unit directories changed after systemd last loaded them.
// Changes that the operation journal attributes to Homelab Control's own
// completed enablement changes (the exact link paths and their directories,
// with change times inside that operation's window) are not counted.

// changeScanDirs are the unit directories checked for changes. Not checked:
// /etc|/run/systemd/system.control (written and applied by PID 1 itself,
// set-property), /run/systemd/transient (loaded by PID 1 when created), the
// generator directories (rewritten by PID 1 at every boot and reload).
var changeScanDirs = []string{
	"/etc/systemd/system/", "/etc/systemd/system.attached/", "/usr/local/lib/systemd/system/",
	"/usr/lib/systemd/system/", "/lib/systemd/system/",
}

// runtimeUnitDirs hold runtime unit configuration (tmpfs): loaded now but
// gone at the next boot, so the boot-time effect of an enable cannot be
// reviewed while any exists. (The directories themselves are created during
// boot, so their own change times are not used; probe 9.)
var runtimeUnitDirs = []string{"/run/systemd/system/", "/run/systemd/system.attached/"}

// generatorDir is recreated by PID 1 at the start of every reload, before
// units are loaded (probe 8b, systemd 249/255/259).
const generatorDir = "/run/systemd/generator"

// ManagerLoad is the manager's record of when it last loaded unit files.
type ManagerLoad struct {
	// Boot load (UnitsLoadStartTimestamp, set once per boot).
	BootRealtime  time.Time
	BootMonotonic time.Duration
	// Last reload (UnitsLoadTimestamp, systemd >= 255; zero before the
	// first reload). HasReloadStamp is false on systemd < 255, which has no
	// such property.
	HasReloadStamp  bool
	ReloadRealtime  time.Time
	ReloadMonotonic time.Duration
}

// ParseManagerShow parses runner.ManagerShowArgs output.
func ParseManagerShow(b []byte) (ManagerLoad, error) {
	var m ManagerLoad
	seen := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(strings.TrimRight(line, "\r"), "=")
		if !ok {
			continue
		}
		if seen[k] {
			return m, errors.New("repeated manager property " + clean(k, 40))
		}
		seen[k] = true
		v = strings.TrimSpace(v)
		switch k {
		case "UnitsLoadStartTimestamp":
			m.BootRealtime, _ = parseUTCus(v)
		case "UnitsLoadStartTimestampMonotonic":
			n, _ := strconv.ParseUint(v, 10, 63)
			m.BootMonotonic = time.Duration(n) * time.Microsecond
		case "UnitsLoadTimestamp":
			m.HasReloadStamp = true
			m.ReloadRealtime, _ = parseUTCus(v)
		case "UnitsLoadTimestampMonotonic":
			m.HasReloadStamp = true
			n, _ := strconv.ParseUint(v, 10, 63)
			m.ReloadMonotonic = time.Duration(n) * time.Microsecond
		}
	}
	if m.BootRealtime.IsZero() || m.BootMonotonic == 0 {
		return m, errors.New("the manager did not report when it loaded its units")
	}
	return m, nil
}

// parseUTCus parses `--timestamp=us+utc` output ("Wed 2026-11-04 11:27:46.629154 UTC").
func parseUTCus(v string) (time.Time, error) {
	if v == "" || v == "n/a" {
		return time.Time{}, nil
	}
	return time.Parse("Mon 2006-01-02 15:04:05.000000 MST", v)
}

// uptime returns CLOCK_BOOTTIME (/proc/uptime, 10 ms resolution); tests
// replace it. BOOTTIME is >= MONOTONIC (it includes suspend), so a threshold
// derived from it is earlier after a suspend (more refusals) and at most
// 10 ms later otherwise, which the generator directory's time bounds.
var uptime = func() (time.Duration, error) {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, err
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0, errors.New("empty /proc/uptime")
	}
	s, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return 0, err
	}
	return time.Duration(s * float64(time.Second)), nil
}

// loadThreshold is the time from which a change on disk counts as not yet
// loaded (probe 8b, probe 9). Units are read after the generators ran, and
// /run/systemd/generator is recreated by those generators at every boot and
// reload; its change time comes from the same coarse kernel clock as every
// other file's, so a change made after the units were read always has a
// change time at or after it: no margin is needed, and package files or links
// written just before their own reload (measured: 0.4-100 ms before) are not
// counted. The threshold is the EARLIEST of that time, the manager's realtime
// load stamp and the stamp's monotonic form converted to the current realtime
// frame (so a realtime clock step after the load cannot move it later).
// systemd < 255 has no reload stamp: the generator directory alone.
func loadThreshold(root Roots, m ManagerLoad, now time.Time) (time.Time, error) {
	var t time.Time
	consider := func(c time.Time) {
		if !c.IsZero() && (t.IsZero() || c.Before(t)) {
			t = c
		}
	}
	if fi, err := os.Stat(root.Path(generatorDir)); err == nil {
		if c := ctimeOf(fi); !c.After(now) {
			consider(c)
		}
	}
	if m.HasReloadStamp {
		real, mono := m.BootRealtime, m.BootMonotonic
		if !m.ReloadRealtime.IsZero() && m.ReloadMonotonic > 0 {
			real, mono = m.ReloadRealtime, m.ReloadMonotonic
		}
		consider(real)
		up, err := uptime()
		if err != nil {
			return time.Time{}, err
		}
		consider(now.Add(-(up - mono)))
	} else if t.IsZero() {
		consider(m.BootRealtime) // no generator directory: everything since boot
	}
	if t.IsZero() {
		return t, errors.New("no load time")
	}
	return t, nil
}

// ownWindowMargin widens an own change's recorded window: the kernel's
// file-time clock is coarse and can read up to one tick (<= 10 ms) earlier
// than the precise clock that recorded the window's start.
var ownWindowMargin = 20 * time.Millisecond

func ctimeOf(fi fs.FileInfo) time.Time {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return time.Unix(st.Ctim.Sec, st.Ctim.Nsec)
	}
	return fi.ModTime()
}

// ownChange is one completed enablement change from the operation journal.
type ownChange struct {
	start, end time.Time
	paths      map[string]bool // link paths and their directories up to /etc/systemd/system
}

// journalObserved is the outcome record's observed state for enablement:
// the unit state plus what the change touched and when (used by the change
// gate; the recovery inspector reads only the embedded fields).
type journalObserved struct {
	UnitObserved
	ChangedLinks    []string  `json:"changed_links,omitempty"`
	UnexpectedLinks []string  `json:"unexpected_links,omitempty"`
	StartedAt       time.Time `json:"started_at"`
	FinishedAt      time.Time `json:"finished_at"`
}

// ownChanges returns enablement changes the journal records as finished
// (any outcome with a recorded window) since t. Interrupted operations have
// no window and are never excused.
func ownChanges(env *Env, since time.Time) []ownChange {
	if env.Writes == nil {
		return nil
	}
	entries, err := env.Writes.Read()
	if err != nil {
		return nil
	}
	var out []ownChange
	for _, e := range entries {
		if e.Final == nil || e.Final.Op != enablementOpName || e.Final.Outcome == "interrupted" {
			continue
		}
		var o journalObserved
		if json.Unmarshal(e.Final.Observed, &o) != nil || o.StartedAt.IsZero() || o.FinishedAt.Before(since) {
			continue
		}
		c := ownChange{start: o.StartedAt, end: o.FinishedAt, paths: map[string]bool{}}
		for _, l := range o.ChangedLinks {
			for p := path.Clean(l); strings.HasPrefix(p, systemdUnitDir) || p+"/" == systemdUnitDir; p = path.Dir(p) {
				c.paths[p] = true
				if p+"/" == systemdUnitDir {
					break
				}
			}
		}
		out = append(out, c)
	}
	return out
}

// Bounds of the change scan (all directories together).
const (
	maxChangeScanEntries = 100000
	maxChangeScanDepth   = 8
)

// unloadedChanges lists entries under the unit directories changed at or
// after threshold and not attributed to an own change (sorted, at most
// limit shown; total count returned).
func unloadedChanges(root Roots, threshold time.Time, own []ownChange, limit int) ([]string, int, error) {
	merged := mergedUsr(root)
	var out []string
	n, seen := 0, 0
	for _, d := range changeScanDirs {
		if merged && d == "/lib/systemd/system/" {
			continue
		}
		base := root.Path(strings.TrimSuffix(d, "/"))
		if _, err := os.Lstat(base); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		err := filepath.WalkDir(base, func(p string, de fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if seen++; seen > maxChangeScanEntries {
				return errLinkScanBounds
			}
			rel, _ := filepath.Rel(base, p)
			if de.IsDir() && rel != "." && strings.Count(rel, string(filepath.Separator)) >= maxChangeScanDepth {
				return errLinkScanBounds
			}
			fi, err := os.Lstat(p)
			if err != nil {
				return err
			}
			c := ctimeOf(fi)
			if c.Before(threshold) {
				return nil
			}
			host := strings.TrimSuffix(d, "/")
			if rel != "." {
				host = path.Join(host, filepath.ToSlash(rel))
			}
			for _, o := range own {
				if o.paths[host] && !c.Before(o.start.Add(-ownWindowMargin)) && !c.After(o.end.Add(ownWindowMargin)) {
					return nil
				}
			}
			n++
			if len(out) < limit {
				out = append(out, host)
			}
			return nil
		})
		if err != nil {
			return nil, 0, unsupported("the unit directory %s could not be checked for changes", clean(d, 120))
		}
	}
	sort.Strings(out)
	return out, n, nil
}

// requireLoadedConfiguration refuses (enable and disable) when unit files changed on
// disk after systemd last loaded them, unless every change is Homelab
// Control's own journaled enablement change.
func requireLoadedConfiguration(ctx context.Context, env *Env, action, id string) error {
	for _, d := range runtimeUnitDirs {
		ents, err := os.ReadDir(env.Roots.Path(strings.TrimSuffix(d, "/")))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return denied("%s cannot be %sd here: %s could not be read", id, action, d)
		}
		if len(ents) > 0 {
			var names []string
			for _, e := range ents {
				if len(names) < 5 {
					names = append(names, e.Name())
				}
			}
			return denied("%s cannot be %sd here: runtime unit configuration exists in %s (%s). It is loaded now but gone at the next boot, "+
				"so the boot-time effect cannot be reviewed. Review the change again after it is removed or after the next boot.", id, action, d, clean(strings.Join(names, ", "), 300))
		}
	}
	m, err := env.enablement().ManagerLoad(ctx, env)
	if err != nil {
		return denied("%s cannot be %sd here: when systemd last loaded its unit files could not be determined (%s)", id, action, clean(err.Error(), 160))
	}
	now := time.Now()
	t, err := loadThreshold(env.Roots, m, now)
	if err != nil {
		return denied("%s cannot be %sd here: when systemd last loaded its unit files could not be determined", id, action)
	}
	changed, n, err := unloadedChanges(env.Roots, t, ownChanges(env, t), 5)
	if err != nil {
		return denied("%s cannot be %sd here: %s", id, action, err.Error())
	}
	if n > 0 {
		return denied("%s cannot be %sd here: %d unit file(s) or link(s) changed on disk after systemd last loaded its configuration (%s%s). "+
			"Boot-time effects of changes systemd has not loaded cannot be reviewed, and this version does not reload systemd. "+
			"Review the change again after the next boot or after systemd reloads its configuration.",
			id, action, n, clean(strings.Join(changed, ", "), 500), map[bool]string{true: ", …", false: ""}[n > len(changed)])
	}
	return nil
}

// Requisite= on disk (disable). A unit that declares Requisite= on this one
// fails at boot once this one is no longer started there; systemd's
// RequisiteOf covers only loaded units with loaded files. The broker scans
// the unit files on disk for Requisite= naming the unit or an alias.

const (
	maxRequisiteFiles = 20000
	maxRequisiteBytes = 64 << 10
)

// requisiteOwners returns the unit (or drop-in directory) names whose files
// on disk declare Requisite= on one of names.
func requisiteOwners(root Roots, names []string) ([]string, error) {
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	merged := mergedUsr(root)
	owners := map[string]bool{}
	files := 0
	for _, d := range append(append([]string{}, changeScanDirs...), "/etc/systemd/system.control/", "/run/systemd/system.control/", "/run/systemd/generator/", "/run/systemd/generator.early/", "/run/systemd/generator.late/", "/run/systemd/transient/") {
		if merged && d == "/lib/systemd/system/" {
			continue
		}
		base := root.Path(strings.TrimSuffix(d, "/"))
		if _, err := os.Lstat(base); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		err := filepath.WalkDir(base, func(p string, de fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if de.IsDir() {
				rel, _ := filepath.Rel(base, p)
				if rel != "." && strings.Count(rel, string(filepath.Separator)) >= 2 {
					return fs.SkipDir // unit files and <name>.d/ drop-ins only
				}
				return nil
			}
			if !de.Type().IsRegular() {
				return nil // links: aliases and .wants entries carry no settings of their own
			}
			if files++; files > maxRequisiteFiles {
				return errLinkScanBounds
			}
			b, err := readFixed(p, maxRequisiteBytes)
			if err != nil {
				return unsupported("%s could not be read", clean(p, 160))
			}
			if !requisiteNames(b, want) {
				return nil
			}
			owner := filepath.Base(p)
			if dir := filepath.Base(filepath.Dir(p)); strings.HasSuffix(dir, ".d") {
				owner = strings.TrimSuffix(dir, ".d")
			}
			owners[owner] = true
			return nil
		})
		if err != nil {
			return nil, unsupported("the unit files in %s could not be checked for Requisite=", clean(d, 120))
		}
	}
	out := make([]string, 0, len(owners))
	for o := range owners {
		out = append(out, o)
	}
	sort.Strings(out)
	return out, nil
}

// requisiteNames reports whether a unit file's [Unit] section has a
// Requisite= naming one of want (logical lines joined across continuations;
// specifiers and quotes count as a match, fail closed).
func requisiteNames(b []byte, want map[string]bool) bool {
	section, logical := "", ""
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	sc.Buffer(make([]byte, 0, 4096), maxRequisiteBytes)
	check := func(l string) bool {
		k, v, ok := strings.Cut(l, "=")
		if !ok || section != "Unit" || strings.TrimSpace(k) != "Requisite" {
			return false
		}
		if strings.ContainsAny(v, "%\"'\\") {
			return true
		}
		for _, f := range strings.Fields(v) {
			if want[f] {
				return true
			}
		}
		return false
	}
	for sc.Scan() {
		t := strings.TrimSpace(sc.Text())
		if logical == "" && (t == "" || t[0] == '#' || t[0] == ';') {
			continue
		}
		if logical == "" && strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			section = t[1 : len(t)-1]
			continue
		}
		if strings.HasSuffix(t, "\\") {
			logical += strings.TrimSuffix(t, "\\") + " "
			continue
		}
		l := logical + t
		logical = ""
		if check(l) {
			return true
		}
	}
	if sc.Err() != nil {
		return true // unreadable (for example an overlong line): fail closed
	}
	return logical != "" && check(logical)
}

// checkRequisitesOnDisk refuses disabling a unit that a protected unit, or a
// shared drop-in (whose units cannot be named), declares Requisite= on.
func checkRequisitesOnDisk(env *Env, id string, names []string) error {
	owners, err := requisiteOwners(env.Roots, names)
	if err != nil {
		return denied("%s cannot be disabled here: %s", id, err.Error())
	}
	for _, o := range owners {
		shared := strings.HasSuffix(strings.TrimSuffix(o, path.Ext(o)), "-") || !strings.Contains(o, ".")
		if shared {
			return denied("%s cannot be disabled here: the shared drop-in directory %s.d declares Requisite= on it, and the units it applies to cannot be checked", id, clean(o, 120))
		}
		if pr := servicepolicy.NameProtection(o); pr.Protected {
			return denied("%s is a requisite of %s (in its unit files on disk), which is protected: %s", id, o, pr.Reason)
		}
	}
	return nil
}
