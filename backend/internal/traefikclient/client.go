// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

package traefikclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

var ErrUnavailable = errors.New("Traefik extension unavailable or invalid response")

type Resource struct {
	Rule        string   `json:"rule,omitempty"`
	Service     string   `json:"service,omitempty"`
	Entrypoints []string `json:"entrypoints,omitempty"`
	TLS         *bool    `json:"tls,omitempty"`

	Name     string `json:"name"`
	Provider string `json:"provider"`
	Status   string `json:"status"`
}
type Inventory struct {
	SchemaVersion int        `json:"schemaVersion"`
	Routers       []Resource `json:"routers"`
	Services      []Resource `json:"services"`
	Middlewares   []Resource `json:"middlewares"`
}
type Check struct {
	Resource string `json:"resource"`
	Status   string `json:"status"`
	Count    int    `json:"count,omitempty"`
	Code     string `json:"code,omitempty"`
	Hint     string `json:"hint,omitempty"`
}
type Diagnostics struct {
	SchemaVersion int     `json:"schemaVersion"`
	Status        string  `json:"status"`
	Checks        []Check `json:"checks"`
}

type Helper interface {
	Inventory(context.Context) (Inventory, error)
	Diagnostics(context.Context) (Diagnostics, error)
	Labels(context.Context, Model) (Preview, error)
}

type Client struct {
	origin string
	token  string
	http   *http.Client
}

func New(origin, token string) (*Client, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.ForceQuery {
		return nil, errors.New("invalid helper origin")
	}
	if len(token) < 32 || len(token) > 256 {
		return nil, errors.New("invalid helper token")
	}
	for _, c := range token {
		if c < 33 || c > 126 {
			return nil, errors.New("invalid helper token")
		}
	}
	tr := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 2 * time.Second}).DialContext, TLSHandshakeTimeout: 2 * time.Second, ResponseHeaderTimeout: 4 * time.Second, MaxConnsPerHost: 8, MaxIdleConnsPerHost: 2}
	return &Client{strings.TrimSuffix(u.String(), "/"), token, &http.Client{Transport: tr, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect refused") }}}, nil
}

// LoadToken fails closed and never includes file paths/contents in errors.
func LoadToken(path string) (string, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", errors.New("helper token file unavailable")
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > 4096 || st.Mode().Perm()&0007 != 0 {
		return "", errors.New("helper token file must be private and regular")
	}
	b, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(b) > 4096 {
		return "", errors.New("invalid helper token file")
	}
	token := strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
	if _, err := New("http://helper.invalid", token); err != nil {
		return "", errors.New("invalid helper token file")
	}
	return token, nil
}

func (c *Client) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return ErrUnavailable
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.origin+path, body)
	if err != nil {
		return ErrUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		return ErrUnavailable
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 4*1024*1024+1))
	if err != nil || len(b) > 4*1024*1024 {
		return ErrUnavailable
	}
	if json.Unmarshal(b, out) != nil {
		return ErrUnavailable
	}
	return nil
}
func (c *Client) Inventory(ctx context.Context) (Inventory, error) {
	var out Inventory
	if err := c.call(ctx, "GET", "/v1/inventory", nil, &out); err != nil {
		return Inventory{}, err
	}
	if out.SchemaVersion != 1 {
		return Inventory{}, ErrUnavailable
	}
	for _, rows := range [][]Resource{out.Routers, out.Services, out.Middlewares} {
		if rows == nil || len(rows) > 5000 {
			return Inventory{}, ErrUnavailable
		}
		for i := range rows {
			r := &rows[i]
			if len(r.Rule) > 262 || (r.Rule != "" && !regexp.MustCompile("^Host\\(`[a-z0-9.-]+`\\)$").MatchString(r.Rule)) {
				r.Rule = ""
			}
			if !regexp.MustCompile(`^[a-zA-Z0-9_.@-]{0,280}$`).MatchString(r.Service) {
				r.Service = ""
			}
			if len(r.Entrypoints) > 32 {
				r.Entrypoints = nil
			}
			for _, ep := range r.Entrypoints {
				if !identifier.MatchString(ep) {
					r.Entrypoints = nil
					break
				}
			}
			for _, s := range []string{r.Name, r.Provider, r.Status} {
				if utf8.RuneCountInString(s) > 280 {
					return Inventory{}, ErrUnavailable
				}
			}
		}
	}
	return out, nil
}

var hints = map[string]string{
	"dns_failed": "Check the private network alias.", "connection_refused": "Check the private API port and listener.", "timeout": "Check private API reachability and response time.", "tls_failed": "Check the API certificate and trusted CA.", "unauthorized": "Traefik API authentication is required.", "forbidden": "Check Traefik API access policy.", "api_not_found": "Check the private Traefik API origin.", "redirect_blocked": "Configure the final API origin; redirects are refused.", "response_too_large": "Traefik API response exceeded the limit.", "invalid_response": "Traefik returned invalid inventory.", "upstream_http": "Traefik returned an unexpected HTTP status.", "network_error": "Check private API connectivity.",
}

func (c *Client) Diagnostics(ctx context.Context) (Diagnostics, error) {
	var out Diagnostics
	if err := c.call(ctx, "GET", "/v1/diagnostics", nil, &out); err != nil {
		return Diagnostics{}, err
	}
	if out.SchemaVersion != 1 || len(out.Checks) != 3 {
		return Diagnostics{}, ErrUnavailable
	}
	seen := map[string]bool{}
	successes := 0
	for i, ch := range out.Checks {
		if (ch.Resource != "routers" && ch.Resource != "services" && ch.Resource != "middlewares") || seen[ch.Resource] {
			return Diagnostics{}, ErrUnavailable
		}
		seen[ch.Resource] = true
		if ch.Status == "available" {
			if ch.Count < 0 || ch.Count > 5000 {
				return Diagnostics{}, ErrUnavailable
			}
			successes++
			out.Checks[i].Code = ""
			out.Checks[i].Hint = ""
		} else if ch.Status == "unavailable" {
			hint, ok := hints[ch.Code]
			if !ok {
				return Diagnostics{}, ErrUnavailable
			}
			out.Checks[i].Count = 0
			out.Checks[i].Hint = hint
		} else {
			return Diagnostics{}, ErrUnavailable
		}
	}
	out.Status = "degraded"
	if successes == 3 {
		out.Status = "available"
	} else if successes == 0 {
		out.Status = "unavailable"
	}
	return out, nil
}
func (c *Client) Labels(ctx context.Context, m Model) (Preview, error) {
	expected, err := m.Labels()
	if err != nil {
		return Preview{}, err
	}
	var out Preview
	if err = c.call(ctx, "POST", "/v1/labels", m, &out); err != nil {
		return Preview{}, err
	}
	if !maps.Equal(out.Labels, expected.Labels) {
		return Preview{}, ErrUnavailable
	}
	return expected, nil
}
