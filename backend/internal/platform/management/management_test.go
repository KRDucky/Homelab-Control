// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package management

import (
	"errors"
	"strings"
	"testing"
	"time"

	"homelab-control/backend/internal/composediscovery"
	"homelab-control/backend/internal/platform"
	"homelab-control/backend/internal/platform/composeadapter"
	"homelab-control/backend/internal/platform/kubernetes"
	"homelab-control/backend/internal/platform/podman"
)

func TestManagementIsSeparateAndDisabled(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	familyOf = func(id string) (string, bool) { return "docker", strings.HasPrefix(id, "docker") }
	defer func() { familyOf = func(string) (string, bool) { return "", false } }()
	op := Operation{Platform: "docker-local", Kind: KindRestart, Target: "docker-local/container/-/web/abc"}
	if op.Validate() != nil {
		t.Fatal("valid op rejected")
	}
	for _, bad := range []Operation{{Platform: "Docker", Kind: KindRestart, Target: "x"}, {Platform: "docker-local", Kind: "exec", Target: "x"}, {Platform: "docker-local", Kind: KindDeploy}} {
		if bad.Validate() == nil {
			t.Fatal("accepted", bad)
		}
	}
	plan := Plan{ID: "p1", Operation: op, Fingerprint: "sha256:abc", Expires: now.Add(time.Minute), RequiresAck: []string{"root-equivalent-daemon"}}
	discovery := func(s string) bool {
		return s == "docker.compose.read" || s == "platforms.read" || s == "networking.proxy.read"
	}
	manage := func(s string) bool { return s == Scope("docker", KindRestart) }
	good := Approval{PlanID: "p1", Fingerprint: "sha256:abc", Scope: Scope("docker", KindRestart), Acknowledge: []string{"root-equivalent-daemon"}}
	// Discovery scopes never grant management.
	if CheckApproval(plan, good, discovery, now) == nil {
		t.Fatal("discovery scope granted management")
	}
	for name, a := range map[string]Approval{
		"other plan":        {PlanID: "p2", Fingerprint: "sha256:abc", Scope: good.Scope, Acknowledge: good.Acknowledge},
		"changed plan":      {PlanID: "p1", Fingerprint: "sha256:zzz", Scope: good.Scope, Acknowledge: good.Acknowledge},
		"wrong scope":       {PlanID: "p1", Fingerprint: "sha256:abc", Scope: Scope("docker", KindDeploy), Acknowledge: good.Acknowledge},
		"missing ack":       {PlanID: "p1", Fingerprint: "sha256:abc", Scope: good.Scope},
		"empty fingerprint": {PlanID: "p1", Scope: good.Scope, Acknowledge: good.Acknowledge},
	} {
		if CheckApproval(plan, a, manage, now) == nil {
			t.Fatal("accepted", name)
		}
	}
	if CheckApproval(plan, good, manage, now.Add(time.Hour)) == nil {
		t.Fatal("expired plan accepted")
	}
	if err := CheckApproval(plan, good, manage, now); err != nil {
		t.Fatal(err)
	}
	// Even a fully valid approval cannot execute in Phase 9.
	var ex Executor = Disabled{}
	if !errors.Is(ex.Execute(plan, good), ErrDisabled) {
		t.Fatal("executor must be disabled")
	}
	// Unknown platform family never matches any scope.
	familyOf = func(string) (string, bool) { return "", false }
	if CheckApproval(plan, good, func(string) bool { return true }, now) == nil {
		t.Fatal("unknown family matched")
	}
}

// The real discovery adapters must not implement the management Executor.
func TestDiscoveryAdaptersAreNotExecutors(t *testing.T) {
	docker, err := composeadapter.New(composediscovery.Fixture{}, composeadapter.Options{Kind: platform.KindDocker, Distribution: platform.DistDockerEngine, Mode: platform.ModeRootless, Auth: platform.AuthFixture, Transport: "fixture", EndpointLabel: "x"})
	if err != nil {
		t.Fatal(err)
	}
	pod, err := composeadapter.New(podman.Fixture{}, composeadapter.Options{Kind: platform.KindPodman, Distribution: platform.DistPodman, Mode: platform.ModeRootful, Auth: platform.AuthFixture, Transport: "fixture", EndpointLabel: "x"})
	if err != nil {
		t.Fatal(err)
	}
	k3s, err := kubernetes.NewFixture()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []platform.Adapter{docker, pod, k3s} {
		if _, ok := a.(Executor); ok {
			t.Fatalf("%T implements Executor", a)
		}
	}
}
