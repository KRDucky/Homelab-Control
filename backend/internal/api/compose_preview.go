// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package api

import (
	"encoding/json"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/composediscovery"
	"homelab-control/backend/internal/routingwarnings"
	"homelab-control/backend/internal/traefikclient"
	"io"
	"mime"
	"net/http"
	"path/filepath"
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
	req, ok := s.decodeComposeRequest(w, r)
	if !ok {
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
	s.markFileApply(&out, inventory, snapshot, req.Model.Name)
	s.traefikResult(w, r, sess, "networking.compose.preview", out, nil)
}

func (s *Server) WithTraefikDynamicDir(dir string) *Server {
	s.traefikDynamicDir = dir
	return s
}

func (s *Server) WithComposeApplyDir(dir string) *Server {
	s.composeApplyDir = dir
	return s
}

func (s *Server) markFileApply(out *composediscovery.Proposal, inventory traefikclient.Inventory, snapshot composediscovery.Snapshot, name string) {
	out.ApplyAvailable = s.traefikDynamicDir != "" && out.FileYAML != "" && composediscovery.FileApplyReady(out.Blockers, name, out.Summary.Hostname, inventory)
	if s.composeApplyDir != "" && out.Project != "" && out.Service != "" {
		if _, err := composediscovery.FindComposeFile(s.composeApplyDir, out.Project, out.Service); err == nil {
			out.SourceFileVerified = true
		}
	}
	out.LabelApplyAvailable = s.composeApplyDir != "" && out.SourceFileVerified && composediscovery.LabelApplyReady(out.Blockers, name, out.Summary.Hostname, inventory, snapshot.Traefik.DockerProvider)
}

func (s *Server) decodeComposeRequest(w http.ResponseWriter, r *http.Request) (composediscovery.Request, bool) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeError(w, 415, "expected_json", "Expected application/json.")
		return composediscovery.Request{}, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req composediscovery.Request
	if err = dec.Decode(&req); err != nil {
		writeError(w, 400, "invalid_model", "Invalid Compose preview model.")
		return composediscovery.Request{}, false
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		writeError(w, 400, "invalid_model", "Expected one preview model.")
		return composediscovery.Request{}, false
	}
	if _, err := req.Model.Labels(); err != nil {
		writeError(w, 400, "invalid_model", "Invalid hostname, port or HTTP/TLS model.")
		return composediscovery.Request{}, false
	}
	return req, true
}

func (s *Server) handleTraefikFileApply(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.composeReady(w, r, sess) {
		return
	}
	if !s.traefikReady(w, r, sess) {
		return
	}
	if s.traefikDynamicDir == "" {
		writeError(w, 503, "file_apply_unavailable", "Traefik dynamic directory is not configured.")
		return
	}
	req, ok := s.decodeComposeRequest(w, r)
	if !ok {
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
	s.markFileApply(&out, inventory, snapshot, req.Model.Name)
	if !out.ApplyAvailable || out.FileYAML == "" {
		writeError(w, 409, "file_apply_blocked", "File YAML cannot be written until preview blockers are resolved.")
		return
	}
	wrote, err := composediscovery.WriteYAML(s.traefikDynamicDir, req.Model.Name, out.FileYAML)
	if err != nil {
		s.auditLog("networking.compose.file.apply", sess.Username, r, "failed", "dynamic yaml write failed")
		writeError(w, 503, "file_apply_unavailable", "Traefik dynamic YAML could not be written.")
		return
	}
	if err = s.auditLog("networking.compose.file.apply", sess.Username, r, "applied", "dynamic yaml wrote"); err != nil {
		writeError(w, 503, "audit_unavailable", "Traefik access could not be audited.")
		return
	}
	writeJSON(w, 200, map[string]any{"wrote": wrote, "applyAvailable": true})
}

func (s *Server) handleTraefikComposeApply(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.composeReady(w, r, sess) {
		return
	}
	if !s.traefikReady(w, r, sess) {
		return
	}
	if s.composeApplyDir == "" {
		writeError(w, 503, "compose_apply_unavailable", "Compose apply directory is not configured.")
		return
	}
	req, ok := s.decodeComposeRequest(w, r)
	if !ok {
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
	s.markFileApply(&out, inventory, snapshot, req.Model.Name)
	if !out.LabelApplyAvailable {
		writeError(w, 409, "compose_apply_blocked", "Compose labels cannot be written until preview blockers are resolved.")
		return
	}
	path, err := composediscovery.FindComposeFile(s.composeApplyDir, out.Project, out.Service)
	if err != nil {
		s.auditLog("networking.compose.labels.apply", sess.Username, r, "failed", "compose file not found")
		writeError(w, 409, "compose_apply_blocked", "Compose source file for the selected application was not found.")
		return
	}
	added, err := composediscovery.PatchServiceLabels(path, out.Service, out.Proposed.Labels)
	if err != nil {
		s.auditLog("networking.compose.labels.apply", sess.Username, r, "failed", "compose label patch failed")
		if err == composediscovery.ErrComposeLabelConflict {
			writeError(w, 409, "compose_apply_blocked", "Existing Compose labels differ from the proposal.")
			return
		}
		if err == composediscovery.ErrComposeLabelsForm {
			writeError(w, 409, "compose_apply_blocked", "Compose labels must be a YAML mapping.")
			return
		}
		writeError(w, 503, "compose_apply_unavailable", "Compose labels could not be written.")
		return
	}
	if err = s.auditLog("networking.compose.labels.apply", sess.Username, r, "applied", "compose labels wrote"); err != nil {
		writeError(w, 503, "audit_unavailable", "Traefik access could not be audited.")
		return
	}
	writeJSON(w, 200, map[string]any{
		"wrote":               filepath.Base(path),
		"added":               added,
		"labelApplyAvailable": true,
		"recreated":           false,
	})
}
