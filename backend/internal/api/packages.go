// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"net/http"
	"strconv"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/hostinfo"
)

// packagesProtectedGET is the read-only Software Center bar on identifier-bearing
// GETs: the session CSRF token before anything is read or audited, the same as
// filesProtectedGET. Server.handle skips Origin/CSRF for GET; every packages
// GET must use this helper. Same-origin browser fetch omits Origin; a present
// (cross-origin) Origin must still match originAllowed.
func (s *Server) packagesProtectedGET(w http.ResponseWriter, r *http.Request, sess *auth.Session) bool {
	if !auth.CheckCSRF(sess, r.Header.Get("X-CSRF-Token")) {
		writeError(w, http.StatusForbidden, "csrf_rejected", "missing or invalid CSRF token")
		return false
	}
	if o := r.Header.Get("Origin"); o != "" && o != "null" && !s.originAllowed(r) {
		writeError(w, http.StatusForbidden, "origin_rejected", "request origin not allowed")
		return false
	}
	if s.audit == nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "Package queries require an access audit logger.")
		return false
	}
	return true
}

// packageFamily maps os-release to the package family the broker adapter
// expects ("unknown" when the collector is absent or os-release unreadable).
func (s *Server) packageFamily() string {
	if s.collector == nil {
		return "unknown"
	}
	osr, _, err := hostinfo.ReadOSRelease(s.collector.Roots.Etc)
	if err != nil {
		return "unknown"
	}
	return hostinfo.DistroFamily(osr)
}

// packageFamilyBinary is the primary query binary of a package family; "" for
// unknown families (which the capability reports as unsupported).
func packageFamilyBinary(family string) string {
	switch family {
	case "debian":
		return "dpkg-query"
	case "fedora":
		return "rpm"
	case "suse":
		return "zypper"
	case "arch":
		return "pacman"
	default:
		return ""
	}
}

type packagesRequest struct {
	Family string `json:"family"`
	Q      string `json:"q,omitempty"`
}

// packagesCall runs one read op and returns the broker error for the handler.
func (s *Server) packagesCall(w http.ResponseWriter, r *http.Request, sess *auth.Session, op string, req packagesRequest, out any) bool {
	if s.broker == nil {
		writeError(w, http.StatusServiceUnavailable, "broker_unavailable", brokerDownReason)
		return false
	}
	if err := s.broker.CallWithArgs(r.Context(), op, req, actorFor(sess), out); err != nil {
		brokerError(w, err)
		return false
	}
	return true
}

func (s *Server) handlePackagesSearch(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.packagesProtectedGET(w, r, sess) {
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" || len(q) > 64 {
		writeError(w, http.StatusBadRequest, "bad_request", "q must be 1-64 characters")
		return
	}
	var out types.PackageList
	if !s.packagesCall(w, r, sess, "packages.search", packagesRequest{Family: s.packageFamily(), Q: q}, &out) {
		return
	}
	// Audit counts and query length only, never package lists or the query text.
	detail := "family=" + s.packageFamily() + " results=" + strconv.Itoa(len(out.Packages)) + " q_len=" + strconv.Itoa(len(q))
	if err := s.auditLog("packages.search.read", sess.Username, r, "returned", detail); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "Search results cannot be returned because their access could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePackagesInstalled(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.packagesProtectedGET(w, r, sess) {
		return
	}
	var out types.PackageList
	if !s.packagesCall(w, r, sess, "packages.installed", packagesRequest{Family: s.packageFamily()}, &out) {
		return
	}
	if err := s.auditLog("packages.installed.read", sess.Username, r, "returned", "family="+s.packageFamily()+" packages="+strconv.Itoa(len(out.Packages))); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "Installed packages cannot be returned because their access could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePackagesUpdates(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.packagesProtectedGET(w, r, sess) {
		return
	}
	var out types.UpdateList
	if !s.packagesCall(w, r, sess, "packages.updates", packagesRequest{Family: s.packageFamily()}, &out) {
		return
	}
	if err := s.auditLog("packages.updates.read", sess.Username, r, "returned", "family="+s.packageFamily()+" updates="+strconv.Itoa(len(out.Updates))); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "Pending updates cannot be returned because their access could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePackagesSources(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.packagesProtectedGET(w, r, sess) {
		return
	}
	var out types.SourceList
	if !s.packagesCall(w, r, sess, "packages.sources", packagesRequest{Family: s.packageFamily()}, &out) {
		return
	}
	if err := s.auditLog("packages.sources.read", sess.Username, r, "returned", "family="+s.packageFamily()+" sources="+strconv.Itoa(len(out.Sources))); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "Configured sources cannot be returned because their access could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, out)
}
