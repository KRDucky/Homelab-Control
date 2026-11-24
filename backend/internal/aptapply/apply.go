// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package aptapply

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// ProtocolVersion is the broker ↔ hc-apt-apply request version.
const ProtocolVersion = 1

// Helper actions.
const (
	ActionConfWrite  = "conf.write"
	ActionConfRemove = "conf.remove"
	ActionMirrorSet  = "mirror.set"
)

// Paths the helper owns besides the vendor files.
const (
	OSRelease = "/etc/os-release"
	BackupDir = "/var/lib/homelab-control-apt-apply/backups"
	// KeepBackups is how many backups per file are kept.
	KeepBackups = 10
	// MaxFileBytes bounds every file the helper reads or writes.
	MaxFileBytes = 256 << 10
)

// Request is one helper call. Digest is the SHA-256 of the target file as the
// plan saw it ("" when the file did not exist); the helper refuses when the
// file changed since then.
type Request struct {
	Version  int      `json:"version"`
	Action   string   `json:"action"`
	Tunables Tunables `json:"tunables,omitempty"`
	File     string   `json:"file,omitempty"`
	From     string   `json:"from,omitempty"`
	To       string   `json:"to,omitempty"`
	Digest   string   `json:"digest"`
}

// Result is the helper's answer.
type Result struct {
	Version int    `json:"version"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	Changed int    `json:"changed,omitempty"`
	Backup  string `json:"backup,omitempty"`
	Digest  string `json:"digest,omitempty"`
}

// Target is the file a request writes.
func (r Request) Target() string {
	if r.Action == ActionMirrorSet {
		return r.File
	}
	return ConfPath
}

// Validate checks a request's shape. The helper calls it again; it does not
// trust the broker's check.
func Validate(r Request) error {
	if r.Version != ProtocolVersion {
		return fmt.Errorf("%w: protocol version", ErrInvalid)
	}
	if r.Digest != "" && (len(r.Digest) != 64 || strings.Trim(r.Digest, "0123456789abcdef") != "") {
		return fmt.Errorf("%w: digest", ErrInvalid)
	}
	switch r.Action {
	case ActionConfWrite:
		if r.File != "" || r.From != "" || r.To != "" {
			return fmt.Errorf("%w: unexpected fields", ErrInvalid)
		}
		return r.Tunables.Validate()
	case ActionConfRemove:
		if r.File != "" || r.From != "" || r.To != "" || r.Tunables != (Tunables{}) {
			return fmt.Errorf("%w: unexpected fields", ErrInvalid)
		}
		return nil
	case ActionMirrorSet:
		if !IsVendorFile(r.File) || r.Digest == "" || r.Tunables != (Tunables{}) {
			return fmt.Errorf("%w: mirror request", ErrInvalid)
		}
		if err := ValidCurrentURI(r.From); err != nil {
			return err
		}
		if err := ValidMirrorURI(r.To); err != nil {
			return err
		}
		if r.From == r.To {
			return fmt.Errorf("%w: the mirror is already in use", ErrInvalid)
		}
		return nil
	default:
		return fmt.Errorf("%w: action", ErrInvalid)
	}
}

// ReadOSRelease reads os-release under root (normally a symlink to
// /usr/lib/os-release, so the link is followed) and parses it.
func ReadOSRelease(root string) (Distro, error) {
	f, err := os.Open(root + OSRelease)
	if err != nil {
		return Distro{}, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 64<<10))
	if err != nil {
		return Distro{}, err
	}
	return ParseOSRelease(string(b)), nil
}

// ParseOSRelease returns ID and VERSION_CODENAME (falling back to
// UBUNTU_CODENAME) from os-release content.
func ParseOSRelease(text string) Distro {
	var d Distro
	ubuntu := ""
	for _, ln := range strings.Split(text, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(ln), "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"'`)
		switch k {
		case "ID":
			d.ID = v
		case "VERSION_CODENAME":
			d.Codename = v
		case "UBUNTU_CODENAME":
			ubuntu = v
		}
	}
	if d.Codename == "" {
		d.Codename = ubuntu
	}
	if !codename.MatchString(d.Codename) {
		d.Codename = ""
	}
	return d
}
