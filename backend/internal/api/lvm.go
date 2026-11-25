// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"bytes"
	"context"
	"net/http"
	"strconv"
	"time"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/lvminfo"
	"homelab-control/backend/internal/storageinfo"
	"homelab-control/backend/internal/storagetools"
)

// lvmProtectedGET is the read-only LVM bar on an identifier-bearing GET.
// The checks live in auditedGET; this wrapper keeps the LVM audit message.
func (s *Server) lvmProtectedGET(w http.ResponseWriter, r *http.Request, sess *auth.Session) bool {
	return s.auditedGET(w, r, sess, "LVM inventory requires an access audit logger.")
}

// handleLVM serves the read-only LVM view: tooling and kernel device-mapper
// (unprivileged), and, when lvm2 is installed and the broker is reachable,
// volume groups, logical volumes and physical volumes from pvs, lvs and vgs.
// No PV, VG or LV create, change or remove exists.
func (s *Server) handleLVM(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.lvmProtectedGET(w, r, sess) {
		return
	}
	if s.lvmCollect == nil || s.lvmSem == nil {
		writeError(w, http.StatusServiceUnavailable, "lvm_unavailable", "LVM inventory is unavailable.")
		return
	}
	select {
	case s.lvmSem <- struct{}{}:
		defer func() { <-s.lvmSem }()
	default:
		writeError(w, http.StatusServiceUnavailable, "lvm_busy", "LVM inventory is already in progress; retry shortly.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	snap, err := s.lvmCollect(ctx)
	if err != nil {
		_ = s.auditLog("lvm.inventory.read", sess.Username, r, "unavailable", "")
		writeError(w, http.StatusServiceUnavailable, "lvm_unavailable", "LVM inventory could not be collected safely.")
		return
	}
	resp := lvmResponse{Snapshot: snap, Status: lvmView(toolReport[storagetools.LVMReport](r.Context(), s, sess, "lvm.status",
		snap.Tooling.RequiredInstalled("lvm"), "lvm2 is not installed, so there are no LVM reports.", "LVM status"))}
	detail := "vgs=0 lvs=0 pvs=0"
	if rep := resp.Status.Report; rep != nil {
		detail = "vgs=" + strconv.Itoa(len(rep.VolumeGroups)) + " lvs=" + strconv.Itoa(len(rep.Volumes)) + " pvs=" + strconv.Itoa(len(rep.PhysicalVolumes))
	}
	if err := s.auditLog("lvm.inventory.read", sess.Username, r, "returned", detail); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "LVM inventory cannot be returned because its access could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// lvmResponse is the unprivileged tooling and kernel probe plus the pvs, lvs
// and vgs reports the broker's lvm.status op returns.
type lvmResponse struct {
	lvminfo.Snapshot
	Status toolStatus[lvmReportView] `json:"status"`
}

// lvmReportView is the parsed LVM reports, as the Storage page decodes them.
type lvmReportView struct {
	VolumeGroups    []storageinfo.VolumeGroup    `json:"volume_groups"`
	Volumes         []storageinfo.LogicalVolume  `json:"volumes"`
	PhysicalVolumes []storageinfo.PhysicalVolume `json:"physical_volumes"`
}

// lvmView decodes the raw reports. A report that does not parse makes the
// status unavailable with the parser's reason.
func lvmView(st toolStatus[storagetools.LVMReport]) toolStatus[lvmReportView] {
	if !st.Available || st.Report == nil {
		return toolStatus[lvmReportView]{Reason: st.Reason}
	}
	vgs, err := storageinfo.ParseVGS(bytes.NewReader(st.Report.VGS))
	var inv storageinfo.LVMInventory
	if err == nil {
		inv, err = storageinfo.ParseLVM(bytes.NewReader(st.Report.LVS), storageinfo.ModeMetadataOnly)
	}
	if err == nil {
		inv.PhysicalVolumes, err = storageinfo.ParsePVS(bytes.NewReader(st.Report.PVS), storageinfo.ModeMetadataOnly)
	}
	if err != nil {
		return toolStatus[lvmReportView]{Reason: "LVM status is unavailable: the LVM report could not be read: " + err.Error()}
	}
	return toolStatus[lvmReportView]{Available: true, Report: &lvmReportView{VolumeGroups: vgs, Volumes: inv.Volumes, PhysicalVolumes: inv.PhysicalVolumes}}
}
