// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package aptapply

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// The vendor source files whose archive URIs a mirror change may edit. No
// other source file is ever written.
const (
	SourcesList   = "/etc/apt/sources.list"
	UbuntuSources = "/etc/apt/sources.list.d/ubuntu.sources"
	DebianSources = "/etc/apt/sources.list.d/debian.sources"
)

// VendorFiles lists the editable files in a fixed order.
var VendorFiles = []string{SourcesList, UbuntuSources, DebianSources}

// IsVendorFile reports whether path is one of VendorFiles.
func IsVendorFile(path string) bool {
	for _, f := range VendorFiles {
		if f == path {
			return true
		}
	}
	return false
}

// Roles of a source entry.
const (
	RoleArchive  = "archive"
	RoleSecurity = "security"
	RolePorts    = "ports"
	RoleOther    = "other"
)

// Entry is one deb822 stanza or one active one-line entry of a vendor file.
type Entry struct {
	File       string   `json:"file"`
	Format     string   `json:"format"` // one-line | deb822
	Line       int      `json:"line"`   // 1-based line of the entry (deb822: of the URIs field)
	Enabled    bool     `json:"enabled"`
	Types      []string `json:"types"`
	URI        string   `json:"uri"`
	Suites     []string `json:"suites"`
	Components []string `json:"components"`
	SignedBy   string   `json:"signed_by,omitempty"`
	Role       string   `json:"role"`
	Switchable bool     `json:"switchable"`
	Reason     string   `json:"reason,omitempty"`

	uris []string
	// byte span of the URI value in the file text
	uriStart, uriEnd int
}

// Distro is the vendor (debian or ubuntu) and release codename that the
// suites are classified against, from /etc/os-release.
type Distro struct {
	ID       string `json:"id"`
	Codename string `json:"codename"`
}

// ErrSources is returned for source content that cannot be edited safely.
var ErrSources = errors.New("APT source file cannot be edited safely")

// Digest is the SHA-256 of file content; a plan is bound to it.
func Digest(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

var (
	mirrorPath = regexp.MustCompile(`^[A-Za-z0-9._~/+-]*$`)
	codename   = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)
)

// ValidMirrorURI accepts http(s)://host[:port]/path with no credentials,
// query or fragment and only plain path characters.
func ValidMirrorURI(s string) error {
	bad := fmt.Errorf("%w: a mirror must be an http or https URL without a user name, query or fragment", ErrInvalid)
	if len(s) > 256 || strings.ContainsAny(s, " \t\r\n\"'#[]") {
		return bad
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" ||
		u.Fragment != "" || u.Opaque != "" || u.Host == "" || !mirrorPath.MatchString(u.Path) {
		return bad
	}
	host := u.Hostname()
	if !proxyHost.MatchString(host) && !proxyHost.MatchString("["+host+"]") {
		return bad
	}
	return nil
}

// mirrorList matches the mirror-list URI that Debian cloud images use for
// their archive (URIs: mirror+file:///etc/apt/mirrors/debian.list). It may
// be replaced by a plain mirror; it is never written.
var mirrorList = regexp.MustCompile(`^mirror\+file:///etc/apt/mirrors/[a-z0-9-]{1,32}\.list$`)

// ValidCurrentURI accepts what an archive entry may use now: a plain mirror
// or a Debian cloud image mirror list.
func ValidCurrentURI(s string) error {
	if mirrorList.MatchString(s) {
		return nil
	}
	return ValidMirrorURI(s)
}

// ParseSources parses one vendor file. Commented one-line entries and
// comments are skipped; their bytes are kept by SetMirror.
func ParseSources(file, text string, d Distro) ([]Entry, error) {
	if !IsVendorFile(file) {
		return nil, fmt.Errorf("%w: %s is not a vendor source file", ErrSources, file)
	}
	if strings.ContainsRune(text, '\x00') || strings.Contains(text, "\r") {
		return nil, fmt.Errorf("%w: unexpected characters", ErrSources)
	}
	var es []Entry
	var err error
	if strings.HasSuffix(file, ".sources") {
		es, err = parseDeb822(text)
	} else {
		es, err = parseOneLine(text)
	}
	if err != nil {
		return nil, err
	}
	for i := range es {
		es[i].File = file
		classify(&es[i], d)
	}
	return es, nil
}

// lineSpans returns [start,end) byte offsets of each line (without '\n').
func lineSpans(text string) [][2]int {
	var out [][2]int
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			out = append(out, [2]int{start, i})
			start = i + 1
		}
	}
	if start < len(text) {
		out = append(out, [2]int{start, len(text)})
	}
	return out
}

func parseOneLine(text string) ([]Entry, error) {
	var es []Entry
	for n, sp := range lineSpans(text) {
		ln := text[sp[0]:sp[1]]
		trim := strings.TrimLeft(ln, " \t")
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		off := sp[0] + (len(ln) - len(trim))
		// Strip a trailing comment.
		body := trim
		if i := strings.IndexByte(body, '#'); i >= 0 {
			body = body[:i]
		}
		e := Entry{Format: "one-line", Line: n + 1, Enabled: true}
		pos := 0
		next := func() (string, int, bool) {
			for pos < len(body) && (body[pos] == ' ' || body[pos] == '\t') {
				pos++
			}
			if pos >= len(body) {
				return "", 0, false
			}
			s := pos
			for pos < len(body) && body[pos] != ' ' && body[pos] != '\t' {
				pos++
			}
			return body[s:pos], s, true
		}
		typ, _, ok := next()
		if !ok || (typ != "deb" && typ != "deb-src") {
			return nil, fmt.Errorf("%w: line %d is not a deb or deb-src entry", ErrSources, n+1)
		}
		e.Types = []string{typ}
		// Options in brackets may contain spaces.
		for pos < len(body) && (body[pos] == ' ' || body[pos] == '\t') {
			pos++
		}
		if pos < len(body) && body[pos] == '[' {
			end := strings.IndexByte(body[pos:], ']')
			if end < 0 {
				return nil, fmt.Errorf("%w: line %d has unclosed options", ErrSources, n+1)
			}
			for _, opt := range strings.Fields(body[pos+1 : pos+end]) {
				if k, v, ok := strings.Cut(opt, "="); ok && k == "signed-by" {
					e.SignedBy = v
				}
			}
			pos += end + 1
		}
		uri, us, ok := next()
		if !ok {
			return nil, fmt.Errorf("%w: line %d has no URI", ErrSources, n+1)
		}
		e.URI, e.uris = uri, []string{uri}
		e.uriStart, e.uriEnd = off+us, off+us+len(uri)
		suite, _, ok := next()
		if !ok {
			return nil, fmt.Errorf("%w: line %d has no suite", ErrSources, n+1)
		}
		e.Suites = []string{suite}
		for {
			c, _, ok := next()
			if !ok {
				break
			}
			e.Components = append(e.Components, c)
		}
		es = append(es, e)
	}
	return es, nil
}

func parseDeb822(text string) ([]Entry, error) {
	var es []Entry
	spans := lineSpans(text)
	type field struct {
		val        string
		line       int
		start, end int
		multi      bool
	}
	cur := map[string]*field{}
	var last *field
	flush := func() error {
		if len(cur) == 0 {
			return nil
		}
		get := func(k string) string {
			if f := cur[strings.ToLower(k)]; f != nil {
				return f.val
			}
			return ""
		}
		u := cur["uris"]
		if u == nil {
			return fmt.Errorf("%w: a stanza has no URIs field", ErrSources)
		}
		e := Entry{Format: "deb822", Line: u.line, Enabled: !strings.EqualFold(get("Enabled"), "no"),
			Types: strings.Fields(get("Types")), Suites: strings.Fields(get("Suites")),
			Components: strings.Fields(get("Components")), SignedBy: strings.TrimSpace(get("Signed-By")),
			uris: strings.Fields(u.val)}
		if len(e.uris) > 0 {
			e.URI = e.uris[0]
		}
		if !u.multi && len(e.uris) == 1 {
			e.uriStart, e.uriEnd = u.start, u.end
		}
		if cur["signed-by"] != nil && cur["signed-by"].multi {
			e.SignedBy = "inline key"
		}
		es = append(es, e)
		cur, last = map[string]*field{}, nil
		return nil
	}
	for n, sp := range spans {
		ln := text[sp[0]:sp[1]]
		if strings.TrimSpace(ln) == "" {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		if strings.HasPrefix(ln, "#") {
			continue
		}
		if ln[0] == ' ' || ln[0] == '\t' {
			if last == nil {
				return nil, fmt.Errorf("%w: line %d continues no field", ErrSources, n+1)
			}
			last.multi = true
			last.val += "\n" + strings.TrimSpace(ln)
			continue
		}
		k, v, ok := strings.Cut(ln, ":")
		if !ok || strings.ContainsAny(k, " \t") || k == "" {
			return nil, fmt.Errorf("%w: line %d is not a field", ErrSources, n+1)
		}
		key := strings.ToLower(k)
		if cur[key] != nil {
			return nil, fmt.Errorf("%w: line %d repeats %s", ErrSources, n+1, k)
		}
		f := &field{val: strings.TrimSpace(v), line: n + 1}
		lead := len(v) - len(strings.TrimLeft(v, " \t"))
		f.start = sp[0] + len(k) + 1 + lead
		f.end = f.start + len(f.val)
		cur[key], last = f, f
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return es, nil
}

func vendorKeyring(signedBy, id string) bool {
	return signedBy == "" || strings.HasPrefix(signedBy, "/usr/share/keyrings/"+id+"-archive-keyring.")
}

func classify(e *Entry, d Distro) {
	e.Role = RoleOther
	cn := d.Codename
	if d.ID != "debian" && d.ID != "ubuntu" || cn == "" {
		e.Reason = "The distribution is not Debian or Ubuntu."
		return
	}
	security := map[string]bool{cn + "-security": true}
	archive := map[string]bool{cn: true, cn + "-updates": true, cn + "-backports": true, cn + "-proposed": true}
	if d.ID == "debian" {
		archive[cn+"-proposed-updates"] = true
		archive[cn+"-backports-sloppy"] = true
	}
	nArch, nSec := 0, 0
	for _, s := range e.Suites {
		switch {
		case archive[s]:
			nArch++
		case security[s]:
			nSec++
		}
	}
	isPorts := strings.Contains(e.URI, "ports.ubuntu.com") || strings.Contains(e.URI, "/ubuntu-ports")
	// A third-party line can reuse the codename (deb https://download.docker.com/linux/ubuntu jammy stable);
	// the distribution's own components tell the archive apart.
	comps := map[string]bool{"main": true, "restricted": true, "universe": true, "multiverse": true}
	if d.ID == "debian" {
		comps = map[string]bool{"main": true, "contrib": true, "non-free": true, "non-free-firmware": true}
	}
	vendorComps := len(e.Components) > 0
	for _, c := range e.Components {
		vendorComps = vendorComps && comps[c]
	}
	switch {
	case len(e.Suites) == 0 || nArch+nSec != len(e.Suites):
		e.Reason = "Its suites are not this release's archive or security suites."
	case !vendorComps:
		e.Reason = "Its components are not the distribution's archive components."
	case nArch > 0 && nSec > 0:
		e.Reason = "It mixes archive and security suites; split it before changing its mirror."
	case !vendorKeyring(e.SignedBy, d.ID):
		e.Reason = "It is not signed by the distribution's archive key."
	case nSec > 0:
		e.Role = RoleSecurity
		e.Reason = "Security updates stay on the distribution's security server, which publishes them first."
	case isPorts:
		e.Role = RolePorts
		e.Reason = "Mirror lists cover the main architectures only; ports mirrors are not supported."
	default:
		e.Role = RoleArchive
	}
	if e.Role != RoleArchive {
		return
	}
	switch {
	case !e.Enabled:
		e.Reason = "The entry is disabled."
	case len(e.uris) != 1:
		e.Reason = "It lists more than one URI; change it by hand."
	case e.uriEnd == 0:
		e.Reason = "Its URIs field spans several lines; change it by hand."
	case ValidCurrentURI(e.URI) != nil:
		e.Reason = "Its URI is not a plain http or https address or a Debian mirror list."
	default:
		e.Switchable = true
	}
}

// SetMirror replaces from with to in every switchable archive entry of the
// file that uses from. Every other byte of the file is kept. It refuses when
// no entry matches.
func SetMirror(file, text string, d Distro, from, to string) (string, int, error) {
	if err := ValidMirrorURI(to); err != nil {
		return "", 0, err
	}
	es, err := ParseSources(file, text, d)
	if err != nil {
		return "", 0, err
	}
	var b strings.Builder
	last, n := 0, 0
	for _, e := range es {
		if !e.Switchable || e.URI != from {
			continue
		}
		if text[e.uriStart:e.uriEnd] != from {
			return "", 0, fmt.Errorf("%w: entry at line %d moved", ErrSources, e.Line)
		}
		b.WriteString(text[last:e.uriStart])
		b.WriteString(to)
		last = e.uriEnd
		n++
	}
	if n == 0 {
		return "", 0, fmt.Errorf("%w: no switchable archive entry uses %s", ErrSources, from)
	}
	b.WriteString(text[last:])
	return b.String(), n, nil
}
