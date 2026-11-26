// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/brokerclient"
	"homelab-control/backend/internal/ext4info"
	"homelab-control/backend/internal/xfsinfo"
)

// fsToolCase is one page whose helper report comes from a broker op.
type fsToolCase struct {
	op, path, scope, noKernel string
	report                    map[string]any
	reportWant                string
	setup                     func(h *harness, kernel bool)
}

func fsToolCases() []fsToolCase {
	return []fsToolCase{
		{op: "ext4.status", path: "/api/v1/ext4", scope: auth.ScopeExt4Read, noKernel: "no ext4 loaded",
			report: map[string]any{"filesystems": []map[string]any{{"device": "253:0", "mounts": []string{"/srv/secret-lv"}, "state": "not clean with errors",
				"error_count": 3, "features": []string{"has_journal"}}}},
			reportWant: `"error_count":3`,
			setup: func(h *harness, kernel bool) {
				h.srv.ext4Collect = func(ctx context.Context) (ext4info.Snapshot, error) {
					s, _ := ext4Fake()(ctx)
					s.KernelExt4.Present = kernel
					return s, nil
				}
			}},
		{op: "xfs.status", path: "/api/v1/xfs", scope: auth.ScopeXFSRead, noKernel: "no XFS loaded",
			report: map[string]any{"filesystems": []map[string]any{{"mountpoint": "/srv/secret-xfs", "mounts": []string{"/srv/secret-xfs"},
				"blocks": 81920, "features": []string{"crc", "reflink"}}}},
			reportWant: `"reflink"`,
			setup: func(h *harness, kernel bool) {
				h.srv.xfsCollect = func(ctx context.Context) (xfsinfo.Snapshot, error) {
					s, _ := xfsFake()(ctx)
					s.KernelXfs.Present = kernel
					return s, nil
				}
			}},
	}
}

func TestFilesystemReportFromBroker(t *testing.T) {
	for _, c := range fsToolCases() {
		fb := newFake()
		fb.results[c.op] = c.report
		h := harnessWith(t, fb, []string{c.scope})
		c.setup(h, true)
		rr := h.do("GET", c.path, "", packagesHeaders(h))
		if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"status":{"available":true`) || !strings.Contains(rr.Body.String(), c.reportWant) {
			t.Fatalf("%s: %d %s", c.op, rr.Code, rr.Body)
		}
		if a := h.audit.String(); !strings.Contains(a, "filesystems=1") || strings.Contains(a, "/srv/secret") {
			t.Fatalf("%s: audit must record counts only: %s", c.op, a)
		}
		left, ok := fb.deadlines[c.op]
		if want := brokerclient.DefaultDeadline(c.op); ok && left < want-time.Second {
			t.Fatalf("%s: hc-api gave %v, the broker client allows %v", c.op, left.Round(time.Second), want)
		}
	}
}

func TestFilesystemReportUnavailableIsAReason(t *testing.T) {
	for _, c := range fsToolCases() {
		fb := newFake()
		fb.errs[c.op] = &proto.Error{Code: proto.CodeUnavailable, Message: "tool is not installed"}
		h := harnessWith(t, fb, []string{c.scope})
		c.setup(h, true)
		rr := h.do("GET", c.path, "", packagesHeaders(h))
		if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"available":false,"reason":`) || !strings.Contains(rr.Body.String(), "is unavailable: tool is not installed") {
			t.Fatalf("%s: %d %s", c.op, rr.Code, rr.Body)
		}
	}
}

func TestFilesystemNoKernelSkipsBroker(t *testing.T) {
	for _, c := range fsToolCases() {
		fb := newFake()
		h := harnessWith(t, fb, []string{c.scope})
		c.setup(h, false)
		rr := h.do("GET", c.path, "", packagesHeaders(h))
		if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), c.noKernel) {
			t.Fatalf("%s: %d %s", c.op, rr.Code, rr.Body)
		}
		for _, op := range fb.ops {
			if op == c.op {
				t.Fatalf("%s asked although the kernel lacks the filesystem", c.op)
			}
		}
	}
}
