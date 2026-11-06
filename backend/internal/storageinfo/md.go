// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storageinfo

import (
	"strconv"
	"strings"
)

// MDState holds Linux md (mdraid) runtime attributes as the kernel publishes
// them in world-readable sysfs (Documentation/admin-guide/md.rst). Values are
// raw observations. This package never derives "healthy" from them; a missing
// attribute stays absent rather than defaulting to a good state.
type MDState struct {
	Level         string     `json:"level"`
	ArrayState    string     `json:"array_state,omitempty"`
	RaidDisks     *uint64    `json:"raid_disks,omitempty"`
	Degraded      *uint64    `json:"degraded,omitempty"`
	SyncAction    string     `json:"sync_action,omitempty"`
	SyncCompleted string     `json:"sync_completed,omitempty"`
	MismatchCount *uint64    `json:"mismatch_count,omitempty"`
	Members       []MDMember `json:"members"`
	Source        string     `json:"source"`
}

// MDMember is one md/dev-* entry. DeviceNumber is the member's kernel
// major:minor, read through the dev-*/block link, never a guessed name.
type MDMember struct {
	DeviceNumber string `json:"device_number,omitempty"`
	Slot         string `json:"slot,omitempty"`
	State        string `json:"state,omitempty"`
}

func mdUint(s string) *uint64 {
	if n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64); err == nil {
		return &n
	}
	return nil
}

// mdToken accepts a short kernel keyword or "N / M" progress value. Anything
// else is dropped, so arbitrary sysfs content is not relayed to clients.
func mdToken(s string) string {
	s = strings.TrimSpace(s)
	if len(s) == 0 || len(s) > 64 {
		return ""
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == ' ' || c == '/' || c == ',') {
			return ""
		}
	}
	return s
}
