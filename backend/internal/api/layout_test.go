// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/state"
	"homelab-control/backend/internal/storageinfo"
)

const layoutWWN = "wwn:naa.5000000000000f01"

func layoutHarness(t *testing.T, scopes []string) (*harness, *state.DB, *int) {
	t.Helper()
	h := harnessWith(t, nil, scopes)
	dbdir := t.TempDir()
	if err := os.Chmod(dbdir, 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := state.Open(filepath.Join(dbdir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	h.srv.WithLayouts(db.Layouts())
	calls := 0
	h.srv.storageCollect = func(context.Context) (storageinfo.Graph, error) {
		calls++
		return storageinfo.Build([]storageinfo.Block{
			{Name: "sda", DeviceNumber: "8:0", Kind: "block", WWID: "naa.5000000000000f01", Serial: "SER-A"},
			{Name: "sda1", DeviceNumber: "8:1", Kind: "partition", Parent: "8:0", PartUUID: "1111-01"},
			{Name: "sdb", DeviceNumber: "8:16", Kind: "block", Serial: "CLONE"},
			{Name: "sdc", DeviceNumber: "8:32", Kind: "block", Serial: "CLONE"},
		}, nil), nil
	}
	return h, db, &calls
}

func readHeaders(h *harness) map[string]string {
	return map[string]string{"X-CSRF-Token": h.csrf}
}

func writeHeaders(h *harness) map[string]string {
	return map[string]string{"Origin": testOrigin, "X-CSRF-Token": h.csrf, "Content-Type": "application/json"}
}

const chassisBody = `{"name":"Main NAS","kind":"server","rows":2,"cols":2,"cells":[{"row":0,"col":0,"kind":"hdd-3.5","label":"Bay 1"},{"row":1,"col":1,"kind":"disabled"}]}`

func decodeLayout(t *testing.T, body []byte) layoutResponse {
	t.Helper()
	var out layoutResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err, string(body))
	}
	return out
}

func TestLayoutScopesCSRFAndOrigin(t *testing.T) {
	// No session.
	h := newHarness(t)
	for _, req := range [][2]string{{"GET", "/api/v1/storage/layout"}, {"POST", "/api/v1/storage/layout/chassis"}} {
		if rr := h.do(req[0], req[1], chassisBody, map[string]string{"Origin": testOrigin, "Content-Type": "application/json"}); rr.Code != http.StatusUnauthorized {
			t.Fatalf("%v without session: %d", req, rr.Code)
		}
	}
	// storage.read never grants layout writes.
	h, _, _ = layoutHarness(t, []string{auth.ScopeStorageRead})
	if rr := h.do("GET", "/api/v1/storage/layout", "", readHeaders(h)); rr.Code != http.StatusOK {
		t.Fatal(rr.Code, rr.Body.String())
	}
	if rr := h.do("POST", "/api/v1/storage/layout/chassis", chassisBody, writeHeaders(h)); rr.Code != http.StatusForbidden {
		t.Fatal("storage.read wrote a layout:", rr.Code)
	}
	if strings.Contains(auth.DefaultScopes, auth.ScopeLayoutManage) || !auth.KnownScopes[auth.ScopeLayoutManage] {
		t.Fatal("invalid layout scope policy")
	}
	// layout.manage alone may edit the grid but not read or assign identities.
	h, _, calls := layoutHarness(t, []string{auth.ScopeLayoutManage})
	if rr := h.do("GET", "/api/v1/storage/layout", "", readHeaders(h)); rr.Code != http.StatusForbidden {
		t.Fatal("layout.manage read identities:", rr.Code)
	}
	if rr := h.do("POST", "/api/v1/storage/layout/chassis", chassisBody, writeHeaders(h)); rr.Code != http.StatusCreated {
		t.Fatal(rr.Code, rr.Body.String())
	}
	if rr := h.do("PUT", "/api/v1/storage/layout/chassis/1/bays/0/0/assignment", `{"persistent_id":"`+layoutWWN+`"}`, writeHeaders(h)); rr.Code != http.StatusForbidden || *calls != 0 {
		t.Fatal("assign without storage.read:", rr.Code, *calls)
	}
	// CSRF token and Origin are mandatory for every write.
	noCSRF := writeHeaders(h)
	delete(noCSRF, "X-CSRF-Token")
	badOrigin := writeHeaders(h)
	badOrigin["Origin"] = "http://evil.test"
	for name, hdr := range map[string]map[string]string{"no csrf": noCSRF, "bad origin": badOrigin} {
		if rr := h.do("DELETE", "/api/v1/storage/layout/chassis/1", "", hdr); rr.Code != http.StatusForbidden {
			t.Fatalf("%s: %d", name, rr.Code)
		}
	}
}

func TestLayoutWithoutStateDatabaseDoesNotPretendToPersist(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeStorageRead, auth.ScopeLayoutManage})
	if rr := h.do("GET", "/api/v1/storage/layout", "", readHeaders(h)); rr.Code != http.StatusServiceUnavailable {
		t.Fatal(rr.Code)
	}
	if rr := h.do("POST", "/api/v1/storage/layout/chassis", chassisBody, writeHeaders(h)); rr.Code != http.StatusServiceUnavailable {
		t.Fatal(rr.Code)
	}
}

func TestLayoutAssignHappyPathIsManualAuditedAndRedacted(t *testing.T) {
	h, _, calls := layoutHarness(t, []string{auth.ScopeStorageRead, auth.ScopeLayoutManage})
	if rr := h.do("POST", "/api/v1/storage/layout/chassis", chassisBody, writeHeaders(h)); rr.Code != http.StatusCreated {
		t.Fatal(rr.Code, rr.Body.String())
	}
	rr := h.do("PUT", "/api/v1/storage/layout/chassis/1/bays/0/0/assignment", `{"persistent_id":"`+layoutWWN+`"}`, writeHeaders(h))
	if rr.Code != http.StatusOK || *calls != 1 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	got := decodeLayout(t, h.do("GET", "/api/v1/storage/layout", "", readHeaders(h)).Body.Bytes())
	a := got.Chassis[0].Cells[0].Assignment
	if a == nil || a.Identity != layoutWWN || a.Provenance != "manual" || a.Confidence != "unverified" || a.AssignedBy != "full" {
		t.Fatalf("%+v", got)
	}
	log := h.audit.String()
	if !strings.Contains(log, "layout.bay.assign") || !strings.Contains(log, "requested") || !strings.Contains(log, "applied") || strings.Contains(log, "5000000000000f01") {
		t.Fatal("assignment audit missing or leaks identifier:", log)
	}
}

// Ambiguous, partition, unknown and kernel-name identities are refused, and a
// replacement drive is never silently assigned to an occupied bay.
func TestLayoutAssignRefusesUnsafeIdentitiesAndReplacement(t *testing.T) {
	h, db, _ := layoutHarness(t, []string{auth.ScopeStorageRead, auth.ScopeLayoutManage})
	if rr := h.do("POST", "/api/v1/storage/layout/chassis", chassisBody, writeHeaders(h)); rr.Code != http.StatusCreated {
		t.Fatal(rr.Code)
	}
	assign := func(bay, id string) int {
		return h.do("PUT", "/api/v1/storage/layout/chassis/1/bays/"+bay+"/assignment", `{"persistent_id":"`+id+`"}`, writeHeaders(h)).Code
	}
	cases := map[string][2]any{
		"ambiguous serial": {"serial:CLONE", http.StatusConflict},
		"not observed":     {"wwn:naa.5000000000000f99", http.StatusConflict},
		"partition":        {"partuuid:1111-01", http.StatusBadRequest},
		"kernel name":      {"sda", http.StatusBadRequest},
		"device number":    {"8:0", http.StatusBadRequest},
	}
	for name, c := range cases {
		if got := assign("0/0", c[0].(string)); got != c[1].(int) {
			t.Errorf("%s: %d, want %d", name, got, c[1])
		}
	}
	if code := assign("0/0", layoutWWN); code != http.StatusOK {
		t.Fatal(code)
	}
	// The original drive disappears and a replacement with a new identity appears.
	h.srv.storageCollect = func(context.Context) (storageinfo.Graph, error) {
		return storageinfo.Build([]storageinfo.Block{{Name: "sda", DeviceNumber: "8:0", Kind: "block", WWID: "naa.5000000000000f02"}}, nil), nil
	}
	if code := assign("0/0", "wwn:naa.5000000000000f02"); code != http.StatusConflict {
		t.Fatal("replacement overwrote an occupied bay:", code)
	}
	if code := assign("1/1", "wwn:naa.5000000000000f02"); code != http.StatusConflict {
		t.Fatal("assigned to a disabled cell:", code)
	}
	list, _ := db.Layouts().List(context.Background())
	if a := list[0].Cells[0].Assignment; a == nil || a.Identity != layoutWWN {
		t.Fatal("missing drive's stored assignment was changed", a)
	}
	// Explicit unassign, then assign: the only replacement path.
	if rr := h.do("DELETE", "/api/v1/storage/layout/chassis/1/bays/0/0/assignment", "", writeHeaders(h)); rr.Code != http.StatusOK {
		t.Fatal(rr.Code)
	}
	if code := assign("0/0", "wwn:naa.5000000000000f02"); code != http.StatusOK {
		t.Fatal(code)
	}
}

func TestLayoutWriteFailsClosedWhenAuditFails(t *testing.T) {
	h, db, _ := layoutHarness(t, []string{auth.ScopeStorageRead, auth.ScopeLayoutManage})
	h.srv.audit = audit.New(brokenWriter{})
	if rr := h.do("POST", "/api/v1/storage/layout/chassis", chassisBody, writeHeaders(h)); rr.Code != http.StatusServiceUnavailable {
		t.Fatal(rr.Code)
	}
	if list, _ := db.Layouts().List(context.Background()); len(list) != 0 {
		t.Fatal("unaudited change was saved")
	}
}

// The layout read releases stored serial/WWN identities, so it follows the
// /storage rule: CSRF before anything is read or audited, and no release
// without a durable access audit record that itself names no identifier.
func TestLayoutReadRequiresCSRFAndAuditBeforeRelease(t *testing.T) {
	h, db, _ := layoutHarness(t, []string{auth.ScopeStorageRead, auth.ScopeLayoutManage})
	if rr := h.do("POST", "/api/v1/storage/layout/chassis", chassisBody, writeHeaders(h)); rr.Code != http.StatusCreated {
		t.Fatal(rr.Code)
	}
	if rr := h.do("PUT", "/api/v1/storage/layout/chassis/1/bays/0/0/assignment", `{"persistent_id":"`+layoutWWN+`"}`, writeHeaders(h)); rr.Code != http.StatusOK {
		t.Fatal(rr.Code, rr.Body.String())
	}
	before := h.audit.String()
	for name, hdr := range map[string]map[string]string{"cookie only": nil, "wrong token": {"X-CSRF-Token": "not-the-token"}} {
		rr := h.do("GET", "/api/v1/storage/layout", "", hdr)
		if rr.Code != http.StatusForbidden || strings.Contains(rr.Body.String(), "5000000000000f01") {
			t.Fatalf("%s: %d %s", name, rr.Code, rr.Body.String())
		}
	}
	if h.audit.String() != before {
		t.Fatal("a CSRF-refused layout read was audited (audit amplification)")
	}
	rr := h.do("GET", "/api/v1/storage/layout", "", readHeaders(h))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), layoutWWN) {
		t.Fatal(rr.Code, rr.Body.String())
	}
	log := h.audit.String()
	if !strings.Contains(log, "storage.layout.read") || !strings.Contains(log, "assignments=1") || strings.Contains(log, "5000000000000f01") || strings.Contains(log, "SER-A") {
		t.Fatal("layout read audit missing or leaks identifier:", log)
	}
	// Audit write failure and absent audit: nothing is released.
	h.srv.audit = audit.New(brokenWriter{})
	if rr := h.do("GET", "/api/v1/storage/layout", "", readHeaders(h)); rr.Code != http.StatusServiceUnavailable || strings.Contains(rr.Body.String(), "5000000000000f01") {
		t.Fatal("released without audit:", rr.Code, rr.Body.String())
	}
	h.srv.audit = nil
	if rr := h.do("GET", "/api/v1/storage/layout", "", readHeaders(h)); rr.Code != http.StatusServiceUnavailable || strings.Contains(rr.Body.String(), "5000000000000f01") {
		t.Fatal("released with no audit logger:", rr.Code, rr.Body.String())
	}
	if list, _ := db.Layouts().List(context.Background()); list[0].Cells[0].Assignment == nil {
		t.Fatal("refused read changed stored state")
	}
}
