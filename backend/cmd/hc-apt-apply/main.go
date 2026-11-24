// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Command hc-apt-apply is the opt-in APT configuration helper (owner
// authorization 2026-11-24). It reads one JSON request from the socket,
// validates it again, and writes only the managed APT settings file or the
// archive URI of a vendor source file, after a backup. It executes no
// programs and has no network. It is not started unless
// /etc/homelab-control/apt-apply.enabled exists.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"homelab-control/backend/internal/aptapply"
	"homelab-control/backend/internal/safefile"
)

const (
	maxRequestBytes  = 64 << 10
	maxResponseBytes = 16 << 10
)

func main() {
	if err := run(os.Stdin, os.Stdout, "", time.Now); err != nil {
		fmt.Fprintln(os.Stderr, "hc-apt-apply:", err.Error())
		os.Exit(1)
	}
}

func run(in io.Reader, out io.Writer, root string, now func() time.Time) error {
	b, err := io.ReadAll(io.LimitReader(in, maxRequestBytes+1))
	if err != nil {
		return err
	}
	if len(b) > maxRequestBytes {
		return errors.New("request too large")
	}
	var req aptapply.Request
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return err
	}
	body, err := json.Marshal(execute(req, root, now))
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if len(body) > maxResponseBytes {
		return errors.New("response too large")
	}
	_, err = out.Write(body)
	return err
}

func result(ok bool, msg string) aptapply.Result {
	return aptapply.Result{Version: aptapply.ProtocolVersion, OK: ok, Message: msg}
}

func under(root, abs string) string { return root + abs }

func execute(req aptapply.Request, root string, now func() time.Time) aptapply.Result {
	if err := aptapply.Validate(req); err != nil {
		return result(false, "request refused")
	}
	target := under(root, req.Target())
	old, err := safefile.Read(target, aptapply.MaxFileBytes)
	exists := err == nil
	switch {
	case err != nil && !os.IsNotExist(err):
		return result(false, "the target file cannot be read safely (symlink, not a regular file or too large)")
	case exists && !ownedByRoot(target):
		return result(false, "the target file is not owned by root")
	}
	cur := ""
	if exists {
		cur = aptapply.Digest(old)
	}
	if cur != req.Digest {
		return result(false, "the file changed since the plan was made; nothing was written")
	}
	var next []byte
	changed := 1
	switch req.Action {
	case aptapply.ActionConfWrite:
		s, err := aptapply.Render(req.Tunables)
		if err != nil {
			return result(false, "request refused")
		}
		next = []byte(s)
	case aptapply.ActionConfRemove:
		if !exists {
			return result(false, "the managed settings file does not exist")
		}
	case aptapply.ActionMirrorSet:
		if !exists {
			return result(false, "the source file does not exist")
		}
		d, err := aptapply.ReadOSRelease(root)
		if err != nil {
			return result(false, "os-release cannot be read")
		}
		s, n, err := aptapply.SetMirror(req.File, string(old), d, req.From, req.To)
		if err != nil {
			return result(false, "the source file cannot be edited safely; nothing was written")
		}
		next, changed = []byte(s), n
	}
	res := result(true, "")
	res.Changed = changed
	if exists {
		name, err := backup(root, req.Target(), old, now())
		if err != nil {
			return result(false, "the backup could not be written; nothing was changed")
		}
		res.Backup = name
	}
	if req.Action == aptapply.ActionConfRemove {
		if err := os.Remove(target); err != nil {
			return result(false, "the managed settings file could not be removed")
		}
		if _, err := os.Lstat(target); !os.IsNotExist(err) {
			return result(false, "the managed settings file is still present")
		}
		res.Message = "The managed settings file was removed; APT uses the rest of its configuration."
		return res
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return result(false, "the target directory is missing")
	}
	if err := safefile.Write(target, next, 0o644); err != nil {
		return result(false, "the file could not be written; the previous content is unchanged")
	}
	got, err := safefile.Read(target, aptapply.MaxFileBytes)
	if err != nil || !bytes.Equal(got, next) {
		return result(false, "the written file does not read back as planned; restore it from the backup")
	}
	res.Digest = aptapply.Digest(next)
	res.Message = "Written."
	return res
}

// rootUID is the owner a target file must have; tests set their own uid.
var rootUID uint32

func ownedByRoot(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Uid == rootUID
}

// backup writes old to BackupDir/<base>.<UTC time>.bak (0600) and keeps the
// newest KeepBackups backups of that file. It returns the backup file name.
func backup(root, target string, old []byte, t time.Time) (string, error) {
	dir := under(root, aptapply.BackupDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	base := filepath.Base(target)
	name := base + "." + t.UTC().Format("20060102T150405.000000000Z") + ".bak"
	if err := safefile.Write(filepath.Join(dir, name), old, 0o600); err != nil {
		return "", err
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return name, nil
	}
	var mine []string
	for _, e := range ents {
		if e.Type().IsRegular() && strings.HasPrefix(e.Name(), base+".") && strings.HasSuffix(e.Name(), ".bak") {
			mine = append(mine, e.Name())
		}
	}
	sort.Strings(mine)
	for len(mine) > aptapply.KeepBackups {
		_ = os.Remove(filepath.Join(dir, mine[0]))
		mine = mine[1:]
	}
	return name, nil
}
