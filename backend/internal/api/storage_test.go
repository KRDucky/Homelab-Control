// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/storageinfo"
)

func TestStorageRequiresOwnScopeBeforeCollection(t *testing.T) {
	h := newHarness(t)
	calls := 0
	h.srv.storageCollect = func(context.Context) (storageinfo.Graph, error) { calls++; return storageinfo.Build(nil, nil), nil }
	if rr := h.do("GET", "/api/v1/storage", "", storageHeaders(h)); rr.Code != http.StatusUnauthorized {
		t.Fatal(rr.Code)
	}
	if rr := h.login("reader", "reader-password-123"); rr.Code != 200 {
		t.Fatal(rr.Code)
	}
	if rr := h.do("GET", "/api/v1/storage", "", storageHeaders(h)); rr.Code != http.StatusForbidden {
		t.Fatal(rr.Code)
	}
	if calls != 0 {
		t.Fatal("collected storage before authorization")
	}
	if strings.Contains(auth.DefaultScopes, auth.ScopeStorageRead) || !auth.KnownScopes[auth.ScopeStorageRead] {
		t.Fatal("invalid storage scope policy")
	}
}
func TestStorageReturnsAuditedActualCollectorOutput(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeStorageRead})
	h.srv.storageCollect = func(ctx context.Context) (storageinfo.Graph, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("collection unbounded")
		}
		return storageinfo.Build([]storageinfo.Block{{Name: "fixture-disk", DeviceNumber: "8:0", WWID: "naa.5000000000000f01"}}, nil), nil
	}
	rr := h.do("GET", "/api/v1/storage?root=/untrusted&device=/dev/sda", "", storageHeaders(h))
	if rr.Code != http.StatusOK {
		t.Fatal(rr.Code, rr.Body.String())
	}
	var out storageResponse
	if e := json.Unmarshal(rr.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	if !out.ReadOnly || out.CollectedAt.IsZero() || len(out.Graph.Nodes) != 1 || out.Graph.Nodes[0].PersistentID != "wwn:naa.5000000000000f01" {
		t.Fatal(out)
	}
	if !strings.Contains(h.audit.String(), "storage.inventory.read") || strings.Contains(h.audit.String(), "5000000000000f01") {
		t.Fatal("missing audit or identifier logged")
	}
}
func TestStorageUnavailableDoesNotLeakPrivatePaths(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeStorageRead})
	h.srv.storageCollect = func(context.Context) (storageinfo.Graph, error) {
		return storageinfo.Graph{}, errors.New("secret path /private/foo")
	}
	rr := h.do("GET", "/api/v1/storage", "", storageHeaders(h))
	if rr.Code != 503 || strings.Contains(rr.Body.String(), "/private") || strings.Contains(h.audit.String(), "/private") {
		t.Fatal(rr.Code, rr.Body.String())
	}
}
func TestStorageConcurrentCollectionRefused(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeStorageRead})
	calls := 0
	h.srv.storageCollect = func(context.Context) (storageinfo.Graph, error) { calls++; return storageinfo.Build(nil, nil), nil }
	h.srv.storageSem <- struct{}{}
	rr := h.do("GET", "/api/v1/storage", "", storageHeaders(h))
	<-h.srv.storageSem
	if rr.Code != 503 || calls != 0 {
		t.Fatal("concurrency limit not enforced")
	}
}

func TestStorageWithholdsIdentifiersWhenAuditFails(t *testing.T) {
	for _, absent := range []bool{false, true} {
		h := harnessWith(t, nil, []string{auth.ScopeStorageRead})
		h.srv.storageCollect = func(context.Context) (storageinfo.Graph, error) {
			return storageinfo.Build([]storageinfo.Block{{Name: "fixture", DeviceNumber: "8:0", Serial: "private-serial"}}, nil), nil
		}
		if absent {
			h.srv.audit = nil
		} else {
			h.srv.audit = audit.New(brokenWriter{})
		}
		rr := h.do("GET", "/api/v1/storage", "", storageHeaders(h))
		if rr.Code != 503 || strings.Contains(rr.Body.String(), "private-serial") {
			t.Fatalf("audit fail-open: %d %s", rr.Code, rr.Body.String())
		}
	}
}

func TestStorageCapabilityRequiresIndependentGrantAndExplainsPartialData(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeSystemRead})
	if c := caps(t, h); c["storage"].Status != CapForbidden {
		t.Fatal("storage capability exposed without grant")
	}
	h = harnessWith(t, nil, []string{auth.ScopeStorageRead})
	if c := caps(t, h); c["storage"].Status != CapDegraded || !strings.Contains(c["storage"].Reason, "signatures") {
		t.Fatal("partial inventory presented as complete")
	}
}

type snapshotRecorder struct {
	at    time.Time
	boot  string
	graph []byte
	fail  bool
}

func (s *snapshotRecorder) Save(_ context.Context, at time.Time, boot string, graph []byte) (int64, error) {
	s.at, s.boot, s.graph = at, boot, append([]byte(nil), graph...)
	if s.fail {
		return 0, errors.New("private database error")
	}
	return 42, nil
}
func TestStoragePersistsOnlyFreshAuditedObservations(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeStorageRead})
	store := &snapshotRecorder{}
	h.srv.WithStorageSnapshots(store)
	h.srv.storageCollect = func(context.Context) (storageinfo.Graph, error) {
		g := storageinfo.Build(nil, nil)
		g.BootID = "boot-A"
		return g, nil
	}
	rr := h.do("GET", "/api/v1/storage", "", storageHeaders(h))
	var out storageResponse
	if e := json.Unmarshal(rr.Body.Bytes(), &out); e != nil || rr.Code != 200 || out.SnapshotID != "42" || !out.SnapshotPersisted || store.at.IsZero() || store.boot != "boot-A" {
		t.Fatal(rr.Code, rr.Body.String(), e)
	}
	store.graph = nil
	h.srv.audit = audit.New(brokenWriter{})
	rr = h.do("GET", "/api/v1/storage", "", storageHeaders(h))
	if rr.Code != 503 || store.graph != nil {
		t.Fatal("persisted graph after audit failure")
	}
}
func TestStorageSnapshotWriteFailureDoesNotPretendPersistence(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeStorageRead})
	h.srv.WithStorageSnapshots(&snapshotRecorder{fail: true})
	h.srv.storageCollect = func(context.Context) (storageinfo.Graph, error) { return storageinfo.Build(nil, nil), nil }
	rr := h.do("GET", "/api/v1/storage", "", storageHeaders(h))
	if rr.Code != 503 || strings.Contains(rr.Body.String(), "private database") {
		t.Fatal(rr.Code, rr.Body.String())
	}
}

// storageHeaders is what the UI sends: the session CSRF token (see handleStorage).
func storageHeaders(h *harness) map[string]string { return map[string]string{"X-CSRF-Token": h.csrf} }

// a cookie alone (a same-site <img>, no-cors fetch, navigation
// or prefetch from another port or subdomain) cannot trigger an audited,
// persisted read. Refusals are not collected, audited or saved.
func TestStorageReadRequiresSessionCSRFToken(t *testing.T) {
	h := harnessWith(t, nil, []string{auth.ScopeStorageRead})
	calls := 0
	h.srv.storageCollect = func(context.Context) (storageinfo.Graph, error) { calls++; return storageinfo.Build(nil, nil), nil }
	store := &recordingSnapshots{}
	h.srv.WithStorageSnapshots(store)
	before := h.audit.Len()
	for name, hdr := range map[string]map[string]string{
		"cookie only":         nil,
		"wrong token":         {"X-CSRF-Token": "not-the-session-token"},
		"same-site image":     {"Sec-Fetch-Site": "same-site", "Sec-Fetch-Dest": "image"},
		"prefetch":            {"Sec-Purpose": "prefetch", "Sec-Fetch-Site": "same-origin"},
		"token in query only": nil,
	} {
		path := "/api/v1/storage"
		if name == "token in query only" {
			path += "?csrf=" + h.csrf
		}
		rr := h.do("GET", path, "", hdr)
		if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "csrf_rejected") {
			t.Fatalf("%s: %d %s", name, rr.Code, rr.Body.String())
		}
	}
	if calls != 0 || store.saves != 0 || h.audit.Len() != before {
		t.Fatalf("refused reads had side effects: collect=%d saves=%d audit grew=%v", calls, store.saves, h.audit.Len() != before)
	}
	if rr := h.do("GET", "/api/v1/storage", "", storageHeaders(h)); rr.Code != http.StatusOK || calls != 1 || store.saves != 1 {
		t.Fatalf("token-bearing read refused: %d", rr.Code)
	}
}

type recordingSnapshots struct{ saves int }

func (r *recordingSnapshots) Save(context.Context, time.Time, string, []byte) (int64, error) {
	r.saves++
	return int64(r.saves), nil
}
