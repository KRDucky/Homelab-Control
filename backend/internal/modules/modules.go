// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package modules is the minimal, compiled-in module registry (M2-3; D-10
// proposed implementation, docs/m2/M2-3-DESIGN-REVIEW.md §5). A module
// declares the API route prefix and broker op prefix it owns; an
// administrator can disable it in a root-owned file read at start by both
// hc-api and hc-broker. Disabled modules have no routes (404, identical to an
// unknown route) and their broker operations are refused.
//
// There is no runtime loading, no third-party module and no way to change the
// setting through the API: this package holds no privileged code and executes
// nothing, so hc-api may import it.
package modules

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"syscall"
)

// DefaultPath is the administrator's module setting.
const DefaultPath = "/etc/homelab-control/modules.json"

// Module is one compiled-in, first-party module.
type Module struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Scopes      []string `json:"scopes"`
	// APIPrefix: every route under it belongs to the module.
	APIPrefix string `json:"-"`
	// OpPrefix: every broker op whose name starts with it belongs to the module.
	OpPrefix string `json:"-"`
	// Path is the module's page in the web UI.
	Path string `json:"path"`
}

var registry = []Module{
	{
		ID:          "services",
		Title:       "Services",
		Description: "systemd units: inventory, timers and sockets, unit details and logs; start/stop/restart and boot-time enable/disable through reviewed operations",
		Scopes:      []string{"services.read", "services.logs", "services.files", "services.operate", "services.configure"},
		APIPrefix:   "/api/v1/services",
		OpPrefix:    "services.",
		Path:        "/services",
	},
	{
		ID:          "packages",
		Title:       "Software",
		Description: "read-only Software Center: package search, installed list, pending-update preview and configured sources from the local cache",
		Scopes:      []string{"packages.read"},
		APIPrefix:   "/api/v1/packages",
		OpPrefix:    "packages.",
		Path:        "/software",
	},
	{ID: "networking", Title: "Networking", Description: "Reverse Proxy: read-only Traefik inventory, diagnostics and label previews", Scopes: []string{"networking.proxy.read"}, APIPrefix: "/api/v1/networking", Path: "/networking/reverse-proxy/traefik"},
	{ID: "mergerfs", Title: "MergerFS", Description: "read-only MergerFS pool inventory and optional SnapRAID configuration from the local host", Scopes: []string{"mergerfs.read"}, APIPrefix: "/api/v1/mergerfs", Path: "/storage/mergerfs"},
	{ID: "zfs", Title: "ZFS", Description: "read-only OpenZFS inventory from the local host", Scopes: []string{"zfs.read"}, APIPrefix: "/api/v1/zfs", Path: "/storage/zfs"},
}

// All returns the registered modules.
func All() []Module { return slices.Clone(registry) }

// Get returns a module by ID.
func Get(id string) (Module, bool) {
	for _, m := range registry {
		if m.ID == id {
			return m, true
		}
	}
	return Module{}, false
}

// ForRoute returns the module owning an API path ("" for core routes). A
// route belongs to a module when the path equals its prefix or continues it
// with '/'.
func ForRoute(path string) string {
	for _, m := range registry {
		if path == m.APIPrefix || strings.HasPrefix(path, m.APIPrefix+"/") {
			return m.ID
		}
	}
	return ""
}

// ForOp returns the module owning a broker op ("" for core ops).
func ForOp(op string) string {
	for _, m := range registry {
		if m.OpPrefix != "" && strings.HasPrefix(op, m.OpPrefix) {
			return m.ID
		}
	}
	return ""
}

// Set is the effective enablement. The zero value enables every module.
type Set struct {
	disabled map[string]bool
	// Reason explains a fail-closed state ("" when the setting was read).
	Reason string
}

// Enabled reports whether module id is enabled; core ("") always is.
func (s Set) Enabled(id string) bool { return id == "" || !s.disabled[id] }

// Disabled lists disabled module IDs.
func (s Set) Disabled() []string {
	var out []string
	for _, m := range registry {
		if s.disabled[m.ID] {
			out = append(out, m.ID)
		}
	}
	return out
}

// AllDisabled is the fail-closed state: every module disabled.
func AllDisabled(reason string) Set {
	s := Set{disabled: map[string]bool{}, Reason: reason}
	for _, m := range registry {
		s.disabled[m.ID] = true
	}
	return s
}

type file struct {
	Version  int      `json:"version"`
	Disabled []string `json:"disabled"`
}

const maxFile = 16 << 10

// ErrUnsafe marks a setting file with unsafe ownership or permissions.
var ErrUnsafe = errors.New("module setting file is not root-owned or is group/world-writable")

// Load reads the setting. A missing file means every module is enabled. Any
// other problem is an error; callers then use AllDisabled (fail closed).
// requireRoot checks that the file is owned by uid 0 (production); tests pass
// false.
func Load(path string, requireRoot bool) (Set, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return Set{disabled: map[string]bool{}}, nil
	}
	if err != nil {
		return Set{}, fmt.Errorf("%s: %w", path, err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return Set{}, err
	}
	if !fi.Mode().IsRegular() {
		return Set{}, fmt.Errorf("%s: not a regular file", path)
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return Set{}, fmt.Errorf("%s: %w", path, ErrUnsafe)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && requireRoot && st.Uid != 0 {
		return Set{}, fmt.Errorf("%s: %w", path, ErrUnsafe)
	}
	b, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if err != nil {
		return Set{}, err
	}
	if len(b) > maxFile {
		return Set{}, fmt.Errorf("%s: larger than %d bytes", path, maxFile)
	}
	return Parse(b)
}

// Parse strictly decodes the setting: {"version":1,"disabled":["id",...]}.
// Unknown fields, unknown module IDs and trailing data are errors.
func Parse(b []byte) (Set, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var f file
	if err := dec.Decode(&f); err != nil {
		return Set{}, fmt.Errorf("module setting: %w", err)
	}
	if dec.More() {
		return Set{}, errors.New("module setting: trailing data")
	}
	if f.Version != 1 {
		return Set{}, fmt.Errorf("module setting: unsupported version %d", f.Version)
	}
	s := Set{disabled: map[string]bool{}}
	for _, id := range f.Disabled {
		if _, ok := Get(id); !ok {
			return Set{}, fmt.Errorf("module setting: unknown module %q", id)
		}
		s.disabled[id] = true
	}
	return s, nil
}

// LoadOrFailClosed is Load that never returns an error: an unreadable or
// invalid setting disables every module and records why.
func LoadOrFailClosed(path string, requireRoot bool) Set {
	s, err := Load(path, requireRoot)
	if err != nil {
		return AllDisabled("The module setting " + path + " could not be used (" + err.Error() + "), so every optional module is disabled.")
	}
	return s
}
