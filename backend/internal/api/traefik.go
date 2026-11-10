// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/traefikclient"
)

func (s *Server) WithTraefik(h traefikclient.Helper) *Server { s.traefik = h; return s }
func (s *Server) traefikReady(w http.ResponseWriter, r *http.Request, sess *auth.Session) bool {
	if (r.Method == "GET" || r.Method == "HEAD") && (!auth.CheckCSRF(sess, r.Header.Get("X-CSRF-Token")) || (r.Header.Get("Origin") != "" && !s.originAllowed(r))) {
		writeError(w, 403, "csrf_rejected", "Invalid CSRF token or request origin.")
		return false
	}
	if r.URL.RawQuery != "" {
		writeError(w, 400, "bad_request", "Query parameters are not supported.")
		return false
	}
	if s.audit == nil {
		writeError(w, 503, "audit_unavailable", "Traefik access requires an audit logger.")
		return false
	}
	if s.traefik == nil {
		writeError(w, 503, "traefik_unavailable", "Traefik extension is not configured.")
		return false
	}
	return true
}
func (s *Server) traefikResult(w http.ResponseWriter, r *http.Request, sess *auth.Session, event string, out any, err error) {
	if err != nil {
		writeError(w, 502, "traefik_unavailable", "The Traefik extension could not complete this read-only request.")
		return
	}
	if err = s.auditLog(event, sess.Username, r, "returned", "read-only extension response"); err != nil {
		writeError(w, 503, "audit_unavailable", "Traefik access could not be audited.")
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleTraefikInventory(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.traefikReady(w, r, sess) {
		return
	}
	out, err := s.traefik.Inventory(r.Context())
	s.traefikResult(w, r, sess, "networking.traefik.inventory.read", out, err)
}
func (s *Server) handleTraefikDiagnostics(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.traefikReady(w, r, sess) {
		return
	}
	out, err := s.traefik.Diagnostics(r.Context())
	s.traefikResult(w, r, sess, "networking.traefik.diagnostics.read", out, err)
}
func (s *Server) handleTraefikLabels(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.traefikReady(w, r, sess) {
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeError(w, 415, "expected_json", "Expected application/json.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var model traefikclient.Model
	if err = dec.Decode(&model); err != nil {
		writeError(w, 400, "invalid_model", "Invalid Traefik preview model.")
		return
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		writeError(w, 400, "invalid_model", "Expected one model.")
		return
	}
	if _, err = model.Labels(); err != nil {
		writeError(w, 400, "invalid_model", "Invalid hostname, port or HTTP/TLS settings.")
		return
	}
	out, err := s.traefik.Labels(r.Context(), model)
	s.traefikResult(w, r, sess, "networking.traefik.labels.preview", out, err)
}
