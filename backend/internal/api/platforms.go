// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/platform"
)

// WithPlatforms installs the administrator-built platform registry. There is
// no API to add, change or remove a registration.
func (s *Server) WithPlatforms(r *platform.Registry) *Server { s.platforms = r; return s }

func (s *Server) platformsReady(w http.ResponseWriter, r *http.Request, sess *auth.Session) bool {
	if !sess.HasScope(auth.ScopePlatformsRead) {
		writeError(w, 403, "forbidden", "Container platform permission is required.")
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
		writeError(w, 503, "audit_unavailable", "Platform access requires an audit logger.")
		return false
	}
	if s.platforms.Len() == 0 {
		writeError(w, 503, "platforms_unavailable", "No container platform is registered by the administrator.")
		return false
	}
	return true
}

type platformsResponse struct {
	Platforms []platform.PublicView `json:"platforms"`
	// AutoSelected is set when exactly one visible platform can produce
	// routing previews, so the UI can skip the platform step.
	AutoSelected *string `json:"autoSelected"`
}

func (s *Server) handlePlatforms(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.platformsReady(w, r, sess) {
		return
	}
	out := platformsResponse{Platforms: s.platforms.Visible(sess.HasScope)}
	if id, ok := platform.AutoSelect(out.Platforms, platform.PreviewCapabilities...); ok {
		out.AutoSelected = &id
	}
	if s.auditLog("platform.registry.read", sess.Username, r, "returned", "visible platforms listed") != nil {
		writeError(w, 503, "audit_unavailable", "Platform access could not be audited.")
		return
	}
	writeJSON(w, 200, out)
}

type platformInventoryResponse struct {
	Platform  platform.PublicView `json:"platform"`
	Inventory platform.Inventory  `json:"inventory"`
}

func (s *Server) handlePlatformInventory(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.platformsReady(w, r, sess) {
		return
	}
	id := r.PathValue("id")
	if !platform.ValidID(id) {
		writeError(w, 404, "not_found", "Unknown platform.")
		return
	}
	inv, view, err := s.platforms.Inventory(r.Context(), id, sess.HasScope)
	switch {
	case errors.Is(err, platform.ErrNotFound):
		// Same answer for unknown and unauthorized platforms.
		writeError(w, 404, "not_found", "Unknown platform.")
		return
	case err != nil:
		s.auditLog("platform."+string(view.Kind)+".inventory.read", sess.Username, r, "unavailable", "platform="+id)
		writeError(w, 503, "platform_unavailable", "Platform metadata is unavailable; verify the administrator-approved connection.")
		return
	}
	if s.auditLog("platform."+string(view.Kind)+".inventory.read", sess.Username, r, "returned", "platform="+id) != nil {
		writeError(w, 503, "audit_unavailable", "Platform access could not be audited.")
		return
	}
	writeJSON(w, 200, platformInventoryResponse{view, inv})
}

// handlePlatformPreview needs networking.proxy.read (route), platforms.read
// and the selected platform's own scope. It never applies anything.
func (s *Server) handlePlatformPreview(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.platformsReady(w, r, sess) || !s.traefikReady(w, r, sess) {
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
	var req platform.PreviewRequest
	if dec.Decode(&req) != nil {
		writeError(w, 400, "invalid_model", "Invalid platform preview request.")
		return
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		writeError(w, 400, "invalid_model", "Expected one preview request.")
		return
	}
	if _, err := req.Model.Labels(); err != nil || !platform.ValidID(req.Platform) || len(req.Workload) > 512 || len(req.Service) > 512 || len(req.RouteClass) > 128 {
		writeError(w, 400, "invalid_model", "Invalid platform, hostname, port or HTTP/TLS selection.")
		return
	}
	inv, view, err := s.platforms.Inventory(r.Context(), req.Platform, sess.HasScope)
	if errors.Is(err, platform.ErrNotFound) {
		writeError(w, 404, "not_found", "Unknown platform.")
		return
	} else if err != nil {
		writeError(w, 503, "platform_unavailable", "Platform metadata is unavailable.")
		return
	}
	previewer, err := s.platforms.Previewer(req.Platform, sess.HasScope)
	if err != nil {
		writeError(w, 400, "unsupported", "This platform cannot produce routing previews.")
		return
	}
	routers, err := s.traefik.Inventory(r.Context())
	if err != nil {
		writeError(w, 502, "traefik_unavailable", "Inventory is required to check conflicts.")
		return
	}
	out, err := previewer.Preview(r.Context(), inv, req, routers)
	switch {
	case errors.Is(err, platform.ErrUnavailable):
		writeError(w, 503, "platform_unavailable", "Platform metadata is unavailable.")
		return
	case err != nil:
		writeError(w, 400, "invalid_model", "Invalid workload, service, port, network, provider or TLS selection.")
		return
	}
	// Defence in depth: a preview is never applicable or deployable here.
	out.Platform, out.ApplyAvailable, out.Deployable = view, false, false
	if s.auditLog("platform."+string(view.Kind)+".routing.preview", sess.Username, r, "returned", "platform="+req.Platform+" target="+string(req.Target)) != nil {
		writeError(w, 503, "audit_unavailable", "Platform access could not be audited.")
		return
	}
	writeJSON(w, 200, out)
}
