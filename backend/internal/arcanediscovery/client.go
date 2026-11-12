// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package arcanediscovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"homelab-control/backend/internal/composediscovery"
)

// ErrProjectNotFound reports that no Arcane project matched the requested
// Compose project/service. The caller falls back to local file apply.
var ErrProjectNotFound = errors.New("no matching Arcane project")

// ErrArcane is a non-HTTP client error (bad URL, transport).
var ErrArcane = errors.New("Arcane API unavailable")

// Project is one Arcane project in an environment.
type Project struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Path         string `json:"path"`
	RelativePath string `json:"relativePath"`
}

// Compose is one project's published Compose document.
type Compose struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	ComposeFileName string `json:"composeFileName"`
	ComposeContent  string `json:"composeContent"`
}

// Result describes a successful Arcane label apply.
type Result struct {
	Wrote string `json:"wrote"`
	Added int    `json:"added"`
}

type envelope[T any] struct {
	Success bool `json:"success"`
	Data    T    `json:"data"`
}

// Client is a stdlib-only Arcane API client with redirects disabled and the
// documented per-call timeouts (8 s GET, 15 s PUT). It never logs bodies.
type Client struct {
	url    string
	apiKey string
	env    string
	http   *http.Client
}

// New builds a client for an Arcane instance. env is the environment id the
// endpoints operate on ("0" by default).
func New(url, apiKey, env string) (*Client, error) {
	url = strings.TrimRight(strings.TrimSpace(url), "/")
	if url == "" || !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("%w: invalid Arcane URL", ErrArcane)
	}
	if apiKey == "" {
		return nil, fmt.Errorf("%w: missing Arcane API key", ErrArcane)
	}
	if env == "" {
		env = "0"
	}
	return &Client{
		url:    url,
		apiKey: apiKey,
		env:    env,
		http:   &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

func (c *Client) do(ctx context.Context, method, path string, timeout time.Duration, body any, out any) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.url+path, reader)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrArcane, err)
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrArcane, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrArcane, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Never include the body: it may contain Compose env content.
		return fmt.Errorf("%w: status %d", ErrArcane, resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	var env envelope[json.RawMessage]
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("%w: %v", ErrArcane, err)
	}
	if !env.Success {
		return fmt.Errorf("%w: success=false", ErrArcane)
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("%w: %v", ErrArcane, err)
	}
	return nil
}

// ListProjects returns the projects of the configured environment.
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	var projects []Project
	err := c.do(ctx, http.MethodGet, "/api/environments/"+c.env+"/projects", 8*time.Second, nil, &projects)
	return projects, err
}

// GetCompose returns one project's Compose document.
func (c *Client) GetCompose(ctx context.Context, projectID string) (Compose, error) {
	var compose Compose
	err := c.do(ctx, http.MethodGet, "/api/environments/"+c.env+"/projects/"+projectID+"/compose", 8*time.Second, nil, &compose)
	return compose, err
}

// UpdateCompose writes composeContent for one project and returns the stored
// Compose (used for the wrote name). envContent is deliberately omitted.
func (c *Client) UpdateCompose(ctx context.Context, projectID, composeContent string) (Compose, error) {
	var compose Compose
	err := c.do(ctx, http.MethodPut, "/api/environments/"+c.env+"/projects/"+projectID, 15*time.Second,
		map[string]string{"composeContent": composeContent}, &compose)
	return compose, err
}

// ApplyLabels patches the matching project's Compose content with the same
// SafeLabels rules as the local file apply, then writes it back. It returns
// ErrProjectNotFound when no project matches (caller falls back to files).
func (c *Client) ApplyLabels(ctx context.Context, project, service string, labels map[string]string) (Result, error) {
	projects, err := c.ListProjects(ctx)
	if err != nil {
		return Result{}, err
	}
	for _, p := range projects {
		compose, err := c.GetCompose(ctx, p.ID)
		if err != nil {
			return Result{}, err
		}
		if !projectMatches(project, p, compose) || !serviceMatches(service, compose.ComposeContent) {
			continue
		}
		patched, added, err := composediscovery.PatchLabelsText(compose.ComposeContent, service, labels)
		if err != nil {
			return Result{}, err
		}
		if added == 0 {
			// Already present and equal: nothing to write, but this project matched.
			return Result{Wrote: composeName(compose, p), Added: 0}, nil
		}
		stored, err := c.UpdateCompose(ctx, p.ID, patched)
		if err != nil {
			return Result{}, err
		}
		return Result{Wrote: composeName(stored, p), Added: added}, nil
	}
	return Result{}, ErrProjectNotFound
}

func projectMatches(project string, p Project, c Compose) bool {
	if project == "" {
		return true
	}
	return c.Name == project || p.Name == project
}

func composeName(c Compose, p Project) string {
	if c.ComposeFileName != "" {
		return c.ComposeFileName
	}
	return p.Name
}

// serviceMatches reports whether composeContent defines service as a service
// key or as a service's container_name.
func serviceMatches(service, composeContent string) bool {
	_, services, containers := composeIdentity(composeContent)
	if services[service] {
		return true
	}
	for name := range containers {
		if name == service {
			return true
		}
	}
	return false
}

// composeIdentity scans Compose YAML text for the top-level name, the set of
// service keys and the set of container_name values. It is line-based and
// stdlib-only (the same approach as composediscovery's file parser).
func composeIdentity(content string) (name string, services map[string]bool, containers map[string]bool) {
	services = map[string]bool{}
	containers = map[string]bool{}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	inServices := false
	servicesIndent := -1
	cur := ""
	curIndent := -1
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") || strings.ContainsRune(line, '\t') {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		key, rest, ok := scanKey(trim)
		if !ok {
			continue
		}
		if indent == 0 && key == "name" && name == "" {
			name = unquote(rest)
			continue
		}
		if indent == 0 && key == "services" {
			inServices = true
			servicesIndent = indent
			continue
		}
		if inServices && indent <= servicesIndent {
			inServices = false
			cur, curIndent = "", -1
		}
		if !inServices {
			continue
		}
		if indent == servicesIndent+2 {
			cur, curIndent = key, indent
			services[key] = true
			continue
		}
		if cur != "" && indent == curIndent+2 && key == "container_name" {
			containers[unquote(rest)] = true
		}
	}
	return name, services, containers
}

var composeKey = func(trim string) (string, string, bool) {
	i := strings.Index(trim, ":")
	if i <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(trim[:i]), strings.TrimSpace(trim[i+1:]), true
}

func scanKey(trim string) (string, string, bool) { return composeKey(trim) }

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'")
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return strings.ReplaceAll(s[1:len(s)-1], `\"`, `"`)
	}
	return s
}
