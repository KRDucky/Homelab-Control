// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package storagecap is the shared P0 storage capability and install-plan
// layer. It probes well-known binary paths with os.Stat only: no $PATH
// search, no os/exec, no package-manager spawn. Install plans never
// execute (ADR 0014 is unwritten). This package does not mutate storage.
package storagecap

import "os"

// StatFunc reports whether path is an existing regular file (os.Stat
// follows usr-merge symlinks). Injectable for tests.
type StatFunc func(path string) bool

// Spec is one required or optional tool to probe.
type Spec struct {
	ID       string
	Package  string
	Paths    []string
	Required bool
}

// ToolItem is one probed package/binary. JSON tags match the MergerFS
// first-checkpoint wire format.
type ToolItem struct {
	ID        string `json:"id"`
	Package   string `json:"package"`
	Installed bool   `json:"installed"`
	Binary    string `json:"binary"`
}

// Tooling is the package/binary probe result.
type Tooling struct {
	Family   string     `json:"family"`
	Required []ToolItem `json:"required"`
	Optional []ToolItem `json:"optional"`
}

// RealStat is the production StatFunc.
func RealStat(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}

// Probe reports binary presence for specs. Unknown families may still
// receive empty Package names from the caller; this function does not
// invent package names.
func Probe(family string, specs []Spec, stat StatFunc) Tooling {
	if stat == nil {
		stat = RealStat
	}
	out := Tooling{Family: family}
	for _, s := range specs {
		item := probeItem(s, stat)
		if s.Required {
			out.Required = append(out.Required, item)
		} else {
			out.Optional = append(out.Optional, item)
		}
	}
	return out
}

func probeItem(s Spec, stat StatFunc) ToolItem {
	for _, p := range s.Paths {
		if stat(p) {
			return ToolItem{ID: s.ID, Package: s.Package, Installed: true, Binary: p}
		}
	}
	return ToolItem{ID: s.ID, Package: s.Package, Installed: false}
}

// RequiredInstalled reports whether a required tool id is installed.
func (t Tooling) RequiredInstalled(id string) bool {
	for _, it := range t.Required {
		if it.ID == id {
			return it.Installed
		}
	}
	return false
}

// KnownFamily is true for the four pkgcenter families.
func KnownFamily(family string) bool {
	switch family {
	case "debian", "fedora", "suse", "arch":
		return true
	default:
		return false
	}
}
