// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// Manual drive-bay layout (M2-5 Stage A). Everything here is an
// administrator's statement stored by the application: provenance is always
// "manual" and confidence always "unverified". Nothing is read from or
// written to the host. Assignments are keyed by a stable persistent identity
// (never a kernel name, device number or snapshot node ID) and are never
// moved, transferred or dropped implicitly.

const (
	MaxLayoutGrid    = 24
	MaxLayoutChassis = 32
	MaxLayoutLabel   = 64
	maxIdentityLen   = 512
)

var (
	ErrLayoutNotFound = errors.New("layout: not found")
	ErrLayoutConflict = errors.New("layout: conflicts with an existing assignment")
	ErrLayoutInvalid  = errors.New("layout: invalid request")
)

var chassisKinds = map[string]bool{"server": true, "shelf": true, "external": true, "desktop": true}

// BayKinds hold drives. Every other cell kind cannot carry an assignment.
var BayKinds = map[string]bool{"hdd-3.5": true, "ssd-2.5": true, "nvme-m2": true, "nvme-u2": true, "tray": true}
var cellKinds = map[string]bool{"hdd-3.5": true, "ssd-2.5": true, "nvme-m2": true, "nvme-u2": true, "tray": true, "empty": true, "disabled": true, "reserved": true}

// AssignableIdentityPrefixes are the persistent-identity tiers accepted for
// a manual assignment: world-wide names, NVMe namespace UUIDs and device
// serials. Weaker tiers (by-id, vendor-derived, unclassified) are refused.
var AssignableIdentityPrefixes = []string{"wwn:", "nvme-uuid:", "serial:"}

type LayoutAssignment struct {
	Identity   string    `json:"identity"`
	Provenance string    `json:"provenance"`
	Confidence string    `json:"confidence"`
	AssignedBy string    `json:"assigned_by"`
	AssignedAt time.Time `json:"assigned_at"`
}

type LayoutCell struct {
	Row        int               `json:"row"`
	Col        int               `json:"col"`
	Kind       string            `json:"kind"`
	Label      string            `json:"label"`
	Assignment *LayoutAssignment `json:"assignment,omitempty"`
}

type Chassis struct {
	ID        int64        `json:"id"`
	Name      string       `json:"name"`
	Kind      string       `json:"kind"`
	Rows      int          `json:"rows"`
	Cols      int          `json:"cols"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
	Cells     []LayoutCell `json:"cells"`
}

// CellSpec and ChassisSpec are caller input. Positions not listed default to
// an unlabelled "hdd-3.5" bay.
type CellSpec struct {
	Row   int    `json:"row"`
	Col   int    `json:"col"`
	Kind  string `json:"kind"`
	Label string `json:"label"`
}

type ChassisSpec struct {
	Name  string     `json:"name"`
	Kind  string     `json:"kind"`
	Rows  int        `json:"rows"`
	Cols  int        `json:"cols"`
	Cells []CellSpec `json:"cells"`
}

func printable(s string, max int) bool {
	if len(s) > max*4 || len([]rune(s)) > max {
		return false
	}
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// Validate checks bounds, kinds, labels and duplicate positions.
func (s ChassisSpec) Validate() error {
	name := strings.TrimSpace(s.Name)
	if name == "" || name != s.Name || !printable(s.Name, MaxLayoutLabel) || !chassisKinds[s.Kind] ||
		s.Rows < 1 || s.Rows > MaxLayoutGrid || s.Cols < 1 || s.Cols > MaxLayoutGrid || len(s.Cells) > s.Rows*s.Cols {
		return ErrLayoutInvalid
	}
	seen := map[[2]int]bool{}
	for _, c := range s.Cells {
		p := [2]int{c.Row, c.Col}
		if c.Row < 0 || c.Row >= s.Rows || c.Col < 0 || c.Col >= s.Cols || seen[p] || !cellKinds[c.Kind] || !printable(c.Label, MaxLayoutLabel) {
			return ErrLayoutInvalid
		}
		seen[p] = true
	}
	return nil
}

// ValidAssignableIdentity reports whether id has an accepted tier and form.
func ValidAssignableIdentity(id string) bool {
	if len(id) == 0 || len(id) > maxIdentityLen || !printable(id, maxIdentityLen) {
		return false
	}
	for _, p := range AssignableIdentityPrefixes {
		if strings.HasPrefix(id, p) && len(id) > len(p) {
			return true
		}
	}
	return false
}

func (s ChassisSpec) cells() []CellSpec {
	listed := map[[2]int]CellSpec{}
	for _, c := range s.Cells {
		listed[[2]int{c.Row, c.Col}] = c
	}
	out := make([]CellSpec, 0, s.Rows*s.Cols)
	for r := 0; r < s.Rows; r++ {
		for c := 0; c < s.Cols; c++ {
			cell, ok := listed[[2]int{r, c}]
			if !ok {
				cell = CellSpec{Row: r, Col: c, Kind: "hdd-3.5"}
			}
			out = append(out, cell)
		}
	}
	return out
}

type LayoutStore struct{ db *DB }

func (d *DB) Layouts() *LayoutStore { return &LayoutStore{db: d} }

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// List returns every chassis with its cells and assignments.
func (l *LayoutStore) List(ctx context.Context) ([]Chassis, error) {
	rows, err := l.db.sql.QueryContext(ctx, `SELECT id,name,kind,row_count,col_count,created_at,updated_at FROM layout_chassis ORDER BY id`)
	if err != nil {
		return nil, err
	}
	out := []Chassis{}
	index := map[int64]int{}
	for rows.Next() {
		var c Chassis
		var created, updated string
		if err := rows.Scan(&c.ID, &c.Name, &c.Kind, &c.Rows, &c.Cols, &created, &updated); err != nil {
			rows.Close()
			return nil, err
		}
		c.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		c.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		c.Cells = []LayoutCell{}
		index[c.ID] = len(out)
		out = append(out, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	cells, err := l.db.sql.QueryContext(ctx, `SELECT c.chassis_id,c.row_index,c.col_index,c.kind,c.label,
		a.identity,a.provenance,a.confidence,a.assigned_by,a.assigned_at
		FROM layout_cells c LEFT JOIN layout_assignments a
		ON a.chassis_id=c.chassis_id AND a.row_index=c.row_index AND a.col_index=c.col_index
		ORDER BY c.chassis_id,c.row_index,c.col_index`)
	if err != nil {
		return nil, err
	}
	defer cells.Close()
	for cells.Next() {
		var id int64
		var cell LayoutCell
		var identity, provenance, confidence, by, at sql.NullString
		if err := cells.Scan(&id, &cell.Row, &cell.Col, &cell.Kind, &cell.Label, &identity, &provenance, &confidence, &by, &at); err != nil {
			return nil, err
		}
		if identity.Valid {
			when, _ := time.Parse(time.RFC3339Nano, at.String)
			cell.Assignment = &LayoutAssignment{Identity: identity.String, Provenance: provenance.String, Confidence: confidence.String, AssignedBy: by.String, AssignedAt: when}
		}
		if i, ok := index[id]; ok {
			out[i].Cells = append(out[i].Cells, cell)
		}
	}
	return out, cells.Err()
}

// Create adds a chassis and all of its cells in one transaction.
func (l *LayoutStore) Create(ctx context.Context, spec ChassisSpec, at time.Time) (int64, error) {
	if err := spec.Validate(); err != nil {
		return 0, err
	}
	tx, err := l.db.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM layout_chassis`).Scan(&count); err != nil {
		return 0, err
	}
	if count >= MaxLayoutChassis {
		return 0, ErrLayoutInvalid
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO layout_chassis(name,kind,row_count,col_count,created_at,updated_at) VALUES (?,?,?,?,?,?)`,
		spec.Name, spec.Kind, spec.Rows, spec.Cols, stamp(at), stamp(at))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := upsertCells(ctx, tx, id, spec); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func upsertCells(ctx context.Context, tx *sql.Tx, id int64, spec ChassisSpec) error {
	for _, c := range spec.cells() {
		if _, err := tx.ExecContext(ctx, `INSERT INTO layout_cells(chassis_id,row_index,col_index,kind,label) VALUES (?,?,?,?,?)
			ON CONFLICT(chassis_id,row_index,col_index) DO UPDATE SET kind=excluded.kind, label=excluded.label`,
			id, c.Row, c.Col, c.Kind, c.Label); err != nil {
			return err
		}
	}
	return nil
}

// Update replaces a chassis' name, kind, size and cells. It is refused when
// any assigned bay would leave the grid or stop being a bay.
func (l *LayoutStore) Update(ctx context.Context, id int64, spec ChassisSpec, at time.Time) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	tx, err := l.db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	res, err := tx.ExecContext(ctx, `UPDATE layout_chassis SET name=?,kind=?,row_count=?,col_count=?,updated_at=? WHERE id=?`,
		spec.Name, spec.Kind, spec.Rows, spec.Cols, stamp(at), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrLayoutNotFound
	}
	next := map[[2]int]string{}
	for _, c := range spec.cells() {
		next[[2]int{c.Row, c.Col}] = c.Kind
	}
	assigned, err := tx.QueryContext(ctx, `SELECT row_index,col_index FROM layout_assignments WHERE chassis_id=?`, id)
	if err != nil {
		return err
	}
	for assigned.Next() {
		var r, c int
		if err := assigned.Scan(&r, &c); err != nil {
			assigned.Close()
			return err
		}
		if kind, ok := next[[2]int{r, c}]; !ok || !BayKinds[kind] {
			assigned.Close()
			return ErrLayoutConflict
		}
	}
	assigned.Close()
	if err := assigned.Err(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM layout_cells WHERE chassis_id=? AND (row_index>=? OR col_index>=?)`, id, spec.Rows, spec.Cols); err != nil {
		return err
	}
	if err := upsertCells(ctx, tx, id, spec); err != nil {
		return err
	}
	return tx.Commit()
}

// Delete removes a chassis without assignments.
func (l *LayoutStore) Delete(ctx context.Context, id int64) error {
	tx, err := l.db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM layout_chassis WHERE id=?`, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrLayoutNotFound
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM layout_assignments WHERE chassis_id=?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrLayoutConflict
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM layout_cells WHERE chassis_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM layout_chassis WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// Assign records a manual, unverified assignment. An occupied bay, a non-bay
// cell or an identity already assigned elsewhere is a conflict: nothing is
// overwritten or moved.
func (l *LayoutStore) Assign(ctx context.Context, id int64, row, col int, identity, by string, at time.Time) error {
	if !ValidAssignableIdentity(identity) || by == "" {
		return ErrLayoutInvalid
	}
	tx, err := l.db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	var kind string
	err = tx.QueryRowContext(ctx, `SELECT kind FROM layout_cells WHERE chassis_id=? AND row_index=? AND col_index=?`, id, row, col).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrLayoutNotFound
	}
	if err != nil {
		return err
	}
	if !BayKinds[kind] {
		return ErrLayoutConflict
	}
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM layout_assignments WHERE (chassis_id=? AND row_index=? AND col_index=?) OR identity=?`, id, row, col, identity).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrLayoutConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO layout_assignments(chassis_id,row_index,col_index,identity,provenance,confidence,assigned_by,assigned_at) VALUES (?,?,?,?,'manual','unverified',?,?)`,
		id, row, col, identity, by, stamp(at)); err != nil {
		return fmt.Errorf("record assignment: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE layout_chassis SET updated_at=? WHERE id=?`, stamp(at), id); err != nil {
		return err
	}
	return tx.Commit()
}

// Unassign removes one bay's assignment, whether or not the drive is present.
func (l *LayoutStore) Unassign(ctx context.Context, id int64, row, col int, at time.Time) error {
	tx, err := l.db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	res, err := tx.ExecContext(ctx, `DELETE FROM layout_assignments WHERE chassis_id=? AND row_index=? AND col_index=?`, id, row, col)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrLayoutNotFound
	}
	if _, err := tx.ExecContext(ctx, `UPDATE layout_chassis SET updated_at=? WHERE id=?`, stamp(at), id); err != nil {
		return err
	}
	return tx.Commit()
}
