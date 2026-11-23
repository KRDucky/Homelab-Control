// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"homelab-control/backend/internal/auth"
)

func TestAuditedGETKeepsEachWrapperMessage(t *testing.T) {
	h := newHarness(t)
	logger := h.srv.audit
	sess := &auth.Session{CSRFToken: "good-token"}
	wrappers := []struct {
		name string
		call func(http.ResponseWriter, *http.Request, *auth.Session) bool
		msg  string
	}{
		{"files", h.srv.filesProtectedGET, "File listing requires an access audit logger."},
		{"packages", h.srv.packagesProtectedGET, "Package queries require an access audit logger."},
		{"zfs", h.srv.zfsProtectedGET, "ZFS inventory requires an access audit logger."},
		{"lvm", h.srv.lvmProtectedGET, "LVM inventory requires an access audit logger."},
		{"btrfs", h.srv.btrfsProtectedGET, "Btrfs inventory requires an access audit logger."},
		{"mdadm", h.srv.mdadmProtectedGET, "Software RAID inventory requires an access audit logger."},
		{"mergerfs", h.srv.mergerfsProtectedGET, "MergerFS inventory requires an access audit logger."},
	}

	for _, w := range wrappers {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://hc.test/api", nil)
		if w.call(rr, req, sess) || rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "csrf_rejected") {
			t.Fatalf("%s missing token: %d %s", w.name, rr.Code, rr.Body)
		}

		rr = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "http://hc.test/api", nil)
		req.Header.Set("Origin", "http://evil.test")
		req.Header.Set("X-CSRF-Token", sess.CSRFToken)
		h.srv.audit = nil
		if w.call(rr, req, sess) || rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "origin_rejected") {
			t.Fatalf("%s bad origin: %d %s", w.name, rr.Code, rr.Body)
		}

		rr = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "http://hc.test/api", nil)
		req.Header.Set("Origin", "null")
		req.Header.Set("X-CSRF-Token", sess.CSRFToken)
		if w.call(rr, req, sess) || rr.Code != http.StatusServiceUnavailable || !strings.Contains(rr.Body.String(), w.msg) {
			t.Fatalf("%s nil audit: %d %s", w.name, rr.Code, rr.Body)
		}

		rr = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "http://hc.test/api", nil)
		req.Header.Set("X-CSRF-Token", sess.CSRFToken)
		h.srv.audit = logger
		if !w.call(rr, req, sess) || rr.Body.Len() != 0 {
			t.Fatalf("%s pass: %d %s", w.name, rr.Code, rr.Body)
		}
	}
}
