// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"errors"
	"net/http"
	"strconv"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/filesinfo"
)

type filesRootsResponse struct {
	Roots           []filesinfo.PublicRoot `json:"roots"`
	BrowseAvailable bool                   `json:"browse_available"`
}

type filesEntriesResponse struct {
	Root    string            `json:"root"`
	Path    string            `json:"path"`
	Entries []filesinfo.Entry `json:"entries"`
}

// filesProtectedGET is the layout/storage bar on identifier-bearing GETs:
// the session CSRF token before anything is read or audited. Server.handle
// skips Origin/CSRF for GET; every files GET must use this helper.
// Same-origin browser GET fetch omits Origin; requiring it 403s the UI.
// A present Origin (cross-origin GET) still has to match originAllowed.
func (s *Server) filesProtectedGET(w http.ResponseWriter, r *http.Request, sess *auth.Session) bool {
	if !auth.CheckCSRF(sess, r.Header.Get("X-CSRF-Token")) {
		writeError(w, http.StatusForbidden, "csrf_rejected", "missing or invalid CSRF token")
		return false
	}
	if o := r.Header.Get("Origin"); o != "" && o != "null" && !s.originAllowed(r) {
		writeError(w, http.StatusForbidden, "origin_rejected", "request origin not allowed")
		return false
	}
	if s.audit == nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "File listing requires an access audit logger.")
		return false
	}
	return true
}

func (s *Server) handleFilesRoots(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.filesProtectedGET(w, r, sess) {
		return
	}
	roots := s.files.PublicRoots()
	detail := "roots=" + strconv.Itoa(len(roots))
	if !s.files.BrowseAvailable() {
		detail = "browse_available=false"
	}
	if err := s.auditLog("files.roots.read", sess.Username, r, "returned", detail); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "File roots cannot be returned because their access could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, filesRootsResponse{Roots: roots, BrowseAvailable: s.files.BrowseAvailable()})
}

func (s *Server) handleFilesEntries(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.filesProtectedGET(w, r, sess) {
		return
	}
	id := r.URL.Query().Get("root")
	rel := r.URL.Query().Get("path")
	cleaned, err := filesinfo.RelPath(rel)
	if err != nil {
		s.filesListError(w, err)
		return
	}
	ents, err := s.files.List(id, rel)
	if err != nil {
		s.filesListError(w, err)
		return
	}
	// Audit root id + relative path only. Never contents, never the jail path.
	detail := "root=" + id + " path=" + cleaned
	if err := s.auditLog("files.entries.read", sess.Username, r, "returned", detail); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "Directory entries cannot be returned because their access could not be audited.")
		return
	}
	if ents == nil {
		ents = []filesinfo.Entry{}
	}
	writeJSON(w, http.StatusOK, filesEntriesResponse{Root: id, Path: cleaned, Entries: ents})
}

func (s *Server) filesListError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, filesinfo.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "files_unavailable", "No browseable file roots are configured on this server.")
	case errors.Is(err, filesinfo.ErrUnknownRoot):
		writeError(w, http.StatusNotFound, "not_found", "Unknown file root or path.")
	case errors.Is(err, filesinfo.ErrBadPath):
		writeError(w, http.StatusBadRequest, "bad_request", "Path must be a relative path inside the confined root.")
	case errors.Is(err, filesinfo.ErrEscape):
		writeError(w, http.StatusForbidden, "forbidden", "Path is outside the confined root.")
	case errors.Is(err, filesinfo.ErrPermission):
		writeError(w, http.StatusForbidden, "not_readable", "The path could not be listed.")
	case errors.Is(err, filesinfo.ErrNotDir):
		writeError(w, http.StatusBadRequest, "not_a_directory", "Path is not a directory.")
	case errors.Is(err, filesinfo.ErrTooMany):
		writeError(w, http.StatusBadRequest, "too_many_entries", "Directory has too many entries to list.")
	default:
		writeError(w, http.StatusBadRequest, "bad_request", "The directory could not be listed.")
	}
}
