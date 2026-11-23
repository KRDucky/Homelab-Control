// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"homelab-control/backend/internal/broker/proto"
)

// systemExt4View is the production inspector. It stats one by-id path,
// reads mountinfo, swaps, and sysfs holders. It does not format.
type systemExt4View struct{}

func (systemExt4View) Inspect(ctx context.Context, env *Env, byID string) (Ext4DeviceFact, error) {
	if err := ctx.Err(); err != nil {
		return Ext4DeviceFact{}, &proto.Error{Code: proto.CodeTimeout, Message: "the disk check was cancelled"}
	}
	resolved, fi, err := resolveExt4ByID(env, byID)
	if err != nil {
		return Ext4DeviceFact{}, err
	}
	if fi.Mode()&os.ModeDevice == 0 || fi.Mode()&os.ModeCharDevice != 0 {
		return Ext4DeviceFact{}, nil
	}
	rdev, ok := linuxRdev(fi)
	if !ok {
		return Ext4DeviceFact{}, &proto.Error{Code: proto.CodePolicyDenied, Message: "the disk device number could not be read"}
	}
	mounted, holders, err := ext4DeviceBusy(env, filepath.Base(resolved), rdev)
	if err != nil {
		return Ext4DeviceFact{}, err
	}
	return Ext4DeviceFact{BlockDevice: true, Mounted: mounted, HasHolders: holders, Rdev: rdev}, nil
}

func resolveExt4ByID(env *Env, byID string) (string, os.FileInfo, error) {
	start := env.Roots.Path(byID)
	fi, err := os.Lstat(start)
	if err != nil {
		return "", nil, &proto.Error{Code: proto.CodeUnavailable, Message: "the disk is not visible to this broker; nothing was formatted"}
	}
	target := start
	if fi.Mode()&os.ModeSymlink != 0 {
		link, err := os.Readlink(start)
		if err != nil {
			return "", nil, &proto.Error{Code: proto.CodeUnavailable, Message: "the disk symlink could not be read; nothing was formatted"}
		}
		if filepath.IsAbs(link) {
			target = filepath.Join(env.Roots.Root, link)
		} else {
			target = filepath.Join(filepath.Dir(start), link)
		}
		target = filepath.Clean(target)
		fi, err = os.Lstat(target)
		if err != nil {
			return "", nil, &proto.Error{Code: proto.CodeUnavailable, Message: "the disk is not visible to this broker; nothing was formatted"}
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", nil, &proto.Error{Code: proto.CodePolicyDenied, Message: "the by-id path is not a single symlink to a block device"}
		}
	}
	devRoot := "/dev"
	if env.Roots.Root != "" {
		devRoot = filepath.Clean(filepath.Join(env.Roots.Root, "dev"))
	}
	rel, err := filepath.Rel(devRoot, target)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || strings.Contains(rel, string(filepath.Separator)) {
		return "", nil, &proto.Error{Code: proto.CodePolicyDenied, Message: "the disk does not resolve to one node under /dev"}
	}
	if refusedKernelDisk(rel) {
		return "", nil, &proto.Error{Code: proto.CodePolicyDenied, Message: "mapper, md, loop, and device-mapper nodes are not ext4 format targets"}
	}
	return target, fi, nil
}

func refusedKernelDisk(name string) bool {
	return strings.HasPrefix(name, "dm-") || strings.HasPrefix(name, "md") || strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "zd") || strings.Contains(name, ".")
}

func linuxRdev(fi os.FileInfo) (string, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return "", false
	}
	dev := uint64(st.Rdev)
	maj := (dev >> 8 & 0xfff) | (dev >> 32 & 0xfffff000)
	min := (dev & 0xff) | (dev >> 12 & 0xffffff00)
	if maj < 1 {
		return "", false
	}
	return strconv.FormatUint(maj, 10) + ":" + strconv.FormatUint(min, 10), true
}

func ext4DeviceBusy(env *Env, name, rdev string) (mounted, holders bool, err error) {
	mountinfo, err := readExt4Text(env.Roots.Path("/proc/self/mountinfo"))
	if err != nil {
		return false, false, ext4Unreadable("mounts")
	}
	swaps, err := readExt4Text(env.Roots.Path("/proc/swaps"))
	if err != nil {
		return false, false, ext4Unreadable("swap")
	}
	parts, err := ext4PartitionRdevs(env, name)
	if err != nil {
		return false, false, err
	}
	if ext4MountBusy(mountinfo, rdev, parts) {
		mounted = true
	}
	swapBusy, err := ext4SwapBusy(env, swaps, rdev, parts)
	if err != nil {
		return false, false, err
	}
	if swapBusy {
		mounted = true
	}
	holders, err = ext4HasHolders(env, rdev)
	if err != nil {
		return false, false, err
	}
	return mounted, holders, nil
}

func ext4Unreadable(what string) error {
	return &proto.Error{Code: proto.CodeUnavailable, Message: "the disk " + what + " could not be read; nothing was formatted"}
}

func readExt4Text(path string) (string, error) {
	b, err := readFixed(path, 1<<20)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func ext4MountBusy(mountinfo, self string, parts map[string]bool) bool {
	for _, line := range strings.Split(mountinfo, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if fields[2] == self || parts[fields[2]] {
			return true
		}
	}
	return false
}

func ext4SwapBusy(env *Env, body, self string, parts map[string]bool) (bool, error) {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 || (i == 0 && fields[0] == "Filename") {
			continue
		}
		name := fields[0]
		if name == self || parts[name] {
			return true, nil
		}
		if strings.Contains(name, ":") && !strings.Contains(name, "/") {
			continue
		}
		path := name
		if filepath.IsAbs(name) {
			path = env.Roots.Path(name)
		}
		fi, err := os.Lstat(path)
		if err != nil {
			return false, ext4Unreadable("swap")
		}
		rdev, ok := linuxRdev(fi)
		if ok && (rdev == self || parts[rdev]) {
			return true, nil
		}
	}
	return false, nil
}

func ext4PartitionRdevs(env *Env, name string) (map[string]bool, error) {
	dir := env.Roots.Path("/sys/block/" + name)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, ext4Unreadable("partitions")
	}
	out := map[string]bool{}
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := readFixed(filepath.Join(dir, e.Name(), "dev"), 64)
		if err != nil {
			continue
		}
		rdev := strings.TrimSpace(string(b))
		if rdev == "" {
			continue
		}
		n++
		if n > 256 {
			return nil, ext4Unreadable("partitions")
		}
		out[rdev] = true
	}
	return out, nil
}

func ext4HasHolders(env *Env, rdev string) (bool, error) {
	for _, sub := range []string{"holders", "slaves"} {
		dir := env.Roots.Path("/sys/dev/block/" + rdev + "/" + sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false, ext4Unreadable(sub)
		}
		if len(entries) > 0 {
			return true, nil
		}
	}
	return false, nil
}
