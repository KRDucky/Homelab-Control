// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package containeridentity

import "fmt"

// Profile is a reusable permission arrangement. Profiles never contain host
// IDs: group needs are expressed as roles that each platform maps to its own
// GIDs (PlatformContext.Roles). PUID/PGID are never set by a profile.
type Profile struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Umask       *Umask   `json:"umask,omitempty"`
	GroupRoles  []string `json:"groupRoles"`
	ReadOnly    bool     `json:"readOnlyVolumes"`
}

func mask(v Umask) *Umask { return &v }

var profiles = []Profile{
	{ID: "media-application", Name: "Media application", Description: "Group-writable files for shared media libraries (UMASK 002) and the platform's media role group.", Umask: mask(0o002), GroupRoles: []string{"media"}},
	{ID: "shared-storage", Name: "Shared storage access", Description: "Group-writable files (UMASK 002) and the platform's shared-storage role group.", Umask: mask(0o002), GroupRoles: []string{"shared-storage"}},
	{ID: "backup-service", Name: "Backup service", Description: "Private output (UMASK 077) with the platform's backup-read role group for reading sources.", Umask: mask(0o077), GroupRoles: []string{"backup-read"}},
	{ID: "read-only-monitoring", Name: "Read-only monitoring service", Description: "Private files (UMASK 077), no extra groups, read-only volumes expected.", Umask: mask(0o077), GroupRoles: []string{}, ReadOnly: true},
}

// Profiles returns the built-in profiles.
func Profiles() []Profile {
	out := make([]Profile, len(profiles))
	copy(out, profiles)
	return out
}

func ProfileByID(id string) (Profile, bool) {
	for _, p := range profiles {
		if p.ID == id {
			return p, true
		}
	}
	return Profile{}, false
}

// settings converts the profile into a layer for one platform. Roles the
// platform has not mapped are reported, not guessed.
func (p Profile) settings(pc PlatformContext, add func(Severity, string, string, string, ...any)) Settings {
	s := Settings{Umask: p.Umask}
	groups := []uint32{}
	for _, role := range p.GroupRoles {
		gid, ok := pc.Roles[role]
		if !ok {
			add(SeverityWarning, "profile_role_unmapped", "profile", "Profile %q needs the %q group role, which this platform does not map to a GID; no group is added for it.", p.ID, role)
			continue
		}
		groups = append(groups, gid)
	}
	if len(p.GroupRoles) > 0 || p.ReadOnly {
		s.SupplementalGroups = &groups
	}
	if p.ReadOnly {
		add(SeverityInfo, "profile_read_only", "profile", fmt.Sprintf("Profile %q expects every volume to be mounted read-only.", p.ID))
	}
	return s
}
