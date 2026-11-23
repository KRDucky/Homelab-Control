// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/partplan"
	"homelab-control/backend/internal/storageinfo"
)

func TestPartitionDryRunRequiresScopeAndCSRF(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeSystemRead})
	h.srv.storageCollect = func(context.Context) (storageinfo.Graph, error) {
		t.Fatal("dry run collected storage")
		return storageinfo.Graph{}, nil
	}
	body := `{"disk_bytes":1048576,"parts":[{"start_bytes":0,"size_bytes":1048576,"filesystem":"none"}]}`
	hdr := map[string]string{"Content-Type": "application/json", "Origin": testOrigin, "X-CSRF-Token": h.csrf}
	if rr := h.do("POST", "/api/v1/storage/plan/dry-run", body, hdr); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "missing scope storage.read") {
		t.Fatalf("scope: %d %s", rr.Code, rr.Body)
	}
	h = harnessWith(t, nil, []string{auth.ScopeStorageRead})
	h.srv.storageCollect = func(context.Context) (storageinfo.Graph, error) {
		t.Fatal("dry run collected storage")
		return storageinfo.Graph{}, nil
	}
	if rr := h.do("POST", "/api/v1/storage/plan/dry-run", body, map[string]string{"Content-Type": "application/json", "Origin": testOrigin}); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "csrf_rejected") {
		t.Fatalf("csrf: %d %s", rr.Code, rr.Body)
	}
}

func TestPartitionDryRunRejectsUnknownFields(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeStorageRead})
	body := `{"disk_bytes":1048576,"parts":[{"start_bytes":0,"size_bytes":1048576,"filesystem":"none","extra":true}]}`
	rr := h.do("POST", "/api/v1/storage/plan/dry-run", body, map[string]string{"Content-Type": "application/json", "Origin": testOrigin, "X-CSRF-Token": h.csrf})
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "invalid_body") {
		t.Fatalf("%d %s", rr.Code, rr.Body)
	}
	if strings.Contains(h.audit.String(), "extra") || !strings.Contains(h.audit.String(), "storage.plan.dry-run") {
		t.Fatalf("audit %s", h.audit)
	}
}

func TestPartitionDryRunValidatesWithoutExecuting(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeStorageRead})
	calls := 0
	h.srv.storageCollect = func(context.Context) (storageinfo.Graph, error) {
		calls++
		return storageinfo.Graph{}, nil
	}
	body := `{"disk_bytes":4194304,"parts":[{"start_bytes":1048576,"size_bytes":1048576,"filesystem":"ext4"}]}`
	rr := h.do("POST", "/api/v1/storage/plan/dry-run", body, map[string]string{"Content-Type": "application/json", "Origin": testOrigin, "X-CSRF-Token": h.csrf})
	if rr.Code != http.StatusOK {
		t.Fatalf("%d %s", rr.Code, rr.Body)
	}
	var rep partplan.Report
	if err := json.Unmarshal(rr.Body.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if !rep.OK || rep.Executes || calls != 0 {
		t.Fatalf("ok=%v executes=%v collect=%d", rep.OK, rep.Executes, calls)
	}
	var format bool
	for _, step := range rep.Steps {
		if step.Executable {
			t.Fatal("executable step")
		}
		if step.Action == "format" && step.Destructive && strings.Contains(step.Detail, "ext4.format on a whole by-id disk, not this slice") {
			format = true
		}
	}
	if !format {
		t.Fatalf("steps %+v", rep.Steps)
	}
	audit := h.audit.String()
	if !strings.Contains(audit, "storage.plan.dry-run") || !strings.Contains(audit, "parts=1") || strings.Contains(audit, "filesystem") || strings.Contains(audit, "by-id") {
		t.Fatalf("audit leaked the proposal: %s", audit)
	}
	overlap := `{"disk_bytes":2097152,"parts":[{"start_bytes":0,"size_bytes":2097152,"filesystem":"none"},{"start_bytes":1048576,"size_bytes":1048576,"filesystem":"none"}]}`
	rr = h.do("POST", "/api/v1/storage/plan/dry-run", overlap, map[string]string{"Content-Type": "application/json", "Origin": testOrigin, "X-CSRF-Token": h.csrf})
	if rr.Code != http.StatusOK {
		t.Fatalf("overlap status %d %s", rr.Code, rr.Body)
	}
	rep = partplan.Report{}
	if err := json.Unmarshal(rr.Body.Bytes(), &rep); err != nil || rep.OK || rep.Executes {
		t.Fatalf("%v %+v", err, rep)
	}
	if !strings.Contains(h.audit.String(), "parts=2") {
		t.Fatalf("audit %s", h.audit)
	}
}
