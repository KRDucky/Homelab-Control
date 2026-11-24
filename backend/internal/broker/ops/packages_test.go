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
	// Only SUSE executes a tool for sources; the other families read files.
	if q, ok := familyQuery("suse", "sources"); !ok || q != runner.PkgSUSESources {
		t.Errorf("suse sources missing or wrong: %v %v", q, ok)
	}
	for _, family := range []string{"debian", "fedora", "arch"} {
		if _, ok := familyQuery(family, "sources"); ok {
			t.Errorf("%s sources unexpectedly has a package query", family)
		}
	}
	if _, ok := familyQuery("unknown", "installed"); ok {
		t.Error("unknown family has a package query")
	}
}

// packageBinary must never fall through to pacman for an empty or unknown
// query (the SUSE sources bug): unknown kinds fail closed, not with a wrong
// binary check.
func TestPackageBinaryNeverFallsThrough(t *testing.T) {
	if b := packageBinary(""); b != "" {
		t.Errorf("empty query fell through to binary %q", b)
	}
	if b := packageBinary(runner.PkgSUSESources); b != "zypper" {
		t.Errorf("suse sources binary = %q, want zypper", b)
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

func TestPackagesSimulationRejectsInvalidRequests(t *testing.T) {
	env := &Env{Roots: Roots{Root: t.TempDir()}}
	for _, raw := range []string{
		`{}`,
		`{"family":"fedora","action":"install","package":"bash"}`,
		`{"family":"debian","action":"update","package":"bash"}`,
		`{"family":"debian","action":"install","package":"--help"}`,
		`{"family":"debian","action":"install","package":"bash","flags":["--force-yes"]}`,
	} {
		if _, err := packagesSimulation(context.Background(), env, json.RawMessage(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestPackagesSimulationRequiresCache(t *testing.T) {
	_, err := packagesSimulation(context.Background(), &Env{Roots: Roots{Root: t.TempDir()}},
		json.RawMessage(`{"family":"debian","action":"install","package":"bash"}`))
	var pe *proto.Error
	if !errors.As(err, &pe) || pe.Code != proto.CodeUnavailable {
		t.Fatalf("cache missing: %v", err)
	}
}

func TestPackagesSimulationAuditRedactsTarget(t *testing.T) {
	got := packagesSimulationAuditArgs(json.RawMessage(`{"family":"debian","action":"remove","package":"private-package"}`))
	if got != "family=debian action=remove" {
		t.Fatalf("unsafe audit: %q", got)
	}
}

func TestAptSimulationPreviewFailsClosed(t *testing.T) {
	// Fixture: stdout of a real non-root "apt-get --simulate
	// --no-install-recommends install -- sl" (apt 3.3.3, Ubuntu), test data only.
	ok := `NOTE: This is only a simulation!
      apt-get needs root privileges for real execution.
      Keep also in mind that locking is deactivated,
      so don't depend on the relevance to the real current situation!
Reading package lists...
Building dependency tree...
Reading state information...
Solving dependencies...
The following NEW packages will be installed:
  sl
0 upgraded, 1 newly installed, 0 to remove and 8 not upgraded.
Inst sl (5.02-1build1 Ubuntu:26.10/stonking [amd64])
Conf sl (5.02-1build1 Ubuntu:26.10/stonking [amd64])
`
	p, err := aptSimulationPreview(runner.Result{Stdout: []byte(ok)})
	if err != nil || p.InstallCount != 1 || p.ConfigureCount != 1 || p.Changes[0].Name != "sl" || p.Changes[0].CandidateVersion != "5.02-1build1" {
		t.Fatalf("clean run: %+v %v", p, err)
	}
	for name, res := range map[string]runner.Result{
		"exit":   {Stdout: []byte(ok), ExitCode: 100},
		"stderr": {Stdout: []byte(ok), Stderr: "W: something\n"},
		"parse":  {Stdout: []byte("Inst sl (5.02-1build1 Ubuntu:26.10/stonking [amd64])\n")},
	} {
		var pe *proto.Error
		if _, err := aptSimulationPreview(res); !errors.As(err, &pe) || pe.Code != proto.CodeUnavailable {
			t.Errorf("%s: want unavailable, got %v", name, err)
		}
	}
}
