// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// On-disk view of a unit's files and links (M2-3 review, probe 7 in
// docs/m2/M2-3-DESIGN-REVIEW.md). `systemctl enable` and `disable` work from
// the files on disk, not from the copy PID 1 has loaded, so the review must
// too:
//   - systemctl looks for the fragment and for "<id>.d/*.conf" drop-ins along
//     its search path; for a loaded unit PID 1's FragmentPath/DropInPaths may
//     be stale (a new drop-in or a new /etc fragment written without a reload
//     is honoured by enable but not listed by `systemctl show`, measured on
//     systemd 249 and 259);
//   - systemctl ignores [Install] in prefix ("foo-.service.d") and type-level
//     ("service.d") drop-ins, which PID 1 does list (measured);
//   - disable removes EVERY symbolic link under /etc/systemd/system, at any
//     depth, whose name or target names the unit, including links its
//     [Install] section does not describe (measured).

// installSearchDirs is systemctl's search order for system units.
var installSearchDirs = []string{
	"/etc/systemd/system.control/", "/run/systemd/system.control/", "/run/systemd/transient/",
	"/run/systemd/generator.early/", "/etc/systemd/system/", "/etc/systemd/system.attached/",
	"/run/systemd/system/", "/run/systemd/system.attached/", "/run/systemd/generator/",
	"/usr/local/lib/systemd/system/", "/usr/lib/systemd/system/", "/lib/systemd/system/",
	"/run/systemd/generator.late/",
}

// mergedUsr reports whether /lib is the merged-usr link to usr/lib.
func mergedUsr(root Roots) bool {
	t, err := os.Readlink(root.Path("/lib"))
	return err == nil && (t == "usr/lib" || t == "/usr/lib")
}

// canonUnitPath maps /lib/... to /usr/lib/... on merged-usr systems, so that
// systemd's answers (249 reports /lib/systemd/system/...) compare equal.
func canonUnitPath(root Roots, p string) string {
	if strings.HasPrefix(p, "/lib/") && mergedUsr(root) {
		return "/usr" + p
	}
	return p
}

// maxDropInEntries bounds one drop-in directory listing.
const maxDropInEntries = 256

// diskUnitFiles finds the fragment and the "<id>.d" drop-ins of id the way
// systemctl does: the first fragment along the search path, and drop-ins by
// file name with earlier directories overriding later ones, sorted by name.
func diskUnitFiles(root Roots, id string) (string, []string, error) {
	merged := mergedUsr(root)
	fragment := ""
	byName := map[string]string{}
	for _, d := range installSearchDirs {
		if merged && d == "/lib/systemd/system/" {
			continue // the same directory as /usr/lib/systemd/system/
		}
		if fragment == "" {
			if _, err := os.Lstat(root.Path(d + id)); err == nil {
				fragment = d + id
			} else if !errors.Is(err, fs.ErrNotExist) {
				return "", nil, unsupported("the unit directory %s could not be read", clean(d, 120))
			}
		}
		dir := d + id + ".d"
		fi, err := os.Lstat(root.Path(dir))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil || !fi.IsDir() {
			return "", nil, unsupported("%s is not a plain directory", clean(dir, 200))
		}
		ents, err := os.ReadDir(root.Path(dir))
		if err != nil || len(ents) > maxDropInEntries {
			return "", nil, unsupported("the drop-in directory %s could not be read safely", clean(dir, 200))
		}
		for _, e := range ents {
			if !strings.HasSuffix(e.Name(), ".conf") {
				continue
			}
			if _, seen := byName[e.Name()]; !seen {
				byName[e.Name()] = dir + "/" + e.Name()
			}
		}
	}
	if fragment == "" {
		return "", nil, unsupported("no unit file named %s exists on disk", clean(id, 120))
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	dropIns := make([]string, 0, len(names))
	for _, n := range names {
		dropIns = append(dropIns, byName[n])
	}
	return fragment, dropIns, nil
}

// ownDropIn reports whether a drop-in path belongs to "<id>.d" (as opposed to
// a prefix or type-level drop-in directory).
func ownDropIn(id, p string) bool { return path.Base(path.Dir(p)) == id+".d" }

// sameUnitFiles compares the on-disk files with the ones PID 1 has loaded.
func sameUnitFiles(root Roots, id, diskFragment string, diskDropIns []string, ip InstallProps) bool {
	if canonUnitPath(root, diskFragment) != canonUnitPath(root, ip.FragmentPath) {
		return false
	}
	var loaded []string
	for _, d := range ip.DropInPaths {
		if ownDropIn(id, d) {
			loaded = append(loaded, canonUnitPath(root, d))
		}
	}
	disk := make([]string, 0, len(diskDropIns))
	for _, d := range diskDropIns {
		disk = append(disk, canonUnitPath(root, d))
	}
	sort.Strings(loaded)
	sort.Strings(disk)
	return strings.Join(loaded, "\n") == strings.Join(disk, "\n")
}

// Link scan limits: systemctl disable walks the whole tree, and so does the
// broker, within bounds; beyond them the review refuses.
const (
	maxLinkScanEntries = 50000
	maxLinkScanDepth   = 8
)

// unitLink is a symbolic link that names a unit (by its own name or target).
type unitLink struct {
	Path   string // absolute host path
	Target string // as stored in the link
}

// scanUnitLinks returns every symbolic link below dir (absolute host path)
// whose name, or the last component of whose target, is one of names, or
// whose target is the fragment: the links `systemctl disable` removes
// (probe 7: names, renamed links, manual aliases, any depth). Directory links
// are not followed.
func scanUnitLinks(root Roots, dir string, names []string, fragment string) ([]unitLink, error) {
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	frag := canonUnitPath(root, fragment)
	base := root.Path(dir)
	if _, err := os.Lstat(base); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	var out []unitLink
	seen := 0
	err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		seen++
		if seen > maxLinkScanEntries {
			return errLinkScanBounds
		}
		rel, _ := filepath.Rel(base, p)
		if d.IsDir() && rel != "." && strings.Count(rel, string(filepath.Separator)) >= maxLinkScanDepth {
			return errLinkScanBounds
		}
		if d.Type()&fs.ModeSymlink == 0 {
			return nil
		}
		t, err := os.Readlink(p)
		if err != nil {
			return err
		}
		if want[d.Name()] || want[path.Base(t)] || (frag != "" && canonUnitPath(root, t) == frag) {
			out = append(out, unitLink{Path: path.Join(dir, filepath.ToSlash(rel)), Target: t})
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errLinkScanBounds) {
			return nil, unsupported("%s has more files or deeper directories than this version scans", clean(dir, 120))
		}
		return nil, unsupported("%s could not be scanned safely", clean(dir, 120))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

var errLinkScanBounds = errors.New("link scan bounds exceeded")

// pointsTo reports whether link target t (as stored) resolves to the fragment.
func pointsTo(root Roots, t, fragment string) bool {
	if !path.IsAbs(t) {
		return false // systemctl writes absolute targets
	}
	if canonUnitPath(root, t) == canonUnitPath(root, fragment) {
		return true
	}
	a, err1 := os.Stat(root.Path(t))
	b, err2 := os.Stat(root.Path(fragment))
	return err1 == nil && err2 == nil && os.SameFile(a, b)
}
