// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package api

import (
	"encoding/json"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/composediscovery"
	"homelab-control/backend/internal/routingwarnings"
	"io"
	"mime"
	"net/http"
)

func (s *Server) WithComposeDiscovery(p composediscovery.Provider) *Server { s.discovery = p; return s }
func (s *Server) composeReady(w http.ResponseWriter, r *http.Request, sess *auth.Session) bool {
	if !sess.HasScope(auth.ScopeComposeRead) {
		writeError(w, 403, "forbidden", "Compose discovery permission is required.")
		return false
	}
	if (r.Method == "GET" || r.Method == "HEAD") && (!auth.CheckCSRF(sess, r.Header.Get("X-CSRF-Token")) || (r.Header.Get("Origin") != "" && !s.originAllowed(r))) {
		writeError(w, 403, "csrf_rejected", "Invalid CSRF token or origin.")
		return false
	}
	if r.URL.RawQuery != "" {
		writeError(w, 400, "bad_request", "Query parameters are unsupported.")
		return false
	}
	if s.audit == nil {
		writeError(w, 503, "audit_unavailable", "Discovery requires an audit logger.")
		return false
	}
	if s.discovery == nil {
		writeError(w, 503, "discovery_unavailable", "Docker Compose discovery is not enabled. No Docker access is configured.")
		return false
	}
	return true
}
func (s *Server) handleComposeDiscovery(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.composeReady(w, r, sess) {
		return
	}
	snapshot, err := s.discovery.Snapshot(r.Context())
	if err != nil {
		s.auditLog("networking.compose.discovery.read", sess.Username, r, "unavailable", "private metadata read failed")
		writeError(w, 503, "discovery_unavailable", "Compose discovery is unavailable; verify the private metadata service.")
		return
	}
	s.traefikResult(w, r, sess, "networking.compose.discovery.read", composediscovery.View(snapshot), err)
}
func (s *Server) handleComposePreview(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.composeReady(w, r, sess) {
		return
	}
	if !s.traefikReady(w, r, sess) {
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeError(w, 415, "expected_json", "Expected application/json.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req composediscovery.Request
	if err = dec.Decode(&req); err != nil {
		writeError(w, 400, "invalid_model", "Invalid Compose preview model.")
		return
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		writeError(w, 400, "invalid_model", "Expected one preview model.")
		return
	}
	if _, err := req.Model.Labels(); err != nil {
		writeError(w, 400, "invalid_model", "Invalid hostname, port or HTTP/TLS model.")
		return
	}
	snapshot, err := s.discovery.Snapshot(r.Context())
	if err != nil {
		writeError(w, 503, "discovery_unavailable", "Compose discovery is unavailable.")
		return
	}
	inventory, err := s.traefik.Inventory(r.Context())
	if err != nil {
		writeError(w, 502, "traefik_unavailable", "Inventory is required to check conflicts.")
		return
	}
	out, err := composediscovery.Build(snapshot, inventory, req)
	if err != nil {
		writeError(w, 400, "invalid_model", "Invalid hostname, port, service, network or TLS selection.")
		return
	}
	out.Warnings = routingwarnings.Consolidate(out.Warnings, inventory)
	s.traefikResult(w, r, sess, "networking.compose.preview", out, nil)
}
