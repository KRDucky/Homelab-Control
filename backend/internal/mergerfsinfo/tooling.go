// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mergerfsinfo

import "os"

// ToolItem is one probed package/binary the module needs.
type ToolItem struct {
	ID        string `json:"id"`
	Package   string `json:"package"`
	Installed bool   `json:"installed"`
	Binary    string `json:"binary"`
}

// Tooling is the package/binary probe result carried on the response.
type Tooling struct {
	Family   string     `json:"family"`
	Required []ToolItem `json:"required"`
	Optional []ToolItem `json:"optional"`
}

// statFunc reports whether path is an existing regular file (os.Stat follows
// usr-merge symlinks). Injectable for tests.
type statFunc func(path string) bool

// Well-known, fixed paths only: never search $PATH, never exec the binary.
var (
	mergerfsBinaryPaths = []string{"/usr/bin/mergerfs", "/usr/sbin/mergerfs"}
	snapraidBinaryPaths = []string{"/usr/bin/snapraid", "/usr/sbin/snapraid"}
)

func realStat(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}

// ProbeTooling reports binary presence with family-adapted package names. It
// never spawns a process and never calls the package manager.
func ProbeTooling(family string) Tooling {
	return probeTooling(family, realStat)
}

func probeTooling(family string, stat statFunc) Tooling {
	return Tooling{
		Family:   family,
		Required: []ToolItem{probeItem("mergerfs", requiredPackage(family), mergerfsBinaryPaths, stat)},
		Optional: []ToolItem{probeItem("snapraid", optionalPackage(family), snapraidBinaryPaths, stat)},
	}
}

func probeItem(id, pkg string, paths []string, stat statFunc) ToolItem {
	for _, p := range paths {
		if stat(p) {
			return ToolItem{ID: id, Package: pkg, Installed: true, Binary: p}
		}
	}
	return ToolItem{ID: id, Package: pkg, Installed: false}
}

// requiredPackage and optionalPackage are family-adapted starting names,
// labelled *verify* per distribution (see docs/addons/06-DISTRO-DEPENDENCIES.md).
// Unknown families get an empty package name: no install offer can be built.
func requiredPackage(family string) string {
	switch family {
	case "debian", "fedora", "suse", "arch":
		return "mergerfs"
	default:
		return ""
	}
}

func optionalPackage(family string) string {
	switch family {
	case "debian", "fedora", "suse", "arch":
		return "snapraid"
	default:
		return ""
	}
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
