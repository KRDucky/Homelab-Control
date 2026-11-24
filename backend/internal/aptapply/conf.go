// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package aptapply holds the typed APT tunables and vendor source edits that
// the hc-apt-apply root helper may write (owner authorization 2026-11-24).
// Everything here is pure: validation, rendering, parsing and editing of
// file content. The helper does the file I/O.
package aptapply

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ConfPath is the only APT configuration file Homelab Control writes.
const ConfPath = "/etc/apt/apt.conf.d/60homelab-control"

const confHeader = "// Managed by Homelab Control (Software Center > APT settings).\n" +
	"// Changes made here are replaced the next time the settings are applied there.\n"

// Tunables is the closed set of APT settings. A nil field is not written, so
// APT uses the value from the rest of its configuration.
type Tunables struct {
	InstallRecommends *bool  `json:"install_recommends,omitempty"`
	InstallSuggests   *bool  `json:"install_suggests,omitempty"`
	SkipTranslations  *bool  `json:"skip_translations,omitempty"`
	ForceIPv4         *bool  `json:"force_ipv4,omitempty"`
	PDiffs            *bool  `json:"pdiffs,omitempty"`
	KeepDownloaded    *bool  `json:"keep_downloaded,omitempty"`
	Retries           *int   `json:"retries,omitempty"`
	TimeoutSeconds    *int   `json:"timeout_seconds,omitempty"`
	QueueMode         string `json:"queue_mode,omitempty"`
	HTTPProxy         string `json:"http_proxy,omitempty"`
}

// Limits for the numeric and text tunables.
const (
	MinRetries, MaxRetries = 0, 10
	MinTimeout, MaxTimeout = 5, 300
	maxProxyLen            = 200
)

var (
	// ErrInvalid is returned for any value outside the closed set.
	ErrInvalid = errors.New("invalid APT setting")
	proxyHost  = regexp.MustCompile(`^[A-Za-z0-9.-]{1,253}$|^\[[0-9A-Fa-f:.]{2,45}\]$`)
)

// Validate refuses any value that is outside the closed set or that could
// change the meaning of the rendered file.
func (t Tunables) Validate() error {
	if t.Retries != nil && (*t.Retries < MinRetries || *t.Retries > MaxRetries) {
		return fmt.Errorf("%w: retries must be %d to %d", ErrInvalid, MinRetries, MaxRetries)
	}
	if t.TimeoutSeconds != nil && (*t.TimeoutSeconds < MinTimeout || *t.TimeoutSeconds > MaxTimeout) {
		return fmt.Errorf("%w: timeout must be %d to %d seconds", ErrInvalid, MinTimeout, MaxTimeout)
	}
	switch t.QueueMode {
	case "", "host", "access":
	default:
		return fmt.Errorf("%w: queue mode must be host or access", ErrInvalid)
	}
	if t.HTTPProxy != "" {
		if err := ValidProxy(t.HTTPProxy); err != nil {
			return err
		}
	}
	return nil
}

// ValidProxy accepts only http(s)://host[:port][/] with no credentials, query
// or fragment, so the value can never carry a secret or APT syntax.
func ValidProxy(s string) error {
	bad := fmt.Errorf("%w: the proxy must be http://host:port or https://host:port without a user name or password", ErrInvalid)
	if len(s) > maxProxyLen || strings.ContainsAny(s, "\"';{}\\ \t\r\n") {
		return bad
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") {
		return bad
	}
	host := u.Host
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return bad
		}
		host = strings.TrimSuffix(host, ":"+p)
	}
	if !proxyHost.MatchString(host) {
		return bad
	}
	return nil
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// Render returns the managed file for t. It never copies free text into APT
// syntax: every value is a validated boolean, integer, keyword or URL.
func Render(t Tunables) (string, error) {
	if err := t.Validate(); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(confHeader)
	line := func(key, val string) { fmt.Fprintf(&b, "%s \"%s\";\n", key, val) }
	if t.InstallRecommends != nil {
		line("APT::Install-Recommends", boolStr(*t.InstallRecommends))
	}
	if t.InstallSuggests != nil {
		line("APT::Install-Suggests", boolStr(*t.InstallSuggests))
	}
	if t.SkipTranslations != nil && *t.SkipTranslations {
		line("Acquire::Languages", "none")
	}
	if t.ForceIPv4 != nil {
		line("Acquire::ForceIPv4", boolStr(*t.ForceIPv4))
	}
	if t.PDiffs != nil {
		line("Acquire::PDiffs", boolStr(*t.PDiffs))
	}
	if t.KeepDownloaded != nil {
		line("Binary::apt::APT::Keep-Downloaded-Packages", boolStr(*t.KeepDownloaded))
	}
	if t.Retries != nil {
		line("Acquire::Retries", strconv.Itoa(*t.Retries))
	}
	if t.TimeoutSeconds != nil {
		line("Acquire::http::Timeout", strconv.Itoa(*t.TimeoutSeconds))
		line("Acquire::https::Timeout", strconv.Itoa(*t.TimeoutSeconds))
	}
	if t.QueueMode != "" {
		line("Acquire::Queue-Mode", t.QueueMode)
	}
	if t.HTTPProxy != "" {
		line("Acquire::http::Proxy", t.HTTPProxy)
	}
	return b.String(), nil
}

// Effective is the value APT reports for each tunable, from apt-config dump.
type Effective struct {
	InstallRecommends bool   `json:"install_recommends"`
	InstallSuggests   bool   `json:"install_suggests"`
	SkipTranslations  bool   `json:"skip_translations"`
	ForceIPv4         bool   `json:"force_ipv4"`
	PDiffs            bool   `json:"pdiffs"`
	KeepDownloaded    bool   `json:"keep_downloaded"`
	Retries           int    `json:"retries"`
	TimeoutSeconds    int    `json:"timeout_seconds"`
	QueueMode         string `json:"queue_mode"`
	HTTPProxy         bool   `json:"http_proxy_set"`
}

// APT's compiled-in defaults for keys apt-config dump may not print.
func defaultEffective() Effective {
	return Effective{InstallRecommends: true, PDiffs: true, KeepDownloaded: false, Retries: 0, TimeoutSeconds: 120, QueueMode: "host"}
}

var dumpLine = regexp.MustCompile(`^([A-Za-z0-9:._-]+) "([^"]*)";$`)

func aptBool(v string) bool {
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on", "enable":
		return true
	}
	return false
}

// ParseDump reads `apt-config dump` output and returns the effective values
// of the tunables. Lines it does not need are ignored; a malformed line for
// a tunable key fails.
func ParseDump(out string) (Effective, error) {
	e := defaultEffective()
	var langs []string
	for _, raw := range strings.Split(out, "\n") {
		ln := strings.TrimSpace(raw)
		if ln == "" {
			continue
		}
		m := dumpLine.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		k, v := m[1], m[2]
		switch k {
		case "APT::Install-Recommends":
			e.InstallRecommends = aptBool(v)
		case "APT::Install-Suggests":
			e.InstallSuggests = aptBool(v)
		case "Acquire::Languages":
			// A list prints as `Acquire::Languages "";` then one
			// `Acquire::Languages:: "x";` line per item.
			langs = nil
			if v != "" {
				langs = []string{v}
			}
		case "Acquire::Languages::":
			langs = append(langs, v)
		case "Acquire::ForceIPv4":
			e.ForceIPv4 = aptBool(v)
		case "Acquire::PDiffs":
			e.PDiffs = aptBool(v)
		case "Binary::apt::APT::Keep-Downloaded-Packages":
			e.KeepDownloaded = aptBool(v)
		case "Acquire::Retries":
			n, err := strconv.Atoi(v)
			if err != nil {
				return Effective{}, fmt.Errorf("apt-config: bad %s", k)
			}
			e.Retries = n
		case "Acquire::http::Timeout":
			n, err := strconv.Atoi(v)
			if err != nil {
				return Effective{}, fmt.Errorf("apt-config: bad %s", k)
			}
			e.TimeoutSeconds = n
		case "Acquire::Queue-Mode":
			e.QueueMode = v
		case "Acquire::http::Proxy":
			e.HTTPProxy = v != "" && !strings.EqualFold(v, "DIRECT")
		}
	}
	e.SkipTranslations = len(langs) == 1 && langs[0] == "none"
	return e, nil
}

// Overrides lists the tunables in t whose effective value differs, which
// means a later configuration file sets them.
func Overrides(t Tunables, e Effective) []string {
	var out []string
	chk := func(name string, set bool, ok bool) {
		if set && !ok {
			out = append(out, name)
		}
	}
	chk("install_recommends", t.InstallRecommends != nil, t.InstallRecommends == nil || *t.InstallRecommends == e.InstallRecommends)
	chk("install_suggests", t.InstallSuggests != nil, t.InstallSuggests == nil || *t.InstallSuggests == e.InstallSuggests)
	chk("skip_translations", t.SkipTranslations != nil && *t.SkipTranslations, e.SkipTranslations)
	chk("force_ipv4", t.ForceIPv4 != nil, t.ForceIPv4 == nil || *t.ForceIPv4 == e.ForceIPv4)
	chk("pdiffs", t.PDiffs != nil, t.PDiffs == nil || *t.PDiffs == e.PDiffs)
	chk("keep_downloaded", t.KeepDownloaded != nil, t.KeepDownloaded == nil || *t.KeepDownloaded == e.KeepDownloaded)
	chk("retries", t.Retries != nil, t.Retries == nil || *t.Retries == e.Retries)
	chk("timeout_seconds", t.TimeoutSeconds != nil, t.TimeoutSeconds == nil || *t.TimeoutSeconds == e.TimeoutSeconds)
	chk("queue_mode", t.QueueMode != "", t.QueueMode == "" || t.QueueMode == e.QueueMode)
	chk("http_proxy", t.HTTPProxy != "", e.HTTPProxy)
	sort.Strings(out)
	return out
}

// ParseManaged reads back a file written by Render. It is used to show which
// settings Homelab Control manages; ok is false when the file was changed by
// hand into something Render would not produce.
func ParseManaged(text string) (t Tunables, ok bool) {
	ok = true
	for _, raw := range strings.Split(text, "\n") {
		ln := strings.TrimSpace(raw)
		if ln == "" || strings.HasPrefix(ln, "//") {
			continue
		}
		parts := strings.SplitN(ln, " ", 2)
		if len(parts) != 2 || !strings.HasPrefix(parts[1], "\"") || !strings.HasSuffix(parts[1], "\";") {
			ok = false
			continue
		}
		v := strings.TrimSuffix(strings.TrimPrefix(parts[1], "\""), "\";")
		b := v == "true"
		isBool := v == "true" || v == "false"
		n, nerr := strconv.Atoi(v)
		switch parts[0] {
		case "APT::Install-Recommends":
			if isBool {
				t.InstallRecommends = &b
			} else {
				ok = false
			}
		case "APT::Install-Suggests":
			if isBool {
				t.InstallSuggests = &b
			} else {
				ok = false
			}
		case "Acquire::Languages":
			on := v == "none"
			t.SkipTranslations = &on
			ok = ok && on
		case "Acquire::ForceIPv4":
			if isBool {
				t.ForceIPv4 = &b
			} else {
				ok = false
			}
		case "Acquire::PDiffs":
			if isBool {
				t.PDiffs = &b
			} else {
				ok = false
			}
		case "Binary::apt::APT::Keep-Downloaded-Packages":
			if isBool {
				t.KeepDownloaded = &b
			} else {
				ok = false
			}
		case "Acquire::Retries":
			if nerr == nil {
				t.Retries = &n
			} else {
				ok = false
			}
		case "Acquire::http::Timeout":
			if nerr == nil {
				t.TimeoutSeconds = &n
			} else {
				ok = false
			}
		case "Acquire::https::Timeout":
		case "Acquire::Queue-Mode":
			t.QueueMode = v
		case "Acquire::http::Proxy":
			t.HTTPProxy = v
		default:
			ok = false
		}
	}
	if ok && t.Validate() != nil {
		ok = false
	}
	return t, ok
}
