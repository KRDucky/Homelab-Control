// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/storageinfo"
)

// StorageSnapshotStore persists only already authorized observations.
type StorageSnapshotStore interface {
	Save(context.Context, time.Time, string, []byte) (int64, error)
}

// WithStorageSnapshots attaches the existing application state database.
func (s *Server) WithStorageSnapshots(store StorageSnapshotStore) *Server {
	s.storageSnapshots = store
	return s
}

type storageResponse struct {
	SnapshotID        string            `json:"snapshot_id,omitempty"`
	SnapshotPersisted bool              `json:"snapshot_persisted"`
	CollectedAt       time.Time         `json:"collected_at"`
	ReadOnly          bool              `json:"read_only"`
	Graph             storageinfo.Graph `json:"graph"`
}

// handleStorage exposes only fixed-path unprivileged metadata. Root selection,
// block paths, command arguments and host writes are never request parameters.
//
// This GET has side effects inside the application (an audit row, and a
// bounded snapshot), so it requires the per-session CSRF token like an unsafe
// method. SameSite=Strict alone does not stop a same-site page: SameSite
// ignores ports, so another web application on this host (or a sibling
// subdomain) can make the browser send the session cookie with an <img>,
// no-cors fetch, navigation or prefetch. None of those can attach a custom
// header without a CORS preflight, which this server never grants, and the
// token is readable only by this origin. A refused request is not collected,
// audited or saved, so it cannot be used to grow the audit log either.
func (s *Server) handleStorage(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !auth.CheckCSRF(sess, r.Header.Get("X-CSRF-Token")) {
		writeError(w, http.StatusForbidden, "csrf_rejected", "missing or invalid CSRF token")
		return
	}
	if s.audit == nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "Storage metadata requires an access audit logger.")
		return
	}
	if s.storageCollect == nil || s.storageSem == nil {
		writeError(w, http.StatusServiceUnavailable, "storage_unavailable", "Storage metadata collection is unavailable.")
		return
	}
	select {
	case s.storageSem <- struct{}{}:
		defer func() { <-s.storageSem }()
	default:
		writeError(w, http.StatusServiceUnavailable, "storage_busy", "Storage metadata collection is already in progress; retry shortly.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	collectedAt := time.Now().UTC()
	g, err := s.storageCollect(ctx)
	if err != nil || ctx.Err() != nil {
		_ = s.auditLog("storage.inventory.read", sess.Username, r, "unavailable", "")
		writeError(w, http.StatusServiceUnavailable, "storage_unavailable", "Storage metadata could not be collected safely. No devices or mountpoints were modified.")
		return
	}
	if err = s.auditLog("storage.inventory.read", sess.Username, r, "collected", "nodes="+strconv.Itoa(len(g.Nodes))); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "Storage metadata cannot be returned because its access could not be audited.")
		return
	}
	response := storageResponse{CollectedAt: collectedAt, ReadOnly: true, Graph: g}
	if s.storageSnapshots != nil {
		graphBytes, encodeError := json.Marshal(g)
		if encodeError != nil {
			writeError(w, http.StatusServiceUnavailable, "storage_unavailable", "Storage metadata could not be encoded safely.")
			return
		}
		id, saveError := s.storageSnapshots.Save(ctx, response.CollectedAt, g.BootID, graphBytes)
		if saveError != nil {
			_ = s.auditLog("storage.snapshot.save", sess.Username, r, "failed", "")
			writeError(w, http.StatusServiceUnavailable, "state_unavailable", "Storage metadata could not be preserved in the state database; retry after checking its availability.")
			return
		}
		response.SnapshotID = strconv.FormatInt(id, 10)
		response.SnapshotPersisted = true
	}
	writeJSON(w, http.StatusOK, response)
}
