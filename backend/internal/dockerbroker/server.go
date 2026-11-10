// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package dockerbroker

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"homelab-control/backend/internal/composediscovery"
	"net/http"
	"time"
)

// NewServer authenticates a configured service peer, not browser/session claims.
// CORE is responsible for checking its current session scopes BEFORE making a call.
func NewServer(provider composediscovery.Provider, token string, clientUID uint32) (*http.Server, error) {
	if provider == nil || !validToken(token) {
		return nil, ErrUnavailable
	}
	sem := make(chan struct{}, 4)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		fail := func(code int) { http.Error(w, "Metadata request unavailable", code) }
		if r.URL.Path != SnapshotPath && r.URL.Path != "/v1/health" {
			fail(404)
			return
		}
		if r.Method != "GET" {
			fail(405)
			return
		}
		if r.URL.RawQuery != "" || r.URL.RawPath != "" || r.ContentLength != 0 || len(r.TransferEncoding) > 0 {
			fail(400)
			return
		}
		uid, ok := r.Context().Value(peerKey{}).(uint32)
		if !ok || uid != clientUID || len(r.Header.Values("Authorization")) != 1 || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			fail(403)
			return
		}
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
		default:
			fail(503)
			return
		}
		defer func() {
			if recover() != nil {
				fail(503)
			}
		}()
		if r.URL.Path == "/v1/health" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"schemaVersion":1,"status":"ready"}`))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		defer cancel()
		snapshot, e := provider.Snapshot(ctx)
		if e != nil {
			fail(503)
			return
		}
		envelope, e := FromSnapshot(snapshot)
		if e != nil {
			fail(503)
			return
		}
		data, e := json.Marshal(envelope)
		if e != nil || len(data) > MaxBytes {
			fail(503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	})
	return &http.Server{Handler: h, ConnContext: connContext, ReadHeaderTimeout: time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 2 * time.Second, MaxHeaderBytes: 8192}, nil
}
