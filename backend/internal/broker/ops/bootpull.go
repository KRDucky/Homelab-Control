// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"context"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"homelab-control/backend/internal/servicepolicy"
)

// Disable must not stop a protected unit from starting at boot (blocker B-1,
// probe 10, systemd 249/255/259): a unit that Wants=/Requires=/BindsTo=/
// Upholds= a protected unit — directly, transitively or through an alias —
// is, after it is disabled, no longer started at boot, and neither is the
// protected unit unless something else pulls it in. The owner approved this
// safety objective on 2026-11-05 (not a particular implementation).
//
// The check works on systemd's loaded graph, which the change gate keeps
// equal to the files on disk (disable now also requires that nothing changed
// on disk after the last load):
//  1. R: every unit the boot target (default.target, by its canonical ID)
//     pulls in, transitively, through Requires=, Wants=, BindsTo=, Upholds=
//     and Triggers= (socket/timer/path activation), with the unit being
//     disabled removed from the graph (as if it never started) — counting
//     only relations backed by persistent files and links and names that
//     resolve at boot (B-2: runtime configuration is assumed gone);
//  2. C: every unit the disabled unit pulls in through the same relations
//     that is not in R;
//  3. refused when C contains a protected unit (by its canonical ID or any
//     name), a .service that cannot be inspected, or a dangerous target, or
//     when either walk exceeds its budget or the boot target is unknown.
// Not modelled (each errs towards refusal or is listed as a limit in the
// design review): D-Bus activation, units started by other means (manual,
// kernel command line systemd.unit=), the unit itself still being pulled in
// by others.

// bootPullKeys are the relations through which a start (at boot) pulls in
// other units; Requisite= does not start anything.
var bootPullKeys = []string{"Requires", "Wants", "BindsTo", "Upholds", "Triggers"}

// maxBootWalk bounds the boot-graph walk (units shown).
var maxBootWalk = 4000

// bootRoot is the unit systemd starts at boot.
const bootRoot = "default.target"

// bootWalk is a bounded breadth-first walk over bootPullKeys.
type bootWalk struct {
	ctx     context.Context
	env     *Env
	overlay *linkOverlay
	// persistentOnly: follow only relations backed by persistent files and
	// links, and only names that resolve at boot (B-2; the anchoring graph R).
	persistentOnly bool
	exclude        map[string]bool      // names of the unit being disabled
	skip           map[string]bool      // canonical IDs not to enter (R, for the second walk)
	seen           map[string]bool      // names already resolved
	units          map[string]UnitProps // canonical ID → unit
	via            map[string]string    // canonical ID → the unit that pulled it in
	hidden         []string             // names that cannot be inspected
	shown          int
}

func newBootWalk(ctx context.Context, env *Env, ov *linkOverlay, exclude []string, skip map[string]bool) *bootWalk {
	w := &bootWalk{ctx: ctx, env: env, overlay: ov, exclude: map[string]bool{}, skip: skip, seen: map[string]bool{},
		units: map[string]UnitProps{}, via: map[string]string{}}
	for _, n := range exclude {
		w.exclude[n] = true
	}
	return w
}

// run walks from the start names (pulled in by "from").
func (w *bootWalk) run(start []string, from string) error {
	type item struct{ name, from string }
	queue := make([]item, 0, len(start))
	for _, n := range start {
		queue = append(queue, item{n, from})
	}
	for len(queue) > 0 {
		var names []string
		froms := map[string]string{}
		for _, it := range queue {
			if w.seen[it.name] || w.exclude[it.name] || w.overlay.goneNames[it.name] {
				continue
			}
			w.seen[it.name] = true
			if !inspectable(it.name) {
				w.hidden = append(w.hidden, it.name)
				continue
			}
			names = append(names, it.name)
			froms[it.name] = it.from
		}
		queue = queue[:0]
		if w.shown+len(names) > maxBootWalk {
			return denied("the boot dependency graph has more than %d units to check; manage this unit on the host", maxBootWalk)
		}
		w.shown += len(names)
		for i := 0; i < len(names); i += showBatch {
			batch := names[i:min(i+showBatch, len(names))]
			ps, err := w.env.units().ShowMany(w.ctx, w.env, batch)
			if err != nil {
				return err
			}
			for j, p := range ps {
				if excludedUnit(p, w.exclude) || w.skip[p.ID] {
					continue
				}
				if w.persistentOnly && !bootName(w.env.Roots, batch[min(j, len(batch)-1)], p) {
					continue // the name or the unit itself exists only until the next boot
				}
				if _, done := w.units[p.ID]; done {
					continue
				}
				w.units[p.ID] = p
				w.via[p.ID] = froms[batch[min(j, len(batch)-1)]]
				for _, n := range p.Names {
					w.seen[n] = true
				}
				if p.LoadState != "loaded" {
					continue // not-found or masked: systemd does not start it
				}
				pulled := w.overlay.pulls(p)
				if w.persistentOnly {
					names, ok, err := bootPulls(w.env.Roots, p)
					if err != nil {
						return denied("the boot dependencies of %s could not be read within bounds", p.ID)
					}
					if !ok {
						continue // its boot-time relations cannot be established: it anchors nothing
					}
					pulled = names
				}
				for _, n := range pulled {
					queue = append(queue, item{n, p.ID})
				}
			}
		}
	}
	return nil
}

func excludedUnit(p UnitProps, exclude map[string]bool) bool {
	if exclude[p.ID] {
		return true
	}
	for _, n := range p.Names {
		if exclude[n] {
			return true
		}
	}
	return false
}

// path explains how the disabled unit reaches id (for the refusal message).
func (w *bootWalk) path(id, self string) string {
	parts := []string{id}
	for cur := id; len(parts) < 12; {
		from, ok := w.via[cur]
		if !ok || from == "" || from == self {
			break
		}
		parts = append(parts, from)
		cur = from
	}
	parts = append(parts, self)
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, " → ")
}

// checkBootPulls refuses disabling p when that would stop a protected unit
// from starting at boot. It returns the units started at boot only through p
// (for the plan and the digest).
func checkBootPulls(ctx context.Context, env *Env, p UnitProps) ([]string, error) {
	self := append([]string{p.ID}, p.Names...)
	// 1. R: the boot graph without p.
	rs, err := env.units().ShowMany(ctx, env, []string{bootRoot})
	if err != nil {
		return nil, err
	}
	var root UnitProps
	if len(rs) == 1 {
		root = rs[0]
	}
	if root.LoadState != "loaded" || root.ID == "" {
		return nil, denied("%s cannot be disabled here: the boot target (%s) could not be resolved, so the effect on protected units at boot cannot be checked", p.ID, bootRoot)
	}
	ov, err := ownLinkOverlay(ctx, env)
	if err != nil {
		return nil, denied("%s cannot be disabled here: Homelab Control's own boot-time changes since systemd last loaded its configuration could not be determined", p.ID)
	}
	r := newBootWalk(ctx, env, ov, self, nil)
	r.persistentOnly = true
	if err := r.run([]string{root.ID}, ""); err != nil {
		return nil, err
	}
	inR := map[string]bool{}
	for id := range r.units {
		inR[id] = true
	}
	// 2. C: what p pulls in that nothing else in the boot graph does.
	c := newBootWalk(ctx, env, ov, self, inR)
	if err := c.run(ov.pulls(p), p.ID); err != nil {
		return nil, err
	}
	for _, n := range c.hidden {
		if strings.HasSuffix(n, ".service") {
			return nil, denied("%s cannot be disabled here: it pulls in %s at boot, which cannot be inspected safely", p.ID, n)
		}
	}
	var only []string
	ids := make([]string, 0, len(c.units))
	for id := range c.units {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	// p's own files: a runtime drop-in hiding a persistent one hides what p
	// pulls in at boot.
	if why := dropInRefusal(env.Roots, p); why != "" {
		return nil, denied("%s cannot be disabled here: %s; what it pulls in at boot was not evaluated", p.ID, why)
	}
	for _, id := range ids {
		u := c.units[id]
		// Runtime configuration on a unit p reaches (B-2): its boot-time
		// relations are unknown when it is masked only until the next boot,
		// when a runtime unit file shadows a persistent one, when one of its
		// names is a runtime alias over a persistent unit (relations are
		// shown by canonical ID, so the name they were written with is not
		// known), or when a runtime drop-in hides a persistent one.
		names := append([]string{u.ID}, u.Names...)
		if n, ok := runtimeAliasName(env.Roots, u); ok {
			return nil, denied("%s cannot be disabled here: it pulls in %s at boot (%s), and %s resolves to it only until the next boot (runtime alias); at boot that name is a different unit file, which was not evaluated",
				p.ID, u.ID, c.path(id, p.ID), n)
		}
		if why := dropInRefusal(env.Roots, u); why != "" {
			return nil, denied("%s cannot be disabled here: it pulls in %s at boot (%s), and %s", p.ID, u.ID, c.path(id, p.ID), why)
		}
		if u.LoadState == "masked" && !persistentMask(env.Roots, names) {
			return nil, denied("%s cannot be disabled here: it pulls in %s at boot (%s), which is masked only until the next boot; what %s would start at boot was not evaluated",
				p.ID, u.ID, c.path(id, p.ID), u.ID)
		}
		if u.LoadState == "loaded" && !isPersistentPath(u.FragmentPath) {
			if exists, mask, _ := persistentEntry(env.Roots, u.ID); exists && !mask {
				return nil, denied("%s cannot be disabled here: it pulls in %s at boot (%s), which is defined in %s only until the next boot; its persistent unit file was not evaluated",
					p.ID, u.ID, c.path(id, p.ID), clean(u.FragmentPath, 200))
			}
		}
		if servicepolicy.DangerousTarget(id) {
			continue // not started at boot through p any more: harmless
		}
		if pr := servicepolicy.NamesProtection(append([]string{u.ID}, u.Names...)...); pr.Protected {
			return nil, denied("%s cannot be disabled here: %s is started at boot only through it (%s), and %s is protected: %s. "+
				"Disabling it would stop that unit from starting at the next boot.", p.ID, u.ID, c.path(id, p.ID), pr.Matched, pr.Reason)
		}
		if u.LoadState == "loaded" {
			only = append(only, id)
		}
	}
	return only, nil
}

// linkOverlay corrects systemd's loaded graph for Homelab Control's own
// enablement changes made since the last load (--no-reload, so systemd has
// not loaded them; they are excused by the change gate). For every planned
// link path of those operations, the link's state on disk NOW decides:
// present → the target Wants= the unit; absent → it does not. An alias link
// that is absent makes that name unresolvable. (Own links are only
// "<target>.wants/<unit>" and "<alias>": RequiredBy=/UpheldBy= are refused.)
type linkOverlay struct {
	added     map[string][]string        // target name → unit names it now wants
	removed   map[string]map[string]bool // target name → unit names it no longer wants
	goneNames map[string]bool            // alias names whose link is gone
}

func ownLinkOverlay(ctx context.Context, env *Env) (*linkOverlay, error) {
	ov := &linkOverlay{added: map[string][]string{}, removed: map[string]map[string]bool{}, goneNames: map[string]bool{}}
	m, err := env.enablement().ManagerLoad(ctx, env)
	if err != nil {
		return nil, err
	}
	t, err := loadThreshold(env.Roots, m, time.Now())
	if err != nil {
		return nil, err
	}
	for _, c := range ownChanges(env, t) {
		for _, l := range c.links {
			l = path.Clean(l)
			fi, err := os.Lstat(env.Roots.Path(l))
			present := err == nil && fi.Mode()&os.ModeSymlink != 0
			dir, name := path.Dir(l), path.Base(l)
			switch {
			case dir+"/" == systemdUnitDir: // an alias
				if !present {
					ov.goneNames[name] = true
				} else {
					delete(ov.goneNames, name)
				}
			case strings.HasSuffix(dir, ".wants") && path.Dir(dir)+"/" == systemdUnitDir:
				tgt := strings.TrimSuffix(path.Base(dir), ".wants")
				if present {
					ov.added[tgt] = append(ov.added[tgt], name)
					delete(ov.removed[tgt], name)
				} else {
					if ov.removed[tgt] == nil {
						ov.removed[tgt] = map[string]bool{}
					}
					ov.removed[tgt][name] = true
				}
			}
		}
	}
	return ov, nil
}

// pulls returns u's boot pull relations with the overlay applied.
func (ov *linkOverlay) pulls(u UnitProps) []string {
	deps := union(u, bootPullKeys...)
	if ov == nil {
		return deps
	}
	names := append([]string{u.ID}, u.Names...)
	var out []string
	for _, d := range deps {
		drop := false
		for _, n := range names {
			if ov.removed[n][d] {
				drop = true
			}
		}
		if !drop {
			out = append(out, d)
		}
	}
	for _, n := range names {
		for _, a := range ov.added[n] {
			if !contains(out, a) {
				out = append(out, a)
			}
		}
	}
	return out
}

// bootModel is what the enable review (the start at the NEXT boot) knows
// about that boot, independent of what is running now (final review
// 2026-11-05: the start walk used to skip running dependencies and to stop-
// check only running conflicts, so a change allowed now could break a
// protected unit at the next boot):
//   - anchored: units the boot target pulls in through persistent relations
//     alone, without the unit being enabled (R of the disable check). Only
//     these are not walked further: they start at boot whatever the change
//     does. An under-approximation, so it can only add checks.
//   - mayRun: units that may run at the next boot (anchored, or reached from
//     the boot target in the loaded graph including runtime and generated
//     relations and Homelab Control's own unloaded links). A conflict with
//     any of them, or with a unit the change itself starts, or a running
//     one, gets the full stop checks. Units started at boot by other means
//     (udev, D-Bus activation, the kernel command line) that are not running
//     now are not in it: a residual limit (design review §11).
type bootModel struct {
	anchored map[string]bool
	mayRun   map[string]bool
	ov       *linkOverlay
}

// deps adds, for the boot review, socket/timer/path activation (Triggers=)
// and Homelab Control's own .wants links that systemd has not loaded.
func (m *bootModel) deps(u UnitProps, deps []string) []string {
	out := append([]string{}, deps...)
	for _, n := range m.ov.pulls(u) {
		if !contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

func newBootModel(ctx context.Context, env *Env, p UnitProps) (*bootModel, error) {
	rs, err := env.units().ShowMany(ctx, env, []string{bootRoot})
	if err != nil {
		return nil, err
	}
	if len(rs) != 1 || rs[0].LoadState != "loaded" || rs[0].ID == "" {
		return nil, denied("%s cannot be enabled here: the boot target (%s) could not be resolved, so what it starts at boot cannot be checked", p.ID, bootRoot)
	}
	root := rs[0]
	ov, err := ownLinkOverlay(ctx, env)
	if err != nil {
		return nil, denied("%s cannot be enabled here: Homelab Control's own boot-time changes since systemd last loaded its configuration could not be determined", p.ID)
	}
	self := append([]string{p.ID}, p.Names...)
	r := newBootWalk(ctx, env, ov, self, nil)
	r.persistentOnly = true
	if err := r.run([]string{root.ID}, ""); err != nil {
		return nil, err
	}
	l := newBootWalk(ctx, env, ov, nil, nil)
	if err := l.run([]string{root.ID}, ""); err != nil {
		return nil, err
	}
	m := &bootModel{anchored: map[string]bool{}, mayRun: map[string]bool{}, ov: ov}
	for id := range r.units {
		m.anchored[id], m.mayRun[id] = true, true
	}
	for id := range l.units {
		m.mayRun[id] = true
	}
	return m, nil
}
