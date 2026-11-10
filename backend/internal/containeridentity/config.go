// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package containeridentity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"syscall"

	"homelab-control/backend/internal/platform"
)

// PlatformDefaults is the administrator's identity context for one
// registered platform. Kind and mode are never taken from this file: they
// come from the platform registry, so the file cannot relabel a rootful
// daemon as rootless.
type PlatformDefaults struct {
	Defaults     Settings          `json:"defaults"`
	MappingKnown bool              `json:"mappingKnown"`
	UIDMap       []IDRange         `json:"uidMap"`
	GIDMap       []IDRange         `json:"gidMap"`
	Groups       []NamedGroup      `json:"groups"`
	Roles        map[string]uint32 `json:"roles"`
}

// Config is the optional administrator file passed with -container-identity-config.
type Config struct {
	SchemaVersion int                         `json:"schemaVersion"`
	Platforms     map[string]PlatformDefaults `json:"platforms"`
}

var (
	roleName  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	groupName = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
)

// LoadConfig reads the configuration from a private regular file
// (no symlink, not group/other-writable, at most 64 KiB) and validates it.
func LoadConfig(path string) (Config, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return Config{}, errors.New("identity configuration unavailable")
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0o022 != 0 || st.Size() > 64<<10 {
		return Config{}, errors.New("identity configuration must be a private regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, 64<<10+1))
	if err != nil {
		return Config{}, errors.New("identity configuration unreadable")
	}
	return ParseConfig(b)
}

// ParseConfig strictly decodes and validates configuration bytes.
func ParseConfig(b []byte) (Config, error) {
	var probe any
	if platform.DecodeBounded(b, 64<<10, 8, &probe) != nil {
		return Config{}, errors.New("identity configuration must be one bounded JSON object without duplicate keys")
	}
	var c Config
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("identity configuration: %v", err)
	}
	if c.SchemaVersion != 1 || len(c.Platforms) > 32 {
		return Config{}, errors.New("identity configuration: schemaVersion must be 1 with at most 32 platforms")
	}
	for id, p := range c.Platforms {
		if !platform.ValidID(id) {
			return Config{}, fmt.Errorf("identity configuration: invalid platform id")
		}
		if err := p.validate(); err != nil {
			return Config{}, fmt.Errorf("identity configuration for %s: %v", id, err)
		}
	}
	return c, nil
}

func (p PlatformDefaults) validate() error {
	d := p.Defaults
	for _, v := range []*uint32{d.UID, d.GID} {
		if v != nil && (*v == 0 || *v > MaxID) {
			return errors.New("default UID/GID must be non-root and in range")
		}
	}
	if d.SupplementalGroups != nil {
		if len(*d.SupplementalGroups) > MaxSupplementalGroups {
			return errors.New("too many default supplemental groups")
		}
		for _, g := range *d.SupplementalGroups {
			if g == 0 || g > MaxID {
				return errors.New("default supplemental groups must be non-root and in range")
			}
		}
	}
	if err := firstErr(ValidateRanges(p.UIDMap), ValidateRanges(p.GIDMap)); err != nil {
		return err
	}
	if !p.MappingKnown && (len(p.UIDMap) > 0 || len(p.GIDMap) > 0) {
		return errors.New("uidMap/gidMap require mappingKnown")
	}
	if len(p.Groups) > 256 || len(p.Roles) > 32 {
		return errors.New("too many groups or roles")
	}
	seen := map[uint32]bool{}
	for _, g := range p.Groups {
		if !groupName.MatchString(g.Name) || g.GID > MaxID || seen[g.GID] {
			return errors.New("groups need valid names and unique GIDs")
		}
		seen[g.GID] = true
	}
	for role, gid := range p.Roles {
		if !roleName.MatchString(role) || gid == 0 || gid > MaxID {
			return errors.New("roles need valid names and non-root GIDs")
		}
	}
	return nil
}

// Context combines administrator defaults with the registry's view of a
// platform. Without configuration, the platform has no defaults and an
// unknown mapping (image defaults are preserved).
func (c Config) Context(view platform.PublicView) PlatformContext {
	pc := PlatformContext{ID: view.ID, Kind: string(view.Kind), Mode: string(view.Mode)}
	if d, ok := c.Platforms[view.ID]; ok {
		pc.Defaults, pc.MappingKnown, pc.UIDMap, pc.GIDMap, pc.Groups, pc.Roles = d.Defaults, d.MappingKnown, d.UIDMap, d.GIDMap, d.Groups, d.Roles
	}
	// MappingKnown with empty maps means container IDs are host IDs (for
	// example rootful without userns-remap); that must be stated explicitly.
	return pc
}
