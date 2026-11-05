// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path"
	"strings"

	"homelab-control/backend/internal/servicepolicy"
)

// Runtime unit configuration (blocker B-2, probe 11; P-8 option 3 approved by
// the owner 2026-11-05 as an approach). systemd's loaded graph includes
// configuration under /run (runtime unit files, drop-ins, .wants links,
// aliases, masks, generator output, transient units). None of it is assumed
// to exist at the next boot. Instead of refusing whenever any exists (which
// refused everything on netplan-based Ubuntu hosts), the review asks, for the
// units it actually evaluates, what the boot-time graph built from PERSISTENT
// configuration alone would be:
//   - disable (boot-pull check): a protected unit counts as started at boot by
//     something else only through relations backed by persistent files and
//     links (an under-approximation: anything runtime-only is assumed gone);
//   - enable (start walk): every unit the walk evaluates must not be masked,
//     shadowed or have safety-relevant settings only until the next boot.
// Runtime configuration of units the review does not reach is irrelevant.

// persistentUnitDirs hold configuration that survives a reboot, in systemd's
// precedence order.
var persistentUnitDirs = []string{
	"/etc/systemd/system.control/", "/etc/systemd/system/", "/etc/systemd/system.attached/",
	"/usr/local/lib/systemd/system/", "/lib/systemd/system/", "/usr/lib/systemd/system/",
}

// isPersistentPath reports whether a unit file or drop-in path is in a
// persistent directory (anything under /run is not).
func isPersistentPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/run/") {
		return false
	}
	for _, d := range persistentUnitDirs {
		if strings.HasPrefix(p, d) {
			return true
		}
	}
	return false
}

// persistentEntry finds name in the persistent directories (first match in
// precedence order): exists, whether it is a mask (link to /dev/null), and
// the link target if it is a link.
func persistentEntry(root Roots, name string) (exists, mask bool, target string) {
	merged := mergedUsr(root)
	for _, d := range persistentUnitDirs {
		if merged && d == "/lib/systemd/system/" {
			continue
		}
		fi, err := os.Lstat(root.Path(d + name))
		if err != nil {
			continue
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			t, _ := os.Readlink(root.Path(d + name))
			return true, t == "/dev/null", t
		}
		return true, false, ""
	}
	return false, false, ""
}

// persistentMask reports whether any of the names is masked persistently.
func persistentMask(root Roots, names []string) bool {
	for _, n := range names {
		if _, mask, _ := persistentEntry(root, n); mask {
			return true
		}
	}
	return false
}

// bootFacts are the settings of a unit read from its persistent files.
type bootFacts struct {
	pulls     []string          // Requires=, Wants=, BindsTo=, Upholds= values
	triggers  []string          // [Socket] Service=, [Timer]/[Path] Unit=
	conflicts []string          // Conflicts= values
	binds     []string          // PartOf=, Requisite= values (stop propagation, no start)
	scalars   map[string]string // safety-relevant [Unit] settings, last assignment wins
	uncertain string            // why the files cannot be evaluated ("" if they can)
}

var bootPullFileKeys = map[string]bool{"Requires": true, "Wants": true, "BindsTo": true, "Upholds": true}

// bootScalarKeys are the [Unit] settings whose loaded value a runtime drop-in
// can override: emergency actions and trigger job modes.
var bootScalarKeys = map[string]bool{"FailureAction": true, "SuccessAction": true, "StartLimitAction": true,
	"JobTimeoutAction": true, "OnFailureJobMode": true, "OnSuccessJobMode": true}

// parseBootFacts reads the relevant keys of one unit file or drop-in into f.
// Logical lines are joined across continuations; specifiers, quotes, escapes
// and empty (reset) assignments of the read keys make the files uncertain.
func parseBootFacts(b []byte, f *bootFacts) {
	section, logical := "", ""
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	sc.Buffer(make([]byte, 0, 4096), maxUnitFileBytes)
	handle := func(l string) {
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			return
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		read := (section == "Unit" && (bootPullFileKeys[k] || bootScalarKeys[k] || k == "Conflicts" || k == "PartOf" || k == "Requisite")) ||
			(section == "Socket" && k == "Service") || ((section == "Timer" || section == "Path") && k == "Unit")
		if !read {
			return
		}
		if v == "" {
			f.uncertain = k + "= is reset with an empty assignment"
			return
		}
		if strings.ContainsAny(v, "%\"'\\") {
			f.uncertain = k + "= uses specifiers, quotes or escapes"
			return
		}
		switch {
		case bootScalarKeys[k] && section == "Unit":
			f.scalars[k] = v
		case k == "Conflicts":
			f.conflicts = append(f.conflicts, strings.Fields(v)...)
		case k == "PartOf" || k == "Requisite":
			f.binds = append(f.binds, strings.Fields(v)...)
		case section == "Unit":
			f.pulls = append(f.pulls, strings.Fields(v)...)
		default:
			f.triggers = append(f.triggers, strings.Fields(v)...)
		}
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
		handle(logical + t)
		logical = ""
	}
	if logical != "" {
		handle(logical)
	}
	if sc.Err() != nil {
		f.uncertain = "a unit file could not be read completely"
	}
}

// persistentFacts reads u's persistent fragment and persistent drop-ins (as
// systemd loaded them, runtime ones skipped). ok is false when u has no
// persistent fragment (it would not exist at the next boot, or its boot-time
// definition is unknown).
func persistentFacts(root Roots, u UnitProps) (f bootFacts, ok bool) {
	f.scalars = map[string]string{}
	if !isPersistentPath(u.FragmentPath) {
		return f, false
	}
	files := []string{u.FragmentPath}
	for _, d := range u.DropInPaths {
		if isPersistentPath(d) {
			files = append(files, d)
		}
	}
	for _, p := range files {
		b, err := readFixed(root.Path(p), maxUnitFileBytes)
		if err != nil {
			f.uncertain = "the unit file " + clean(p, 200) + " could not be read"
			return f, true
		}
		parseBootFacts(b, &f)
	}
	return f, true
}

// maxLinkDirEntries bounds one .wants/.requires/.upholds directory.
const maxLinkDirEntries = 1024

// errRuntimeBounds marks a graph too large to evaluate.
var errRuntimeBounds = errors.New("runtime evaluation bounds exceeded")

// persistentLinkPulls lists the units that persistent .wants/.requires/
// .upholds links of any of u's names pull in.
func persistentLinkPulls(root Roots, u UnitProps) ([]string, error) {
	merged := mergedUsr(root)
	var out []string
	for _, n := range append([]string{u.ID}, u.Names...) {
		for _, d := range persistentUnitDirs {
			if merged && d == "/lib/systemd/system/" {
				continue
			}
			for _, suf := range []string{".wants", ".requires", ".upholds"} {
				ents, err := os.ReadDir(root.Path(d + n + suf))
				if errors.Is(err, fs.ErrNotExist) {
					continue
				}
				if err != nil || len(ents) > maxLinkDirEntries {
					return nil, errRuntimeBounds
				}
				for _, e := range ents {
					if e.Type()&fs.ModeSymlink != 0 && !contains(out, e.Name()) {
						out = append(out, e.Name())
					}
				}
			}
		}
	}
	return out, nil
}

// bootPulls is what u pulls in at the next boot according to its persistent
// configuration. ok is false when u's boot-time relations cannot be
// established (no persistent fragment, or uncertain files): it then
// anchors nothing.
func bootPulls(root Roots, u UnitProps) (names []string, ok bool, err error) {
	f, persistent := persistentFacts(root, u)
	if !persistent || f.uncertain != "" {
		return nil, false, nil
	}
	links, err := persistentLinkPulls(root, u)
	if err != nil {
		return nil, false, err
	}
	names = append(append(names, f.pulls...), links...)
	if t := path.Ext(u.ID); t == ".socket" || t == ".timer" || t == ".path" {
		if len(f.triggers) > 0 {
			names = append(names, f.triggers...)
		} else {
			names = append(names, strings.TrimSuffix(u.ID, t)+".service") // the implicit same-name unit
		}
	}
	return names, true, nil
}

// bootName reports whether name (which systemd resolved to u) resolves to u
// at the next boot from persistent configuration alone.
func bootName(root Roots, name string, u UnitProps) bool {
	if name == u.ID {
		return isPersistentPath(u.FragmentPath)
	}
	exists, mask, target := persistentEntry(root, name)
	if !exists || mask || target == "" {
		return false
	}
	return pointsTo(root, target, u.FragmentPath) || path.Base(target) == u.ID
}

// runtimeAliasName returns a name of u that resolves to u only through
// runtime configuration while a persistent, different unit file of that name
// exists (at the next boot the name is that other unit).
func runtimeAliasName(root Roots, u UnitProps) (string, bool) {
	if u.LoadState != "loaded" {
		return "", false
	}
	for _, n := range u.Names {
		if n == u.ID {
			continue
		}
		if exists, mask, target := persistentEntry(root, n); exists && !mask &&
			!(target != "" && (pointsTo(root, target, u.FragmentPath) || path.Base(target) == u.ID)) {
			return n, true
		}
	}
	return "", false
}

// runtimeUnitDirs are the runtime directories of the documented search path
// (checkUnitSearchPath), whose contents are gone at the next boot.
var runtimeUnitDirs = []string{"/run/systemd/system.control/", "/run/systemd/transient/", "/run/systemd/generator.early/",
	"/run/systemd/system/", "/run/systemd/system.attached/", "/run/systemd/generator/", "/run/systemd/generator.late/"}

// dropInDirNames are the drop-in directories systemd reads for the unit name
// n (systemd.unit(5)): "<n>.d"; for a template instance the template's
// "<t>@.<type>.d"; for a name with dashes every truncated prefix
// "<p>-.<type>.d"; and the type-level "<type>.d".
func dropInDirNames(n string) []string {
	ext := path.Ext(n)
	base := strings.TrimSuffix(n, ext)
	out := []string{n + ".d"}
	if ext == "" {
		return out
	}
	if i := strings.Index(base, "@"); i >= 0 && i < len(base)-1 {
		out = append(out, base[:i+1]+ext+".d")
	}
	for j := len(base) - 2; j > 0; j-- {
		if base[j] == '-' {
			out = append(out, base[:j+1]+ext+".d")
		}
	}
	return append(out, strings.TrimPrefix(ext, ".")+".d")
}

// dropInNames lists the ".conf" entries (files, links, masks) in the given
// directories under each drop-in directory name: base name -> path.
func dropInNames(root Roots, dirs, dirNames []string) (map[string]string, error) {
	merged := mergedUsr(root)
	out := map[string]string{}
	for _, d := range dirs {
		if merged && d == "/lib/systemd/system/" {
			continue
		}
		for _, dn := range dirNames {
			ents, err := os.ReadDir(root.Path(d + dn))
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil || len(ents) > maxLinkDirEntries {
				return nil, errRuntimeBounds
			}
			for _, e := range ents {
				if strings.HasSuffix(e.Name(), ".conf") {
					if _, seen := out[e.Name()]; !seen {
						out[e.Name()] = d + dn + "/" + e.Name()
					}
				}
			}
		}
	}
	return out, nil
}

// hiddenPersistentDropIn: a runtime drop-in — a file, or a mask (a link to
// /dev/null) — with the same file name as a persistent drop-in that applies
// to u (its own, alias, template, prefix or type-level drop-in directories)
// hides or competes with it until the next boot. systemd.unit(5) orders
// same-named drop-ins by directory (/etc over /run over /usr/lib) and by the
// prefix hierarchy, but does not specify how both orders combine, so any
// such pair is treated as uncertain and refused, whichever wins now. Returns
// the persistent drop-in (or a reason if the directories could not be read).
func hiddenPersistentDropIn(root Roots, u UnitProps) (string, bool) {
	var dirNames []string
	for _, n := range append([]string{u.ID}, u.Names...) {
		for _, dn := range dropInDirNames(n) {
			if !contains(dirNames, dn) {
				dirNames = append(dirNames, dn)
			}
		}
	}
	rt, err := dropInNames(root, runtimeUnitDirs, dirNames)
	if err != nil {
		return "runtime drop-in directories of " + u.ID + " (not readable within bounds)", true
	}
	if len(rt) == 0 {
		return "", false
	}
	ps, err := dropInNames(root, persistentUnitDirs, dirNames)
	if err != nil {
		return "persistent drop-in directories of " + u.ID + " (not readable within bounds)", true
	}
	for name, rp := range rt {
		if pp, ok := ps[name]; ok {
			return pp + " (same name as " + rp + ")", true
		}
	}
	return "", false
}

// checkBootRuntimeForEnable applies the runtime checks to every unit the
// enable start walk evaluated (by canonical ID and requested names).
func checkBootRuntimeForEnable(env *Env, id string, evaluated map[string]UnitProps) error {
	done := map[string]bool{}
	for name, u := range evaluated {
		if name != u.ID && u.LoadState == "loaded" {
			// A name systemd resolves through a runtime alias: at boot it may
			// name a different, persistent unit file that was not evaluated.
			if exists, mask, target := persistentEntry(env.Roots, name); exists && !mask &&
				!(target != "" && (pointsTo(env.Roots, target, u.FragmentPath) || path.Base(target) == u.ID)) {
				return denied("%s cannot be enabled here: %s resolves to %s only until the next boot (runtime alias); at boot it names a different unit file that was not evaluated", id, name, u.ID)
			}
		}
		if done[u.ID] {
			continue
		}
		done[u.ID] = true
		names := append([]string{u.ID}, u.Names...)
		switch {
		case u.LoadState == "masked" && !persistentMask(env.Roots, names):
			return denied("%s cannot be enabled here: %s is masked only until the next boot (runtime mask); its boot-time behaviour was not evaluated", id, u.ID)
		case u.LoadState == "loaded" && !isPersistentPath(u.FragmentPath):
			if exists, mask, _ := persistentEntry(env.Roots, u.ID); exists && !mask {
				return denied("%s cannot be enabled here: %s is defined in %s only until the next boot; at boot its persistent unit file applies, which was not evaluated", id, u.ID, clean(u.FragmentPath, 200))
			}
		}
		if hidden, ok := hiddenPersistentDropIn(env.Roots, u); ok {
			return denied("%s cannot be enabled here: the persistent drop-in %s of %s applies at the next boot but is hidden by a runtime drop-in until then; its boot-time settings were not evaluated", id, clean(hidden, 200), u.ID)
		}
		runtimeDropIn := false
		for _, d := range u.DropInPaths {
			if !isPersistentPath(d) {
				runtimeDropIn = true
			}
		}
		if !runtimeDropIn || u.LoadState != "loaded" {
			continue
		}
		// A runtime drop-in may override a safety-relevant setting until the
		// next boot: the persistent value is what applies at boot.
		f, persistent := persistentFacts(env.Roots, u)
		if !persistent {
			continue // runtime-only unit: gone at boot (checked above)
		}
		if f.uncertain != "" {
			return denied("%s cannot be enabled here: %s has runtime drop-ins and its persistent files cannot be evaluated (%s)", id, u.ID, f.uncertain)
		}
		for k, v := range f.scalars {
			if (strings.HasSuffix(k, "Action") && !servicepolicy.SafeEmergencyAction(v)) || (strings.HasSuffix(k, "JobMode") && v == "isolate") {
				return denied("%s cannot be enabled here: at the next boot %s has %s=%s (a runtime drop-in overrides it only until then)", id, u.ID, k, clean(v, 40))
			}
		}
	}
	return checkRuntimeMasks(env, id, evaluated)
}

// maxRuntimeEntries bounds the top level of the runtime unit directories.
const maxRuntimeEntries = 4096

// checkRuntimeMasks: runtime configuration can hide persistent configuration
// of ANY unit until the next boot — a runtime mask, a runtime unit file that
// shadows a persistent one, or a runtime drop-in with the same name as a
// persistent drop-in. The hidden persistent file applies at boot, but systemd
// has not loaded it, so its relations to the units the enable walk evaluated
// are invisible in the loaded graph (no ConflictedBy=, BoundBy=, … on them),
// and the walk never reaches a unit through them (for example one started at
// boot anyway). If a hidden persistent file names an evaluated unit in
// Conflicts=, Requires=, Wants=, BindsTo=, Upholds=, PartOf= or Requisite=,
// the change is refused. Hidden configuration that names nothing evaluated
// is irrelevant (final safety review, §11).
func checkRuntimeMasks(env *Env, id string, evaluated map[string]UnitProps) error {
	walked := map[string]bool{}
	for name, u := range evaluated {
		walked[name] = true
		for _, n := range append([]string{u.ID}, u.Names...) {
			walked[n] = true
		}
	}
	// hidden is one persistent file that runtime configuration hides.
	type hidden struct{ unit, file, how string }
	var found []hidden
	files := 0
	for _, d := range runtimeUnitDirs {
		ents, err := os.ReadDir(env.Roots.Path(strings.TrimSuffix(d, "/")))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil || len(ents) > maxRuntimeEntries {
			return denied("%s cannot be enabled here: the runtime unit directory %s could not be read within bounds", id, d)
		}
		for _, e := range ents {
			name := e.Name()
			if e.IsDir() && strings.HasSuffix(name, ".d") {
				confs, err := os.ReadDir(env.Roots.Path(d + name))
				if err != nil || len(confs) > maxLinkDirEntries {
					return denied("%s cannot be enabled here: the runtime drop-in directory %s could not be read within bounds", id, clean(d+name, 200))
				}
				for _, c := range confs {
					if !strings.HasSuffix(c.Name(), ".conf") {
						continue
					}
					if files++; files > maxRuntimeEntries {
						return denied("%s cannot be enabled here: too many runtime drop-ins to evaluate", id)
					}
					// The same name in this or a more general drop-in directory
					// (template, prefix, type) of a persistent directory.
				persistent:
					for _, dn := range dropInDirNames(strings.TrimSuffix(name, ".d")) {
						for _, pd := range persistentUnitDirs {
							if _, err := os.Lstat(env.Roots.Path(pd + dn + "/" + c.Name())); err == nil {
								found = append(found, hidden{strings.TrimSuffix(name, ".d"), pd + dn + "/" + c.Name(), "a runtime drop-in with the same name"})
								break persistent
							}
						}
					}
				}
				continue
			}
			if e.IsDir() {
				continue // .wants/.requires directories add relations only
			}
			if d == "/run/systemd/generator.late/" {
				continue // documented below every persistent directory: hides no unit file
			}
			exists, mask, target := persistentEntry(env.Roots, name)
			if !exists || mask {
				continue // no persistent definition: nothing returns at boot
			}
			file := target
			if file == "" {
				for _, pd := range persistentUnitDirs {
					if _, err := os.Lstat(env.Roots.Path(pd + name)); err == nil {
						file = pd + name
						break
					}
				}
			}
			how := "a runtime unit file"
			if t, _ := os.Readlink(env.Roots.Path(d + name)); t == "/dev/null" {
				how = "a runtime mask"
			}
			found = append(found, hidden{name, file, how})
		}
	}
	for _, h := range found {
		f := bootFacts{scalars: map[string]string{}}
		if b, err := readFixed(env.Roots.Path(h.file), maxUnitFileBytes); err != nil {
			f.uncertain = "unreadable"
		} else {
			parseBootFacts(b, &f)
		}
		if walked[h.unit] && f.uncertain == "" {
			continue // reached by the walk itself: refused there if relevant
		}
		for _, c := range append(append(append([]string{}, f.conflicts...), f.pulls...), f.binds...) {
			if !walked[c] {
				continue
			}
			if h.how == "a runtime mask" {
				return denied("%s cannot be enabled here: %s is masked only until the next boot, and its persistent unit file relates to %s, which this change involves at boot",
					id, h.unit, c)
			}
			return denied("%s cannot be enabled here: %s of %s is hidden by %s until the next boot, and it relates to %s (which this change involves at boot); that relation was not evaluated",
				id, clean(h.file, 200), h.unit, h.how, c)
		}
		if f.uncertain != "" && servicepolicy.NameProtection(h.unit).Protected {
			return denied("%s cannot be enabled here: the protected unit %s has persistent configuration hidden by %s until the next boot that cannot be evaluated", id, h.unit, h.how)
		}
	}
	return nil
}
