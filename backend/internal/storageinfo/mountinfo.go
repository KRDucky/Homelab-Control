// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storageinfo

import (
	"fmt"
	"io"
	"strconv"

	"homelab-control/backend/internal/mountinfo"
)

// ParseMountInfo parses bounded kernel mountinfo (shared parser:
// internal/mountinfo) without probing mountpoints or retaining source/options
// (which can contain network credentials). The storage graph needs every
// mount, so a skipped line or a reached bound fails the parse.
func ParseMountInfo(r io.Reader) ([]Mount, error) {
	recs, info, err := mountinfo.Parse(r)
	if err != nil {
		return nil, err
	}
	if info.Truncated {
		return nil, fmt.Errorf("mount inventory is malformed or exceeds its limits")
	}
	mounts := make([]Mount, 0, len(recs))
	for _, rec := range recs {
		if !validDeviceNumber(rec.DeviceNumber) {
			return nil, fmt.Errorf("invalid mount device number")
		}
		mounts = append(mounts, Mount{ID: strconv.FormatUint(rec.ID, 10), DeviceNumber: rec.DeviceNumber, Root: rec.Root,
			Mountpoint: rec.Mountpoint, FSType: rec.FSType, View: "service"})
	}
	return mounts, nil
}
