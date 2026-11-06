// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// Final review (2026-11-05): unit search path and drop-in precedence are
// checked against systemd.unit(5) ("UNIT FILE LOAD PATH", drop-in rules) and
// the manager's own UnitPath, not only against measurements; anything else is
// refused with a specific reason.

func TestDropInDirNames(t *testing.T) {
	for n, want := range map[string][]string{
		"foo-bar-baz.service": {"foo-bar-baz.service.d", "foo-bar-.service.d", "foo-.service.d", "service.d"},
		"getty@tty1.service":  {"getty@tty1.service.d", "getty@.service.d", "service.d"},
		"demo.service":        {"demo.service.d", "service.d"},
		"service":             {"service.d"},
	} {
		if got := dropInDirNames(n); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v, want %v", n, got, want)
		}
	}
}

func TestParseManagerShowUnitPath(t *testing.T) {
	out := "UnitsLoadStartTimestamp=Wed 2026-11-04 11:25:34.973341 UTC\nUnitsLoadStartTimestampMonotonic=25090489585\nUnitPath=" +
		strings.Join(documentedUnitPath(true), " ") + "\n"
	m, err := ParseManagerShow([]byte(out))
	if err != nil || !reflect.DeepEqual(m.UnitPath, documentedUnitPath(true)) || checkUnitSearchPath(m.UnitPath) != nil {
		t.Fatalf("%v %v", m.UnitPath, err)
	}
	m, _ = ParseManagerShow([]byte(strings.SplitN(out, "UnitPath", 2)[0]))
	if m.UnitPath != nil || checkUnitSearchPath(m.UnitPath) == nil {
		t.Fatal("a missing UnitPath must be refused")
	}
}

// The manager's search path must be exactly the documented one (as reported
// by systemd 249, 255 and 259, with or without /lib/systemd/system).
func TestUnitSearchPathMustBeTheDocumentedOne(t *testing.T) {
	doc := documentedUnitPath(false)
	ins := func(i int, d string) []string { return append(append(append([]string{}, doc[:i]...), d), doc[i:]...) }
	bad := map[string][]string{
		"SYSTEMD_UNIT_PATH prepended": ins(0, "/opt/units"),
		"distribution extra":          ins(len(doc)-1, "/usr/lib64/systemd/system"),
		"order changed":               append([]string{doc[4]}, append(doc[:4:4], doc[5:]...)...),
		"directory missing":           doc[1:],
		"empty":                       {},
		"/lib after /usr/lib":         ins(len(doc)-1, "/lib/systemd/system"),
	}
	for name, up := range bad {
		for _, action := range []string{"enable", "disable"} {
			t.Run(name+"/"+action, func(t *testing.T) {
				var e *eenv
				if action == "enable" {
					e = enableEnv(t)
				} else {
					e = disableEnv(t)
				}
				e.files.unitPath = up
				refused(t, e, "demo.service", action, "unit search path")
			})
		}
	}
	for _, up := range [][]string{documentedUnitPath(true), documentedUnitPath(false)} {
		e := enableEnv(t)
		e.files.unitPath = up
		if _, err := e.eplan(t, "demo.service", "enable"); err != nil {
			t.Fatalf("documented path %v: %v", up, err)
		}
	}
}

// /lib/systemd/system comes before /usr/lib/systemd/system (as the manager
// reports it); on a split-/usr host the /lib file is the one used.
func TestPersistentEntryOrderOnSplitUsr(t *testing.T) {
	e := enableEnv(t)
	writeUnit(t, e.files.root, "/lib/systemd/system/split.service", "[Unit]\n")
	writeUnit(t, e.files.root, "/usr/lib/systemd/system/split.service", "[Unit]\n")
	if mergedUsr(e.env.Roots) {
		t.Skip("test root has merged /usr")
	}
	f, _, _ := diskUnitFiles(e.env.Roots, "split.service")
	if f != "/lib/systemd/system/split.service" {
		t.Fatalf("fragment on split /usr: %s", f)
	}
}

// Drop-in masks and same-named drop-ins across the drop-in hierarchy
// (systemd.unit(5): own, template, prefix and type-level directories) are
// refused for units the review reaches, whichever directory wins.
func TestRuntimeDropInMasksAndHierarchy(t *testing.T) {
	cases := map[string]struct{ runtime, persistent string }{
		"runtime mask hides a persistent drop-in": {"/run/systemd/system/my-app.service.d/10-x.conf", "/usr/lib/systemd/system/my-app.service.d/10-x.conf"},
		"own directory vs persistent prefix":      {"/run/systemd/system/my-app.service.d/10-x.conf", "/etc/systemd/system/my-.service.d/10-x.conf"},
		"own directory vs persistent type-level":  {"/run/systemd/system/my-app.service.d/10-x.conf", "/usr/lib/systemd/system/service.d/10-x.conf"},
		"system.control vs /etc":                  {"/run/systemd/system.control/my-app.service.d/10-x.conf", "/etc/systemd/system/my-app.service.d/10-x.conf"},
	}
	for name, c := range cases {
		for _, action := range []string{"enable", "disable"} {
			t.Run(name+"/"+action, func(t *testing.T) {
				var e *eenv
				if action == "enable" {
					e = enableEnv(t, inactive(unit("my-app.service", nil)))
				} else {
					e = disableEnv(t, unit("my-app.service", nil), unit(prot, nil))
				}
				pulls(e, "demo.service", "Wants", "my-app.service")
				if strings.Contains(name, "mask") {
					writeLink(t, e.files.root, c.runtime, "/dev/null")
				} else {
					writeUnit(t, e.files.root, c.runtime, "[Unit]\nDescription=runtime\n")
				}
				writeUnit(t, e.files.root, c.persistent, "[Unit]\nConflicts="+prot+"\nWants="+prot+"\n")
				e.files.reload()
				refused(t, e, "demo.service", action, "hidden by a runtime drop-in")
			})
		}
	}
	// Different names in the hierarchy: no refusal.
	e := enableEnv(t, inactive(unit("my-app.service", nil)))
	pulls(e, "demo.service", "Wants", "my-app.service")
	writeUnit(t, e.files.root, "/run/systemd/system/my-app.service.d/10-a.conf", "[Unit]\nDescription=runtime\n")
	writeUnit(t, e.files.root, "/usr/lib/systemd/system/service.d/10-b.conf", "[Unit]\nDescription=persistent\n")
	e.files.reload()
	if _, err := e.eplan(t, "demo.service", "enable"); err != nil {
		t.Fatalf("different names: %v", err)
	}
}

// The scan of units the walk does not evaluate also covers more general
// persistent drop-in directories (prefix) and drop-in masks.
func TestEnableHiddenPrefixDropInOfAnUnwalkedUnit(t *testing.T) {
	for _, mask := range []bool{false, true} {
		e := enableEnv(t, unit("z-a.service", nil), inactive(unit("y.service", nil)))
		pulls(e, "multi-user.target", "Wants", "z-a.service")
		pulls(e, "demo.service", "Wants", "y.service")
		if mask {
			writeLink(t, e.files.root, "/run/systemd/system/z-a.service.d/50-x.conf", "/dev/null")
		} else {
			writeUnit(t, e.files.root, "/run/systemd/system/z-a.service.d/50-x.conf", "[Unit]\nDescription=runtime\n")
		}
		writeUnit(t, e.files.root, "/etc/systemd/system/z-.service.d/50-x.conf", "[Unit]\nConflicts=y.service\n")
		e.files.reload()
		refused(t, e, "demo.service", "enable", "hidden by a runtime drop-in")
	}
	_ = time.Second
}

// A drop-in directory beyond the bounds refuses with that reason, not as a
// hidden drop-in.
func TestDropInDirectoryBoundsRefuseWithTheirReason(t *testing.T) {
	for _, action := range []string{"enable", "disable"} {
		var e *eenv
		if action == "enable" {
			e = enableEnv(t, inactive(unit("my-app.service", nil)))
		} else {
			e = disableEnv(t, unit("my-app.service", nil))
		}
		pulls(e, "demo.service", "Wants", "my-app.service")
		for i := 0; i <= maxLinkDirEntries; i++ {
			writeUnit(t, e.files.root, "/run/systemd/system/my-app.service.d/"+itoa(i)+".conf", "[Unit]\n")
		}
		e.files.reload()
		_, err := e.eplan(t, "demo.service", action)
		if err == nil || !strings.Contains(err.Error(), "could not be read within bounds") || strings.Contains(err.Error(), "hidden by") {
			t.Fatalf("%s: %v", action, err)
		}
	}
}
