// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package aptmirror finds candidate Debian/Ubuntu archive mirrors from the
// distributions' official lists and measures them by fetching the release
// index (dists/<codename>/InRelease). It only reads over HTTP; it never
// changes configuration. Signatures are not checked here: a mirror change
// keeps the entry's signing key, so APT checks them when it next updates.
package aptmirror

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"homelab-control/backend/internal/aptapply"
)

// Limits.
const (
	MaxCandidates  = 40
	Parallel       = 8
	ProbeTimeout   = 5 * time.Second
	maxIndexBytes  = 2 << 20
	maxListBytes   = 256 << 10
	staleTolerance = 12 * time.Hour
)

// Candidate sources.
const (
	SourceCurrent  = "current"
	SourceOfficial = "official"
)

// Statuses of a probe.
const (
	StatusOK          = "ok"
	StatusStale       = "stale"
	StatusMismatch    = "mismatch"
	StatusExpired     = "expired"
	StatusUnreachable = "unreachable"
)

// Candidate is one mirror to measure.
type Candidate struct {
	URI    string `json:"uri"`
	Source string `json:"source"`
}

// Result is one measured mirror.
type Result struct {
	URI          string    `json:"uri"`
	Host         string    `json:"host"`
	Source       string    `json:"source"`
	Status       string    `json:"status"`
	Reason       string    `json:"reason,omitempty"`
	FirstByteMS  int64     `json:"first_byte_ms,omitempty"`
	TotalMS      int64     `json:"total_ms,omitempty"`
	KBPerSecond  int64     `json:"kb_per_second,omitempty"`
	IndexDate    time.Time `json:"index_date,omitempty"`
	Rank         int       `json:"rank,omitempty"`
	CurrentInUse bool      `json:"current,omitempty"`
}

var country = regexp.MustCompile(`^[A-Z]{2}$`)

// ValidCountry reports whether c is an ISO 3166 alpha-2 code shape.
func ValidCountry(c string) bool { return country.MatchString(c) }

// Lister fetches a URL's body (bounded); the server passes an HTTP getter.
type Lister func(ctx context.Context, url string) ([]byte, error)

// Candidates returns the official candidates for d in country cc plus the
// current archive URIs, de-duplicated, at most MaxCandidates.
func Candidates(ctx context.Context, d aptapply.Distro, cc string, current []string, list Lister) ([]Candidate, error) {
	if !ValidCountry(cc) {
		return nil, errors.New("country must be a two-letter code")
	}
	seen := map[string]bool{}
	var out []Candidate
	add := func(u, src string) {
		u = normalise(u)
		if u == "" || seen[u] || aptapply.ValidMirrorURI(u) != nil || len(out) >= MaxCandidates {
			return
		}
		seen[u] = true
		out = append(out, Candidate{URI: u, Source: src})
	}
	for _, u := range current {
		add(u, SourceCurrent)
	}
	switch d.ID {
	case "ubuntu":
		body, err := list(ctx, "http://mirrors.ubuntu.com/"+cc+".txt")
		if err != nil {
			return out, fmt.Errorf("the Ubuntu mirror list for %s could not be read", cc)
		}
		sc := bufio.NewScanner(strings.NewReader(string(body)))
		for sc.Scan() {
			add(strings.TrimSpace(sc.Text()), SourceOfficial)
		}
	case "debian":
		add("http://deb.debian.org/debian/", SourceOfficial)
		add("https://deb.debian.org/debian/", SourceOfficial)
		add("http://ftp."+strings.ToLower(cc)+".debian.org/debian/", SourceOfficial)
	default:
		return nil, errors.New("mirror lists exist for Debian and Ubuntu only")
	}
	return out, nil
}

func normalise(u string) string {
	u = strings.TrimSpace(u)
	if u != "" && !strings.HasSuffix(u, "/") {
		u += "/"
	}
	return u
}

// Prober measures candidates.
type Prober struct {
	// Client is used for every request; nil builds the default client.
	Client *http.Client
	Now    func() time.Time
}

// NewClient returns the probing client: bounded time, at most three
// redirects within http/https, and no connections to loopback, private or
// link-local addresses unless allowPrivate says the host is the current
// mirror the administrator configured.
func NewClient(allowPrivate func(host string) bool) *http.Client {
	dialer := &net.Dialer{Timeout: ProbeTimeout}
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          Parallel,
		ResponseHeaderTimeout: ProbeTimeout,
		TLSHandshakeTimeout:   ProbeTimeout,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, _ := net.SplitHostPort(addr)
			d := *dialer
			if allowPrivate == nil || !allowPrivate(host) {
				d.Control = func(_, address string, _ syscall.RawConn) error {
					ip, _, err := net.SplitHostPort(address)
					if err != nil {
						return err
					}
					a := net.ParseIP(ip)
					if a == nil || a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsUnspecified() || a.IsMulticast() {
						return errors.New("mirror address is not public")
					}
					return nil
				}
			}
			return d.DialContext(ctx, network, addr)
		},
	}
	return &http.Client{
		Transport: tr,
		Timeout:   ProbeTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
				return errors.New("too many or unsafe redirects")
			}
			return nil
		},
	}
}

// Probe measures every candidate for the release codename and ranks them.
func (p Prober) Probe(ctx context.Context, cands []Candidate, codename string) []Result {
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	client := p.Client
	if client == nil {
		client = NewClient(nil)
	}
	res := make([]Result, len(cands))
	sem := make(chan struct{}, Parallel)
	var wg sync.WaitGroup
	for i, c := range cands {
		wg.Add(1)
		go func(i int, c Candidate) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				res[i] = Result{URI: c.URI, Host: host(c.URI), Source: c.Source, Status: StatusUnreachable, Reason: "cancelled"}
				return
			}
			defer func() { <-sem }()
			res[i] = probeOne(ctx, client, c, codename, now)
		}(i, c)
	}
	wg.Wait()
	rank(res)
	return res
}

// HostOf returns the host of r.URI ("" when it does not parse).
func (r Result) HostOf() string { return host(r.URI) }

func host(u string) string {
	pu, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return pu.Host
}

func probeOne(ctx context.Context, client *http.Client, c Candidate, codename string, now func() time.Time) Result {
	r := Result{URI: c.URI, Host: host(c.URI), Source: c.Source, CurrentInUse: c.Source == SourceCurrent}
	fail := func(reason string) Result { r.Status, r.Reason = StatusUnreachable, reason; return r }
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URI+"dists/"+codename+"/InRelease", nil)
	if err != nil {
		return fail("bad address")
	}
	req.Header.Set("User-Agent", "homelab-control-mirror-probe")
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return fail("no answer within " + ProbeTimeout.String() + " or the address is not allowed")
	}
	defer resp.Body.Close()
	first := time.Since(start)
	if resp.StatusCode != http.StatusOK {
		return fail(fmt.Sprintf("HTTP %d", resp.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxIndexBytes+1))
	total := time.Since(start)
	if err != nil {
		return fail("the download was interrupted")
	}
	if len(body) > maxIndexBytes {
		return fail("the release index is too large")
	}
	r.FirstByteMS, r.TotalMS = first.Milliseconds(), total.Milliseconds()
	if ms := total.Milliseconds(); ms > 0 {
		r.KBPerSecond = int64(len(body)) / ms
	}
	f := releaseFields(string(body))
	if f["Codename"] != codename {
		r.Status, r.Reason = StatusMismatch, "the release index is for "+clean(f["Codename"])+", not "+codename
		return r
	}
	d, err := time.Parse(time.RFC1123, f["Date"])
	if err != nil {
		r.Status, r.Reason = StatusMismatch, "the release index has no valid date"
		return r
	}
	r.IndexDate = d.UTC()
	if vu := f["Valid-Until"]; vu != "" {
		if t, err := time.Parse(time.RFC1123, vu); err == nil && now().After(t) {
			r.Status, r.Reason = StatusExpired, "the release index has expired"
			return r
		}
	}
	r.Status = StatusOK
	return r
}

func clean(s string) string {
	if len(s) > 32 {
		s = s[:32]
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// releaseFields reads the header fields of a (clear-signed) Release file.
func releaseFields(text string) map[string]string {
	out := map[string]string{}
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimRight(ln, "\r")
		if ln == "" || ln[0] == ' ' || strings.HasPrefix(ln, "-----") {
			continue
		}
		k, v, ok := strings.Cut(ln, ":")
		if !ok || strings.ContainsAny(k, " \t") {
			continue
		}
		if _, dup := out[k]; !dup {
			out[k] = strings.TrimSpace(v)
		}
		if k == "SHA256" || k == "MD5Sum" || k == "SHA512" {
			break
		}
	}
	return out
}

// rank marks indexes older than the newest seen (beyond the tolerance) as
// stale and numbers the usable mirrors by total download time.
func rank(rs []Result) {
	var newest time.Time
	for _, r := range rs {
		if r.Status == StatusOK && r.IndexDate.After(newest) {
			newest = r.IndexDate
		}
	}
	for i := range rs {
		if rs[i].Status == StatusOK && newest.Sub(rs[i].IndexDate) > staleTolerance {
			rs[i].Status = StatusStale
			rs[i].Reason = "its release index is more than 12 hours older than the newest one seen"
		}
	}
	sort.SliceStable(rs, func(i, j int) bool {
		oi, oj := rs[i].Status == StatusOK, rs[j].Status == StatusOK
		if oi != oj {
			return oi
		}
		if oi {
			return rs[i].TotalMS < rs[j].TotalMS
		}
		return rs[i].URI < rs[j].URI
	})
	n := 0
	for i := range rs {
		if rs[i].Status == StatusOK {
			n++
			rs[i].Rank = n
		}
	}
}

// CountryForZone returns the country of an IANA time zone from zone.tab
// content ("" when unknown).
func CountryForZone(zoneTab, zone string) string {
	for _, ln := range strings.Split(zoneTab, "\n") {
		if strings.HasPrefix(ln, "#") {
			continue
		}
		f := strings.Split(ln, "\t")
		if len(f) >= 3 && f[2] == zone && ValidCountry(f[0]) {
			return f[0]
		}
	}
	return ""
}
