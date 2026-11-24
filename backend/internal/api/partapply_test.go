// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"net/http"
	"strings"
	"testing"

	"homelab-control/backend/internal/auth"
)

func TestPartitionApplyRequiresItsOwnScope(t *testing.T) {
	fb := &fakeBroker{errs: map[string]error{}, results: map[string]any{}}
	h := harnessWith(t, fb, []string{auth.ScopeStorageRead, auth.ScopeExt4Format})
	body := `{"by_id":"/dev/disk/by-id/virtio-mmvtestdisk01","identity_kind":"serial","identity_value":"mmvtestdisk01","confirm":["partition"],"disk_bytes":8388608,"parts":[{"start_bytes":1048576,"size_bytes":1048576,"filesystem":"none"}]}`
	rr := h.post("/api/v1/storage/partition/plan", body)
	if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "storage.partition") {
		t.Fatalf("read scope: %d %s", rr.Code, rr.Body)
	}
	if len(fb.args) != 0 {
		t.Fatal("broker called without storage.partition")
	}
}

func TestPartitionApplyPlanAuditsKindAndCountOnly(t *testing.T) {
	h := harnessWith(t, &fakeBroker{errs: map[string]error{}, results: map[string]any{
		"storage.partition.plan": map[string]any{"action": "partition", "risk": "destructive", "step_up_required": true, "confirmation_required": true, "token": "hcp1.aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "expires_at": "2026-11-24T00:00:00Z", "identity_kind": "serial", "rdev": "254:0", "slice_count": 1, "effect": "partition", "consequences": []string{}, "warnings": []string{}},
	}}, []string{auth.ScopeStoragePartition})
	body := `{"by_id":"/dev/disk/by-id/virtio-mmvtestdisk01","identity_kind":"serial","identity_value":"mmvtestdisk01","confirm":["partition"],"disk_bytes":8388608,"parts":[{"start_bytes":1048576,"size_bytes":1048576,"filesystem":"none"}]}`
	rr := h.post("/api/v1/storage/partition/plan", body)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"destructive"`) {
		t.Fatalf("plan: %d %s", rr.Code, rr.Body)
	}
	if strings.Contains(h.audit.String(), "virtio") || strings.Contains(h.audit.String(), "mmvtestdisk") {
		t.Fatalf("audit leaked a path or identity: %s", h.audit.String())
	}
	if !strings.Contains(h.audit.String(), "slices=1") {
		t.Fatalf("audit: %s", h.audit.String())
	}
}
