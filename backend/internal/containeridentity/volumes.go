// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package containeridentity

import (
	"fmt"
	"slices"
)

// Access is the access a volume needs.
type Access string

const (
	AccessRead      Access = "ro"
	AccessReadWrite Access = "rw"
)

// Volume describes a mount the workload needs. Host is ownership metadata
// observed by a trusted host inspector; client-supplied host metadata must
// never be passed here.
type Volume struct {
	Name   string        `json:"name"`
	Target string        `json:"target"`
	Access Access        `json:"access"`
	Host   *HostPathInfo `json:"-"`
}

// HostPathInfo is observed ownership of a bind source (or volume root).
type HostPathInfo struct {
	Directory bool   `json:"directory"`
	OwnerUID  uint32 `json:"ownerUid"`
	OwnerGID  uint32 `json:"ownerGid"`
	Mode      uint32 `json:"mode"` // permission bits only
	// ExtendedACL means POSIX ACLs exist; mode bits alone cannot decide access.
	ExtendedACL bool `json:"extendedAcl"`
}

// VolumeStatus of a compatibility check.
type VolumeStatus string

const (
	VolumeCompatible   VolumeStatus = "compatible"
	VolumeIncompatible VolumeStatus = "incompatible"
	VolumeUnverified   VolumeStatus = "unverified"
)

// VolumeResult never contains host paths: only the volume name and target.
type VolumeResult struct {
	Name   string       `json:"name"`
	Target string       `json:"target"`
	Access Access       `json:"access"`
	Status VolumeStatus `json:"status"`
	Reason string       `json:"reason"`
	// Proposals are human-reviewable host changes. They are never executed:
	// HomeControl does not chmod/chown host paths to make an install work.
	Proposals []string `json:"proposals,omitempty"`
}

func checkVolumes(req Request, r Resolved, add func(Severity, string, string, string, ...any)) []VolumeResult {
	out := []VolumeResult{}
	for _, v := range req.Volumes {
		res := VolumeResult{Name: v.Name, Target: v.Target, Access: v.Access}
		switch {
		case v.Access != AccessRead && v.Access != AccessReadWrite:
			res.Status, res.Reason = VolumeIncompatible, "Access must be ro or rw."
		case v.Host == nil:
			res.Status, res.Reason = VolumeUnverified, "Host ownership is not observable from CORE for this volume; no host inspection was performed."
		case v.Host.ExtendedACL:
			res.Status, res.Reason = VolumeUnverified, "The host path has extended ACLs; mode bits alone cannot determine access."
		case r.UID.Host == nil || r.GID.Host == nil:
			res.Status, res.Reason = VolumeUnverified, "The effective host UID/GID is unknown (image default identity or unknown user-namespace map)."
		default:
			res = evaluate(v, r, req.Platform)
		}
		if res.Status == VolumeIncompatible {
			add(SeverityWarning, "volume_access", "volumes", "Volume %q at %s: %s", v.Name, v.Target, res.Reason)
		}
		out = append(out, res)
	}
	if req.Profile == "read-only-monitoring" {
		for _, v := range req.Volumes {
			if v.Access == AccessReadWrite {
				add(SeverityWarning, "profile_volume_rw", "volumes", "The read-only monitoring profile expects read-only volumes; %q is read-write.", v.Name)
			}
		}
	}
	return out
}

func evaluate(v Volume, r Resolved, pc PlatformContext) VolumeResult {
	res := VolumeResult{Name: v.Name, Target: v.Target, Access: v.Access}
	uid, gid := *r.UID.Host, *r.GID.Host
	groups := append([]uint32{gid}, r.SupplementalGroups.Host...)
	h := v.Host
	var bits uint32
	class := "other"
	switch {
	case h.OwnerUID == uid:
		bits, class = (h.Mode>>6)&7, "owner"
	case slices.Contains(groups, h.OwnerGID):
		bits, class = (h.Mode>>3)&7, "group"
	default:
		bits = h.Mode & 7
	}
	need := uint32(4)
	if v.Access == AccessReadWrite {
		need |= 2
	}
	if h.Directory {
		need |= 1
	}
	if bits&need == need {
		res.Status = VolumeCompatible
		res.Reason = fmt.Sprintf("The workload's host identity has the required access through the %s permission class.", class)
		return res
	}
	res.Status = VolumeIncompatible
	res.Reason = fmt.Sprintf("Access through the %s permission class is %s but %s is required.", class, rwx(bits), rwx(need))
	groupBits := (h.Mode >> 3) & 7
	if class == "other" && groupBits&need == need {
		res.Proposals = append(res.Proposals, fmt.Sprintf("Add the directory's group (container GID mapping to host GID %d) as a supplemental group, after confirming it is not privileged.", h.OwnerGID))
	}
	res.Proposals = append(res.Proposals,
		"Choose a PUID/PGID that owns or is in the group of this path, or",
		fmt.Sprintf("ask an administrator to grant %s to the workload's host identity through a reviewed, approved host change (HomeControl does not change host ownership or modes).", rwx(need)))
	return res
}

func rwx(b uint32) string {
	s := []byte("---")
	if b&4 != 0 {
		s[0] = 'r'
	}
	if b&2 != 0 {
		s[1] = 'w'
	}
	if b&1 != 0 {
		s[2] = 'x'
	}
	return string(s)
}
