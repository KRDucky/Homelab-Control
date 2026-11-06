// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package hostinfo

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Host settings (M2-6 skeleton): hostname (kernel, static, pretty) and
// timezone, observed with unprivileged file reads only. This file never
// writes to the host and never runs a program; changing these settings is
// not implemented (see docs/m2/M2-6-HOST-SETTINGS-SKELETON.md).

// Observation status of one host setting.
const (
	SettingObserved    = "observed"    // read from the host
	SettingNotSet      = "not-set"     // the file that holds it is absent or empty
	SettingUnavailable = "unavailable" // could not be read; the value is unknown
)

// Setting is one observed value with its source. Value is empty unless
// Status is "observed".
type Setting struct {
	Value  string `json:"value,omitempty"`
	Status string `json:"status"`
	Source string `json:"source"`
	Note   string `json:"note,omitempty"`
}

// HostSettings is the read-only response for GET /api/v1/host/settings.
type HostSettings struct {
	KernelHostname Setting `json:"kernel_hostname"`
	StaticHostname Setting `json:"static_hostname"`
	PrettyHostname Setting `json:"pretty_hostname"`
	Timezone       Setting `json:"timezone"`
}

// kernelHostnameNote: the packaged hc-api unit sets ProtectHostname=yes, so
// the service has its own UTS namespace initialised from the host when it
// started. A later change on the host is not visible until hc-api restarts.
const kernelHostnameNote = "As seen by the Homelab Control service; with ProtectHostname=yes this is the host name when the service started."

// HostSettings observes the current host settings beneath c.Roots.
func (c *Collector) HostSettings() HostSettings { return ReadHostSettings(c.Roots) }

// ReadHostSettings reads hostname and timezone settings beneath roots.
func ReadHostSettings(roots Roots) HostSettings {
	var hs HostSettings
	p := filepath.Join(roots.Proc, "sys/kernel/hostname")
	hs.KernelHostname = Setting{Source: p, Note: kernelHostnameNote}
	if v, err := readTrim(p); err != nil || v == "" {
		hs.KernelHostname.Status = SettingUnavailable
	} else {
		hs.KernelHostname.Status, hs.KernelHostname.Value = SettingObserved, v
	}

	p = filepath.Join(roots.Etc, "hostname")
	hs.StaticHostname = Setting{Source: p}
	switch b, err := readSmall(p); {
	case errors.Is(err, fs.ErrNotExist):
		hs.StaticHostname.Status, hs.StaticHostname.Note = SettingNotSet, "No /etc/hostname: no static host name is configured."
	case err != nil:
		hs.StaticHostname.Status = SettingUnavailable
	default:
		if v := firstHostnameLine(b); v == "" {
			hs.StaticHostname.Status, hs.StaticHostname.Note = SettingNotSet, "/etc/hostname is empty."
		} else {
			hs.StaticHostname.Status, hs.StaticHostname.Value = SettingObserved, v
		}
	}

	p = filepath.Join(roots.Etc, "machine-info")
	hs.PrettyHostname = Setting{Source: p}
	switch b, err := readSmall(p); {
	case errors.Is(err, fs.ErrNotExist):
		hs.PrettyHostname.Status, hs.PrettyHostname.Note = SettingNotSet, "No /etc/machine-info: no pretty host name is configured."
	case err != nil:
		hs.PrettyHostname.Status = SettingUnavailable
	default:
		if v := parseKV(b)["PRETTY_HOSTNAME"]; v == "" {
			hs.PrettyHostname.Status = SettingNotSet
		} else if ValidPrettyHostname(v) != "" {
			hs.PrettyHostname.Status, hs.PrettyHostname.Note = SettingUnavailable, "PRETTY_HOSTNAME contains characters that are not shown."
		} else {
			hs.PrettyHostname.Status, hs.PrettyHostname.Value = SettingObserved, v
		}
	}

	hs.Timezone = readTimezone(roots)
	return hs
}

// firstHostnameLine returns the first non-empty, non-comment line, as
// systemd reads /etc/hostname.
func firstHostnameLine(b []byte) string {
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l != "" && !strings.HasPrefix(l, "#") {
			return l
		}
	}
	return ""
}

// ZoneinfoDir is the tz database beneath the root that holds etcRoot.
func ZoneinfoDir(etcRoot string) string {
	return filepath.Join(filepath.Dir(etcRoot), "usr/share/zoneinfo")
}

func readTimezone(roots Roots) Setting {
	p := filepath.Join(roots.Etc, "localtime")
	s := Setting{Source: p}
	target, err := os.Readlink(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		s.Status, s.Note = SettingNotSet, "No /etc/localtime: programs on this host use UTC."
		return s
	case err == nil:
		if i := strings.LastIndex(target, "zoneinfo/"); i >= 0 {
			if name := target[i+len("zoneinfo/"):]; ValidTimezoneName(name) == "" {
				s.Status, s.Value = SettingObserved, name
				return s
			}
		}
		s.Status, s.Note = SettingUnavailable, "/etc/localtime does not point into the time zone database."
		return s
	}
	// Not a symbolic link (a copied zone file): Debian-family hosts also
	// record the name in /etc/timezone.
	tz := filepath.Join(roots.Etc, "timezone")
	if v, err := readTrim(tz); err == nil && ValidTimezoneName(v) == "" {
		return Setting{Value: v, Status: SettingObserved, Source: tz, Note: "/etc/localtime is a copy, not a link; the name comes from /etc/timezone."}
	}
	s.Status, s.Note = SettingUnavailable, "/etc/localtime is not a link into the time zone database, so its zone name is unknown."
	return s
}

// hostnameLabel is one RFC 1123 label in lower case.
var hostnameLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// ValidStaticHostname returns "" for an acceptable static host name, else
// the reason. The policy is deliberately stricter than systemd: lower-case
// RFC 1123 labels, at most 64 characters (HOST_NAME_MAX), no trailing dot.
func ValidStaticHostname(h string) string {
	if h == "" || len(h) > 64 {
		return "A static host name must be 1 to 64 characters."
	}
	for _, l := range strings.Split(h, ".") {
		if !hostnameLabel.MatchString(l) {
			return "Use lower-case letters, digits and hyphens, in dot-separated parts that do not start or end with a hyphen."
		}
	}
	return ""
}

// ValidPrettyHostname returns "" for an acceptable pretty host name ("" is
// allowed and means: remove it), else the reason.
func ValidPrettyHostname(h string) string {
	if len(h) > 255 {
		return "A pretty host name must be at most 255 bytes."
	}
	if !utf8.ValidString(h) {
		return "A pretty host name must be valid UTF-8."
	}
	for _, r := range h {
		if unicode.IsControl(r) {
			return "A pretty host name must not contain control characters or line breaks."
		}
	}
	return ""
}

var tzComponent = regexp.MustCompile(`^[A-Za-z0-9_+-]{1,64}$`)

// ValidTimezoneName checks the syntax of a tz database name ("Area/City").
// It returns "" when acceptable, else the reason. Existence is checked by
// TimezoneExists.
func ValidTimezoneName(name string) string {
	if name == "" || len(name) > 128 {
		return "A time zone name must be 1 to 128 characters."
	}
	for _, c := range strings.Split(name, "/") {
		if !tzComponent.MatchString(c) {
			return "A time zone name looks like Area/City and uses only letters, digits, '_', '+' and '-'."
		}
	}
	return ""
}

// TimezoneExists reports whether name is a compiled zone (a regular file
// starting with the TZif magic) in the tz database beneath etcRoot. The
// lookup is confined to the database directory with os.Root, so a symbolic
// link cannot lead elsewhere. It only reads.
func TimezoneExists(etcRoot, name string) (bool, error) {
	if ValidTimezoneName(name) != "" {
		return false, nil
	}
	root, err := os.OpenRoot(ZoneinfoDir(etcRoot))
	if err != nil {
		return false, err
	}
	defer root.Close()
	f, err := root.Open(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return false, err
	}
	magic := make([]byte, 4)
	if _, err := io.ReadFull(f, magic); err != nil {
		return false, nil
	}
	return string(magic) == "TZif", nil
}
