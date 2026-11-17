// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/state"
	"homelab-control/backend/internal/storageinfo"
)

// Manual drive-bay layout (M2-5 Stage A; docs/m2/M2-5-STAGE-A-CONTRACT.md).
// The layout is application state in state.db, never host state: no broker
// operation, device access or host write is involved. Assignments are
// manual and unverified, keyed by a stable persistent identity, and never
// moved or transferred implicitly.

// LayoutStore is implemented by *state.LayoutStore.
type LayoutStore interface {
	List(context.Context) ([]state.Chassis, error)
	Create(context.Context, state.ChassisSpec, time.Time) (int64, error)
	Import(context.Context, []state.ChassisSpec, time.Time) (int, error)
	Update(context.Context, int64, state.ChassisSpec, time.Time) error
	Delete(context.Context, int64) error
	Assign(ctx context.Context, id int64, row, col int, identity, by string, at time.Time) error
	Unassign(ctx context.Context, id int64, row, col int, at time.Time) error
}

// WithLayouts attaches the state database's layout store. Without it the
// layout routes answer 503: nothing pretends to persist.
func (s *Server) WithLayouts(l LayoutStore) *Server { s.layouts = l; return s }

type layoutResponse struct {
	Chassis []state.Chassis `json:"chassis"`
}

type assignRequest struct {
	PersistentID string `json:"persistent_id"`
}

func (s *Server) layoutAvailable(w http.ResponseWriter) bool {
	if s.layouts == nil {
		writeError(w, http.StatusServiceUnavailable, "state_unavailable", "The drive-bay layout requires the state database.")
		return false
	}
	if s.audit == nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "Layout changes require an audit logger.")
		return false
	}
	return true
}

func (s *Server) writeLayout(w http.ResponseWriter, r *http.Request, status int) {
	list, err := s.layouts.List(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "state_unavailable", "The drive-bay layout could not be read.")
		return
	}
	writeJSON(w, status, layoutResponse{Chassis: list})
}

// handleLayout returns the stored layout. Assignments carry stored serial/WWN
// identities, so this read follows the /storage access rule (see
// handleStorage): the session CSRF token is required before anything is read
// or audited (a cookie-only same-site request cannot grow the audit log), and
// the layout is released only after its access was durably audited. The
// audit detail carries counts only, never a serial or WWN.
func (s *Server) handleLayout(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !auth.CheckCSRF(sess, r.Header.Get("X-CSRF-Token")) {
		writeError(w, http.StatusForbidden, "csrf_rejected", "missing or invalid CSRF token")
		return
	}
	if !s.layoutAvailable(w) {
		return
	}
	list, err := s.layouts.List(r.Context())
	if err != nil {
		_ = s.auditLog("storage.layout.read", sess.Username, r, "unavailable", "")
		writeError(w, http.StatusServiceUnavailable, "state_unavailable", "The drive-bay layout could not be read.")
		return
	}
	assigned := 0
	for _, c := range list {
		for _, cell := range c.Cells {
			if cell.Assignment != nil {
				assigned++
			}
		}
	}
	detail := "chassis=" + strconv.Itoa(len(list)) + " assignments=" + strconv.Itoa(assigned)
	if err := s.auditLog("storage.layout.read", sess.Username, r, "returned", detail); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The drive-bay layout cannot be returned because its access could not be audited.")
		return
	}
	writeJSON(w, http.StatusOK, layoutResponse{Chassis: list})
}

func pathInt(r *http.Request, name string, min, max int64) (int64, bool) {
	v := r.PathValue(name)
	if v == "" || len(v) > 12 || strings.TrimLeft(v, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.ParseInt(v, 10, 64)
	return n, err == nil && n >= min && n <= max
}

func bayPath(r *http.Request) (id int64, row, col int, ok bool) {
	id, ok1 := pathInt(r, "id", 1, 1<<53)
	rr, ok2 := pathInt(r, "row", 0, state.MaxLayoutGrid-1)
	cc, ok3 := pathInt(r, "col", 0, state.MaxLayoutGrid-1)
	return id, int(rr), int(cc), ok1 && ok2 && ok3
}

// layoutWrite runs one audited change: a "requested" record must be written
// before anything changes (fail closed), then the outcome is recorded. Audit
// details name the chassis and bay, never a serial or WWN.
func (s *Server) layoutWrite(w http.ResponseWriter, r *http.Request, sess *auth.Session, event, detail string, status int, change func() error) {
	if err := s.auditLog(event, sess.Username, r, "requested", detail); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "Layout change refused: it could not be audited. Nothing was changed.")
		return
	}
	if err := change(); err != nil {
		code, name, msg := http.StatusServiceUnavailable, "state_unavailable", "The layout could not be saved. Nothing was changed."
		switch {
		case errors.Is(err, state.ErrLayoutInvalid):
			code, name, msg = http.StatusBadRequest, "invalid_layout", "The layout request is invalid or exceeds the supported limits."
		case errors.Is(err, state.ErrLayoutNotFound):
			code, name, msg = http.StatusNotFound, "not_found", "No such chassis, bay or assignment."
		case errors.Is(err, state.ErrLayoutConflict):
			code, name, msg = http.StatusConflict, "layout_conflict", "Refused: this would overwrite, move or drop an existing drive assignment. Unassign it explicitly first."
		}
		_ = s.auditLog(event, sess.Username, r, "refused", detail)
		writeError(w, code, name, msg)
		return
	}
	if err := s.auditLog(event, sess.Username, r, "applied", detail); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The layout change was saved, but its outcome could not be audited.")
		return
	}
	s.writeLayout(w, r, status)
}

func (s *Server) handleLayoutCreate(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.layoutAvailable(w) {
		return
	}
	var spec state.ChassisSpec
	if !decodeStrict(w, r, &spec) {
		return
	}
	if spec.Validate() != nil {
		writeError(w, http.StatusBadRequest, "invalid_layout", "The layout request is invalid or exceeds the supported limits.")
		return
	}
	s.layoutWrite(w, r, sess, "layout.chassis.create", "", http.StatusCreated, func() error {
		_, err := s.layouts.Create(r.Context(), spec, time.Now())
		return err
	})
}

func (s *Server) handleLayoutUpdate(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.layoutAvailable(w) {
		return
	}
	id, ok := pathInt(r, "id", 1, 1<<53)
	var spec state.ChassisSpec
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "No such chassis.")
		return
	}
	if !decodeStrict(w, r, &spec) {
		return
	}
	if spec.Validate() != nil {
		writeError(w, http.StatusBadRequest, "invalid_layout", "The layout request is invalid or exceeds the supported limits.")
		return
	}
	s.layoutWrite(w, r, sess, "layout.chassis.update", "chassis="+strconv.FormatInt(id, 10), http.StatusOK, func() error {
		return s.layouts.Update(r.Context(), id, spec, time.Now())
	})
}

func (s *Server) handleLayoutDelete(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.layoutAvailable(w) {
		return
	}
	id, ok := pathInt(r, "id", 1, 1<<53)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "No such chassis.")
		return
	}
	s.layoutWrite(w, r, sess, "layout.chassis.delete", "chassis="+strconv.FormatInt(id, 10), http.StatusOK, func() error {
		return s.layouts.Delete(r.Context(), id)
	})
}

func bayDetail(id int64, row, col int) string {
	return "chassis=" + strconv.FormatInt(id, 10) + " bay=" + strconv.Itoa(row) + "," + strconv.Itoa(col)
}

// handleLayoutAssign records a manual, unverified assignment. The drive is
// named only by its persistent identity and must be confirmed against a
// fresh observation: exactly one whole disk with that observed, unambiguous
// identity. Node IDs, device numbers and kernel names are never accepted.
func (s *Server) handleLayoutAssign(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	// Assigning reveals and relies on storage identities: storage.read is
	// required in addition to layout.manage (neither implies the other).
	if !sess.HasScope(auth.ScopeStorageRead) {
		s.auditLog("authz.denied", sess.Username, r, "denied", auth.ScopeStorageRead)
		writeError(w, http.StatusForbidden, "forbidden", "missing scope "+auth.ScopeStorageRead)
		return
	}
	if !s.layoutAvailable(w) {
		return
	}
	id, row, col, ok := bayPath(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "No such bay.")
		return
	}
	var req assignRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	if !state.ValidAssignableIdentity(req.PersistentID) {
		writeError(w, http.StatusBadRequest, "identity_not_assignable", "Only a drive with an observed WWN, NVMe namespace UUID or serial identity can be assigned.")
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
		writeError(w, http.StatusServiceUnavailable, "storage_busy", "Storage metadata collection is in progress; retry shortly.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	g, err := s.storageCollect(ctx)
	if err != nil || ctx.Err() != nil {
		writeError(w, http.StatusServiceUnavailable, "storage_unavailable", "The drive could not be confirmed against a current observation. Nothing was changed.")
		return
	}
	if !assignableNow(g, req.PersistentID) {
		writeError(w, http.StatusConflict, "identity_not_assignable", "The drive is not currently observed as exactly one whole disk with an unambiguous identity. Nothing was changed.")
		return
	}
	tier, _, _ := strings.Cut(req.PersistentID, ":")
	s.layoutWrite(w, r, sess, "layout.bay.assign", bayDetail(id, row, col)+" tier="+tier, http.StatusOK, func() error {
		return s.layouts.Assign(r.Context(), id, row, col, req.PersistentID, sess.Username, time.Now())
	})
}

// assignableNow: exactly one node carries the identity (as persistent ID or
// as any identifier), and it is a whole disk whose identity is observed and
// not duplicated.
func assignableNow(g storageinfo.Graph, identity string) bool {
	var match *storageinfo.Node
	count := 0
	for i := range g.Nodes {
		n := &g.Nodes[i]
		hit := n.PersistentID == identity
		for _, id := range n.Identifiers {
			if id.Kind+":"+id.Value == identity {
				hit = true
				if id.Ambiguous {
					return false
				}
			}
		}
		if hit {
			count++
			match = n
		}
	}
	return count == 1 && match.Kind == "block" && match.IdentityStatus == "observed" && match.PersistentID == identity
}

func (s *Server) handleLayoutUnassign(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.layoutAvailable(w) {
		return
	}
	id, row, col, ok := bayPath(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "No such bay.")
		return
	}
	s.layoutWrite(w, r, sess, "layout.bay.unassign", bayDetail(id, row, col), http.StatusOK, func() error {
		return s.layouts.Unassign(r.Context(), id, row, col, time.Now())
	})
}

// The M2-5 layout import accepts an exported chassis schema: a list of
// chassis geometry (name, kind, rows, cols, cells). Export-only fields that
// carry identity or provenance — id, created_at/updated_at, and each cell's
// assignment object — are accepted and discarded so imported geometry is
// always unassigned and can never introduce a serial or WWN. Everything else
// is strict: unknown or trailing fields are rejected.
type layoutImportRequest struct {
	Chassis []layoutImportChassis `json:"chassis"`
}

type layoutImportChassis struct {
	Name  string             `json:"name"`
	Kind  string             `json:"kind"`
	Rows  int                `json:"rows"`
	Cols  int                `json:"cols"`
	Cells []layoutImportCell `json:"cells"`
	// Discarded export-only fields.
	ID        json.RawMessage `json:"id,omitempty"`
	CreatedAt json.RawMessage `json:"created_at,omitempty"`
	UpdatedAt json.RawMessage `json:"updated_at,omitempty"`
}

type layoutImportCell struct {
	Row   int    `json:"row"`
	Col   int    `json:"col"`
	Kind  string `json:"kind"`
	Label string `json:"label"`
	// Discarded: an assignment object (identity, provenance, confidence,
	// assigned_by, assigned_at). Imported geometry is always unassigned.
	Assignment json.RawMessage `json:"assignment,omitempty"`
}

func (c layoutImportChassis) spec() state.ChassisSpec {
	cells := make([]state.CellSpec, 0, len(c.Cells))
	for _, cell := range c.Cells {
		cells = append(cells, state.CellSpec{Row: cell.Row, Col: cell.Col, Kind: cell.Kind, Label: cell.Label})
	}
	return state.ChassisSpec{Name: c.Name, Kind: c.Kind, Rows: c.Rows, Cols: c.Cols, Cells: cells}
}

// layoutImportMaxBytes bounds the import body: a full export of 32 chassis,
// each up to 24x24 cells, stays well under this. The cap is enforced before
// any parsing so an oversized body is refused without reading it all.
const layoutImportMaxBytes = 8 << 20

// handleLayoutImport imports chassis geometry additively in one atomic
// transaction. It never replaces or deletes existing chassis and never stores
// assignment or identity values. The audit detail records counts only.
func (s *Server) handleLayoutImport(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.layoutAvailable(w) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, layoutImportMaxBytes)
	var req layoutImportRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	if len(req.Chassis) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_layout", "The import contains no chassis.")
		return
	}
	specs := make([]state.ChassisSpec, 0, len(req.Chassis))
	cells := 0
	for _, c := range req.Chassis {
		spec := c.spec()
		if spec.Validate() != nil {
			writeError(w, http.StatusBadRequest, "invalid_layout", "The import is invalid or exceeds the supported limits.")
			return
		}
		cells += len(spec.Cells)
		specs = append(specs, spec)
	}
	detail := "chassis=" + strconv.Itoa(len(specs)) + " cells=" + strconv.Itoa(cells)
	s.layoutWrite(w, r, sess, "layout.import", detail, http.StatusOK, func() error {
		_, err := s.layouts.Import(r.Context(), specs, time.Now())
		return err
	})
}
