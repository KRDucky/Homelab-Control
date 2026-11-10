// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package dockerbroker

import (
	"context"
	"homelab-control/backend/internal/composediscovery"
	"io"
	"mime"
	"net"
	"net/http"
	"time"
)

// Client implements the shared discovery Provider. Routing context is trusted
// CORE administrator configuration, never a claim made by a container or broker.
type Client struct {
	http    *http.Client
	token   string
	routing composediscovery.Context
}

func NewClient(socket, token string, serverUID uint32, routing composediscovery.Context) (*Client, error) {
	if !validToken(token) || verifySocket(socket, serverUID) != nil {
		return nil, ErrUnavailable
	}
	tr := &http.Transport{Proxy: nil, DisableKeepAlives: true, MaxConnsPerHost: 2, ResponseHeaderTimeout: 2 * time.Second, MaxResponseHeaderBytes: 16384, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		if verifySocket(socket, serverUID) != nil {
			return nil, ErrUnavailable
		}
		c, e := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", socket)
		if e != nil {
			return nil, ErrUnavailable
		}
		uid, e := peerUID(c)
		if e != nil || uid != serverUID {
			c.Close()
			return nil, ErrUnavailable
		}
		return c, nil
	}}
	return &Client{&http.Client{Transport: tr, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrUnavailable }}, token, routing}, nil
}
func (c *Client) Snapshot(ctx context.Context) (composediscovery.Snapshot, error) {
	req, e := http.NewRequestWithContext(ctx, "GET", "http://metadata"+SnapshotPath, nil)
	if e != nil {
		return composediscovery.Snapshot{}, ErrUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, e := c.http.Do(req)
	if e != nil {
		return composediscovery.Snapshot{}, ErrUnavailable
	}
	defer resp.Body.Close()
	media, _, e := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if e != nil || media != "application/json" || resp.StatusCode != 200 || resp.ContentLength > MaxBytes {
		return composediscovery.Snapshot{}, ErrUnavailable
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, MaxBytes+1))
	if e != nil {
		return composediscovery.Snapshot{}, ErrUnavailable
	}
	services, e := Decode(b)
	if e != nil {
		return composediscovery.Snapshot{}, ErrUnavailable
	}
	return composediscovery.Snapshot{Source: "docker-rootless", Fixture: false, Services: services, Traefik: c.routing}, nil
}
