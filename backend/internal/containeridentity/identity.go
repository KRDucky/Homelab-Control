// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

// Package containeridentity is HomeControl CORE's runtime-neutral model for
// container process identity: PUID, PGID, UMASK and supplemental groups.
//
// It resolves layered desired settings (image, platform, application,
// profile, per-container override), validates them against what the image
// and platform actually support, and renders platform-native configuration
// previews (Compose fields for Docker/Podman, security contexts for
// Kubernetes). It never applies anything: it has no runtime, filesystem or
// network access, and desired values are always kept separate from values a
// platform observed.
package containeridentity

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

// MaxID is the largest valid Linux UID/GID. 4294967295 is (uid_t)-1, which
// the kernel treats as "no change" and is therefore never a usable identity.
const MaxID uint32 = 4294967294

// MaxSupplementalGroups bounds how many extra GIDs one container may request.
const MaxSupplementalGroups = 32

// Layer names the configuration level that supplied a value.
type Layer string

const (
	LayerUnset       Layer = "unset" // nothing supplied: the image's own behaviour applies
	LayerImage       Layer = "image"
	LayerPlatform    Layer = "platform"
	LayerApplication Layer = "application"
	LayerProfile     Layer = "profile"
	LayerOverride    Layer = "override"
)

// Mode is the user's top-level choice in "Container Identity & Permissions".
type Mode string

const (
	ModeImage   Mode = "image"   // use image defaults: HomeControl sets nothing
	ModeInherit Mode = "inherit" // platform/host defaults, refined by application defaults and an optional profile
	ModeCustom  Mode = "custom"  // as inherit, plus per-container overrides
)

// Umask is an octal file-creation mask (0000-0777).
type Umask uint16

var umaskSyntax = regexp.MustCompile(`^0?[0-7]{3}$`)

// ParseUmask accepts three octal digits with an optional leading zero
// ("002", "0022", "027", "077").
func ParseUmask(s string) (Umask, error) {
	if !umaskSyntax.MatchString(s) {
		return 0, errors.New("umask must be three octal digits with an optional leading 0, for example 022")
	}
	v, err := strconv.ParseUint(s, 8, 16)
	if err != nil || v > 0o777 {
		return 0, errors.New("umask out of range")
	}
	return Umask(v), nil
}

func (u Umask) String() string { return fmt.Sprintf("%03o", uint16(u)) }

func (u Umask) MarshalJSON() ([]byte, error) { return json.Marshal(u.String()) }

func (u *Umask) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("umask must be a string such as \"022\"")
	}
	v, err := ParseUmask(s)
	if err != nil {
		return err
	}
	*u = v
	return nil
}

// Settings is one layer of desired identity configuration. Nil fields are
// "not set by this layer". SupplementalGroups distinguishes nil (not set)
// from an explicit empty list (no extra groups); the highest layer that sets
// the list replaces lower layers entirely, so group grants never accumulate
// silently.
type Settings struct {
	UID                *uint32   `json:"uid,omitempty"`
	GID                *uint32   `json:"gid,omitempty"`
	Umask              *Umask    `json:"umask,omitempty"`
	SupplementalGroups *[]uint32 `json:"supplementalGroups,omitempty"`
}

// IdentityMethod is how an image accepts a process identity.
type IdentityMethod string

const (
	// MethodEnv: the image's entrypoint reads PUID/PGID-style variables and
	// drops privileges itself. Environment variables do nothing unless the
	// image is declared to honour them.
	MethodEnv IdentityMethod = "env"
	// MethodRuntimeUser: the runtime starts the process as uid:gid
	// (Compose `user:`, Kubernetes runAsUser/runAsGroup).
	MethodRuntimeUser IdentityMethod = "runtime-user"
	// MethodFixed: the image only works with its built-in identity.
	MethodFixed IdentityMethod = "fixed"
	// MethodUnknown: nothing is declared; HomeControl must not guess.
	MethodUnknown IdentityMethod = "unknown"
)

// UmaskMethod is how an image accepts a UMASK.
type UmaskMethod string

const (
	UmaskEnv        UmaskMethod = "env"        // the image documents a variable (UMASK, UMASK_SET, ...)
	UmaskEntrypoint UmaskMethod = "entrypoint" // HomeControl-owned entrypoint applies the variable before exec
	UmaskNone       UmaskMethod = "none"       // declared unsupported
	UmaskUnknown    UmaskMethod = "unknown"
)

// ImageSupport is what an image (or the plugin manifest describing it)
// declares about identity handling. Nothing here is inferred from names.
type ImageSupport struct {
	IdentityMethod     IdentityMethod `json:"identityMethod"`
	UIDVariable        string         `json:"uidVariable,omitempty"`
	GIDVariable        string         `json:"gidVariable,omitempty"`
	UmaskMethod        UmaskMethod    `json:"umaskMethod"`
	UmaskVariable      string         `json:"umaskVariable,omitempty"`
	SupplementalGroups bool           `json:"supplementalGroups"`
	// Fixed identity, required when IdentityMethod is fixed.
	FixedUID *uint32 `json:"fixedUid,omitempty"`
	FixedGID *uint32 `json:"fixedGid,omitempty"`
	// Declared image defaults, when known (for example from the image's USER).
	DefaultUID *uint32 `json:"defaultUid,omitempty"`
	DefaultGID *uint32 `json:"defaultGid,omitempty"`
}

var envName = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

// Validate checks that the declaration is internally consistent.
func (s ImageSupport) Validate() error {
	switch s.IdentityMethod {
	case MethodEnv:
		if !envName.MatchString(s.UIDVariable) || !envName.MatchString(s.GIDVariable) || s.UIDVariable == s.GIDVariable {
			return errors.New("env identity method requires distinct uidVariable and gidVariable")
		}
	case MethodFixed:
		if s.FixedUID == nil || s.FixedGID == nil || *s.FixedUID > MaxID || *s.FixedGID > MaxID {
			return errors.New("fixed identity method requires fixedUid and fixedGid")
		}
	case MethodRuntimeUser, MethodUnknown:
	default:
		return errors.New("unknown identity method")
	}
	switch s.UmaskMethod {
	case UmaskEnv, UmaskEntrypoint:
		if !envName.MatchString(s.UmaskVariable) {
			return errors.New("umask method requires an umaskVariable")
		}
	case UmaskNone, UmaskUnknown:
	default:
		return errors.New("unknown umask method")
	}
	for _, p := range []*uint32{s.DefaultUID, s.DefaultGID} {
		if p != nil && *p > MaxID {
			return errors.New("image default identity out of range")
		}
	}
	return nil
}

// IDRange maps Size consecutive container IDs starting at ContainerID onto
// host IDs starting at HostID (the shape of /proc/<pid>/uid_map).
type IDRange struct {
	ContainerID uint32 `json:"containerId"`
	HostID      uint32 `json:"hostId"`
	Size        uint32 `json:"size"`
}

// NamedGroup is a host group known to the administrator's platform
// configuration. Names are used only for display, privilege warnings and
// profile roles; GIDs are never assumed from names.
type NamedGroup struct {
	Name string `json:"name"`
	GID  uint32 `json:"gid"`
}

// PlatformContext is the administrator-supplied identity context of one
// registered platform. MappingKnown false means CORE cannot tell how
// container IDs relate to host IDs.
type PlatformContext struct {
	ID           string       `json:"id"`
	Kind         string       `json:"kind"` // docker | podman | kubernetes
	Mode         string       `json:"mode"` // rootless | rootful | cluster
	Defaults     Settings     `json:"defaults"`
	MappingKnown bool         `json:"mappingKnown"`
	UIDMap       []IDRange    `json:"uidMap,omitempty"`
	GIDMap       []IDRange    `json:"gidMap,omitempty"`
	Groups       []NamedGroup `json:"groups,omitempty"`
	// Roles maps profile roles ("media", "shared-storage", ...) to GIDs on this host.
	Roles map[string]uint32 `json:"roles,omitempty"`
}

// Observed is identity information a platform actually reported for a
// running workload. Fields a platform cannot report stay nil.
type Observed struct {
	UID                *uint32  `json:"uid,omitempty"`
	GID                *uint32  `json:"gid,omitempty"`
	SupplementalGroups []uint32 `json:"supplementalGroups,omitempty"`
	Source             string   `json:"source"` // e.g. "kubernetes-pod-template"
}

// mapID translates a container ID to the host ID using ranges; ok is false
// when the ID falls outside every range.
func mapID(id uint32, ranges []IDRange) (uint32, bool) {
	for _, r := range ranges {
		if r.Size > 0 && id >= r.ContainerID && uint64(id) < uint64(r.ContainerID)+uint64(r.Size) {
			return r.HostID + (id - r.ContainerID), true
		}
	}
	return 0, false
}

// ValidateRanges rejects empty, overflowing or overlapping container ranges.
func ValidateRanges(ranges []IDRange) error {
	for i, r := range ranges {
		if r.Size == 0 || uint64(r.ContainerID)+uint64(r.Size)-1 > uint64(MaxID) || uint64(r.HostID)+uint64(r.Size)-1 > uint64(MaxID) {
			return fmt.Errorf("id range %d is empty or exceeds the ID space", i)
		}
		for _, o := range ranges[:i] {
			if uint64(r.ContainerID) < uint64(o.ContainerID)+uint64(o.Size) && uint64(o.ContainerID) < uint64(r.ContainerID)+uint64(r.Size) {
				return fmt.Errorf("id range %d overlaps an earlier range", i)
			}
		}
	}
	return nil
}
