// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/storageinfo"
)

// storageProbeResult mirrors the broker op's closed helper Result. It is
// decoded from the broker call and never exposed as the endpoint response.
type storageProbeResult struct {
	Version int               `json:"version"`
	Code    string            `json:"code,omitempty"`
	Blkid   map[string]string `json:"blkid,omitempty"`
	Errors  []string          `json:"errors,omitempty"`
	Partial bool              `json:"partial"`
}

// handleStorageProbe triggers the on-demand native probe (O-1). It is an
// explicit CSRF-protected POST under the non-default storage.probe scope; it
// is never attached to a polling GET. The body must be empty: devices are
// enumerated server-side. The result is the same snake_case storageResponse
// as GET /storage, with the probe's partial/refusal state folded into
// graph.status and graph.notes and blkid signatures merged onto nodes.
func (s *Server) handleStorageProbe(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !auth.CheckCSRF(sess, r.Header.Get("X-CSRF-Token")) {
		writeError(w, http.StatusForbidden, "csrf_rejected", "missing or invalid CSRF token")
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1))
	if len(body) > 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "this endpoint takes no request body")
		return
	}
	if s.audit == nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The probe cannot run because its access could not be audited.")
		return
	}
	if s.storageCollect == nil || s.storageSem == nil {
		writeError(w, http.StatusServiceUnavailable, "storage_unavailable", "Storage collection is unavailable.")
		return
	}

	// Count-only audit (never identifiers); the probe wakes disks.
	if err := s.auditLog("storage.probe", sess.Username, r, "requested", ""); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The probe cannot run because its access could not be audited.")
		return
	}

	// Single-flight: one probe at a time.
	select {
	case s.storageSem <- struct{}{}:
		defer func() { <-s.storageSem }()
	default:
		writeError(w, http.StatusConflict, "conflict", "A storage probe is already in progress.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	var probe storageProbeResult
	if err := s.broker.Call(ctx, "storage.probe", actorFor(sess), &probe); err != nil {
		_ = s.auditLog("storage.probe", sess.Username, r, "unavailable", "")
		brokerError(w, err)
		return
	}
	// A typed helper refusal is an HTTP 503, not a 200 with graph.status.
	if probe.Code == "device_policy_unresolved" {
		_ = s.auditLog("storage.probe", sess.Username, r, "unavailable", "device_policy_unresolved")
		writeError(w, http.StatusServiceUnavailable, "storage_probe_unavailable", "The storage probe helper refused: required block-device classes are not available.")
		return
	}

	// Collect the unprivileged base graph, then fold in the probe outcome.
	collectedAt := time.Now().UTC()
	g, err := s.storageCollect(ctx)
	if err != nil || ctx.Err() != nil {
		_ = s.auditLog("storage.probe", sess.Username, r, "unavailable", "")
		writeError(w, http.StatusServiceUnavailable, "storage_unavailable", "Storage metadata could not be collected safely.")
		return
	}
	mergeProbeSignatures(&g, probe.Blkid)
	applyProbeOutcome(&g, probe)

	if err = s.auditLog("storage.probe", sess.Username, r, "collected", "nodes="+strconv.Itoa(len(g.Nodes))); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The probe result cannot be returned because its access could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, storageResponse{CollectedAt: collectedAt, ReadOnly: true, Graph: g})
}

// mergeProbeSignatures folds blkid TYPE/UUID into the graph as signature nodes,
// claims and has-signature edges (consistent with storageinfo.Build), never as
// bare fields on block nodes.
func mergeProbeSignatures(g *storageinfo.Graph, blkid map[string]string) {
	if len(blkid) == 0 {
		return
	}
	sigs := make(map[string]storageinfo.BlkidSignature, len(blkid))
	for name, export := range blkid {
		fsType, uuid := parseBlkidExport(export)
		sigs[name] = storageinfo.BlkidSignature{FSType: fsType, UUID: uuid}
	}
	storageinfo.MergeBlkidSignatures(g, sigs)
}

// applyProbeOutcome sets graph.status/notes from the typed helper result
// without erasing a worse base status. device_policy_unresolved is
// "unavailable", Partial is "partial", otherwise "ok"; the worse of the base
// and probe status wins, and helper errors are appended as notes.
func applyProbeOutcome(g *storageinfo.Graph, probe storageProbeResult) {
	probeStatus := "ok"
	switch probe.Code {
	case "device_policy_unresolved":
		probeStatus = "unavailable"
	default:
		if probe.Partial {
			probeStatus = "partial"
		}
	}
	if statusRank(probeStatus) > statusRank(g.Status) {
		g.Status = probeStatus
	}
	g.Notes = append(g.Notes, probe.Errors...)
}

func statusRank(s string) int {
	switch s {
	case "partial":
		return 1
	case "unavailable":
		return 2
	default:
		return 0 // "ok" and empty
	}
}

// parseBlkidExport reads the allow-listed TYPE and UUID from a blkid -o export
// value (one KEY=VALUE per line). Other keys are ignored.
func parseBlkidExport(export string) (fsType, uuid string) {
	for _, line := range strings.Split(export, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "TYPE":
			fsType = v
		case "UUID":
			uuid = v
		}
	}
	return fsType, uuid
}
