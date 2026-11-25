// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"time"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/storageinfo"
	"homelab-control/backend/internal/storagetools"
)

// storageProbeResult is the broker op's closed helper Result.
type storageProbeResult = storagetools.Result

// handleStorageProbe triggers the on-demand native probe (O-1). It is an
// explicit CSRF-protected POST under the non-default storage.probe scope; it
// is never attached to a polling GET. The body must be empty: devices are
// enumerated by the helper. The result is the same snake_case storageResponse
// as GET /storage, with the probe's partial/refusal state folded into
// graph.status and graph.notes, lsblk signatures merged onto nodes, SMART
// readings on disks, and the pvs/lvs inventory.
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

	// Single-flight: one probe at a time. A busy refusal does not wake disks,
	// but the request was already counted, so close that row. Leaving it as
	// "requested" would look like a probe that never finished.
	select {
	case s.storageSem <- struct{}{}:
		defer func() { <-s.storageSem }()
	default:
		if err := s.auditLog("storage.probe", sess.Username, r, "conflict", ""); err != nil {
			writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The probe refusal could not be audited.")
			return
		}
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
	mergeProbeSignatures(&g, probe.Blocks)
	storageinfo.AttachSMART(&g, probe.SMART)
	addProbeLVM(&g, probe)
	applyProbeOutcome(&g, probe)

	if err = s.auditLog("storage.probe", sess.Username, r, "collected", "nodes="+strconv.Itoa(len(g.Nodes))); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The probe result cannot be returned because its access could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, storageResponse{CollectedAt: collectedAt, ReadOnly: true, Graph: g})
}

// mergeProbeSignatures folds lsblk FSTYPE/UUID into the graph as signature
// nodes, claims and has-signature edges (consistent with storageinfo.Build).
func mergeProbeSignatures(g *storageinfo.Graph, blocks []storagetools.BlockDevice) {
	sigs := make(map[string]storageinfo.Signature, len(blocks))
	for _, b := range blocks {
		if b.FSType != "" && b.UUID != "" {
			sigs[b.KName] = storageinfo.Signature{FSType: b.FSType, UUID: b.UUID}
		}
	}
	storageinfo.MergeSignatures(g, sigs, "lsblk")
	if len(blocks) == 0 {
		return
	}
	notes := g.Notes[:0]
	for _, n := range g.Notes {
		if n != storageinfo.NoteSignaturesNotProbed {
			notes = append(notes, n)
		}
	}
	g.Notes = notes
}

// addProbeLVM decodes the helper's pvs/lvs --readonly reports. They are
// metadata-only: --readonly does not query device-mapper, so live RAID sync,
// cache and health state are not shown. PV-to-disk links are decided by
// storageinfo from device numbers.
func addProbeLVM(g *storageinfo.Graph, probe storageProbeResult) {
	if len(probe.LVS) == 0 || len(probe.PVS) == 0 {
		return
	}
	inv, err := storageinfo.ParseLVM(bytes.NewReader(probe.LVS), storageinfo.ModeMetadataOnly)
	if err == nil {
		inv.PhysicalVolumes, err = storageinfo.ParsePVS(bytes.NewReader(probe.PVS), storageinfo.ModeMetadataOnly)
	}
	if err != nil {
		g.Notes = append(g.Notes, "LVM report could not be read: "+err.Error())
		if statusRank("partial") > statusRank(g.Status) {
			g.Status = "partial"
		}
		return
	}
	storageinfo.AddLVM(g, inv)
}

// applyProbeOutcome sets graph.status/notes from the typed helper result
// without erasing a worse base status. device_policy_unresolved is
// "unavailable", Partial is "partial", otherwise "ok"; the worse of the base
// and probe status wins, and helper errors are appended as notes.
func applyProbeOutcome(g *storageinfo.Graph, probe storageProbeResult) {
	probeStatus := "ok"
	switch probe.Code {
	case storagetools.CodeDevicePolicyUnresolved:
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
