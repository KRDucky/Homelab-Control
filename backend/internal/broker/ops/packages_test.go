// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/runner"
)

func TestDecodePackagesArgs(t *testing.T) {
	for _, bad := range []string{
		`{}`,
		`{"family":""}`,
		`{"family":"unknown"}`,
		`{"family":"gentoo"}`,
		`{"family":"debian","q":""}`,
		`{"family":"debian","q":"-o"}`,
		`{"family":"debian","q":"--config"}`,
		`{"family":"debian","q":"a;b"}`,
		`{"family":"debian","q":"$(id)"}`,
		`{"family":"debian","q":"` + "\u0000" + `"}`,
		`{"family":"debian","q":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`,
	} {
		if _, err := decodePackagesArgs(json.RawMessage(bad), true); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	// Query rejected on argument-free ops.
	if _, err := decodePackagesArgs(json.RawMessage(`{"family":"debian","q":"nginx"}`), false); err == nil {
		t.Error("argument-free op accepted a query")
	}
	// Valid query accepted.
	if a, err := decodePackagesArgs(json.RawMessage(`{"family":"debian","q":"nginx"}`), true); err != nil || a.Q != "nginx" || a.Family != "debian" {
		t.Errorf("%+v %v", a, err)
	}
}

func TestFamilyQueryClosed(t *testing.T) {
	for _, family := range []string{"debian", "fedora", "suse", "arch"} {
		for _, kind := range []string{"installed", "updates", "search"} {
			if _, ok := familyQuery(family, kind); !ok {
				t.Errorf("%s %s has no package query", family, kind)
			}
		}
	}
	if _, ok := familyQuery("unknown", "installed"); ok {
		t.Error("unknown family has a package query")
	}
}

func TestPackagesUnsupportedFamily(t *testing.T) {
	_, err := packagesInstalled(context.Background(), &Env{Roots: Roots{Root: t.TempDir()}}, json.RawMessage(`{"family":"unknown"}`))
	var pe *proto.Error
	if !errors.As(err, &pe) || pe.Code != proto.CodeUnavailable {
		t.Fatalf("unknown family: %v", err)
	}
}

func TestPackagesMissingTool(t *testing.T) {
	// nil Runner means no package-manager binary is available.
	_, err := packagesInstalled(context.Background(), &Env{Roots: Roots{Root: t.TempDir()}}, json.RawMessage(`{"family":"debian"}`))
	var pe *proto.Error
	if !errors.As(err, &pe) || pe.Code != proto.CodeUnavailable {
		t.Fatalf("missing tool must be unavailable, got %v", err)
	}
}

func TestPackagesUpdatesRequiresCache(t *testing.T) {
	// No cache directory in the fixture roots: updates must fail closed.
	_, err := packagesUpdates(context.Background(), &Env{Runner: new(runner.Runner), Roots: Roots{Root: t.TempDir()}}, json.RawMessage(`{"family":"debian"}`))
	var pe *proto.Error
	if !errors.As(err, &pe) || pe.Code != proto.CodeUnavailable {
		t.Fatalf("missing cache must be unavailable, got %v", err)
	}
}

func TestPackagesAuditArgsNeverLeaksQuery(t *testing.T) {
	got := packagesAuditArgs(json.RawMessage(`{"family":"debian","q":"super-secret-query"}`))
	if got != "family=debian q_len=18" {
		t.Fatalf("audit args leaked query: %q", got)
	}
}
