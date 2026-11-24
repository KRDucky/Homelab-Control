// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"homelab-control/backend/internal/aptapply"
	"homelab-control/backend/internal/broker/proto"
	. "homelab-control/backend/internal/broker/types"
)

// Fixture: shaped like Ubuntu 24.04's default ubuntu.sources (test data only).
const aptTestSources = `Types: deb
URIs: http://us.archive.ubuntu.com/ubuntu/
Suites: noble noble-updates noble-backports
Components: main restricted universe multiverse
Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg

Types: deb
URIs: http://security.ubuntu.com/ubuntu/
Suites: noble-security
Components: main restricted universe multiverse
Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg
`

type aptFake struct {
	root  string
	dump  string
	calls []aptapply.Request
	fail  error
}

func aptEnv(t *testing.T) (*Env, *aptFake) {
	t.Helper()
	env := mergerfsEnv(t)
	f := &aptFake{root: t.TempDir(), dump: "APT::Install-Recommends \"1\";\n"}
	// os-release is a symlink on Debian and Ubuntu.
	if err := os.MkdirAll(f.root+"/usr/lib", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.root+"/usr/lib/os-release", []byte("ID=ubuntu\nVERSION_CODENAME=noble\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(f.root+"/etc", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../usr/lib/os-release", f.root+aptapply.OSRelease); err != nil {
		t.Fatal(err)
	}
	for p, c := range map[string]string{aptapply.UbuntuSources: aptTestSources} {
		if err := os.MkdirAll(filepath.Dir(f.root+p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f.root+p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(f.root+"/etc/apt/apt.conf.d", 0o755); err != nil {
		t.Fatal(err)
	}
	env.Roots = Roots{Root: f.root}
	env.AptConfigDump = func(context.Context) (string, error) { return f.dump, nil }
	// The fake helper applies the request the way hc-apt-apply does, so the
	// broker's read-back sees the change.
	env.AptApply = func(_ context.Context, req aptapply.Request) (aptapply.Result, error) {
		f.calls = append(f.calls, req)
		if f.fail != nil {
			return aptapply.Result{}, f.fail
		}
		target := f.root + req.Target()
		switch req.Action {
		case aptapply.ActionConfWrite:
			s, _ := aptapply.Render(req.Tunables)
			_ = os.WriteFile(target, []byte(s), 0o644)
		case aptapply.ActionConfRemove:
			_ = os.Remove(target)
		case aptapply.ActionMirrorSet:
			b, _ := os.ReadFile(target)
			s, _, err := aptapply.SetMirror(req.File, string(b), aptapply.Distro{ID: "ubuntu", Codename: "noble"}, req.From, req.To)
			if err != nil {
				return aptapply.Result{Version: 1, OK: false, Message: "refused"}, nil
			}
			_ = os.WriteFile(target, []byte(s), 0o644)
		}
		return aptapply.Result{Version: 1, OK: true, Message: "Written.", Backup: "x.bak"}, nil
	}
	return env, f
}

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func TestAptConfigPlanApplyAndOverride(t *testing.T) {
	env, f := aptEnv(t)
	off := false
	three := 3
	tun := aptapply.Tunables{InstallRecommends: &off, Retries: &three}
	got, err := aptConfigPlan(context.Background(), env, raw(map[string]any{"tunables": tun}))
	if err != nil {
		t.Fatal(err)
	}
	p := got.(aptapply.AptPlan)
	if p.Risk != RiskReversible || p.StepUpRequired || !p.ConfirmationRequired || p.Before != "" ||
		!strings.Contains(p.After, `APT::Install-Recommends "false";`) || len(f.calls) != 0 {
		t.Fatalf("plan %+v calls=%d", p, len(f.calls))
	}
	// apt-config still reports recommends on: a later file overrides ours.
	out, err := aptConfigApply(context.Background(), env, raw(map[string]any{"tunables": tun, "token": p.Token}))
	if err != nil {
		t.Fatal(err)
	}
	o := out.(aptapply.AptOutcome)
	if o.Outcome != OutcomeSucceeded || len(f.calls) != 1 || f.calls[0].Digest != "" ||
		strings.Join(o.Overrides, ",") != "install_recommends,retries" {
		t.Fatalf("outcome %+v calls=%+v", o, f.calls)
	}
	if _, err := aptConfigApply(context.Background(), env, raw(map[string]any{"tunables": tun, "token": p.Token})); code(err) != proto.CodeInvalidToken {
		t.Fatalf("replay: %v", err)
	}
	// The same settings again: nothing to do.
	if _, err := aptConfigPlan(context.Background(), env, raw(map[string]any{"tunables": tun})); code(err) != proto.CodeConflict {
		t.Fatalf("no-op plan: %v", err)
	}
	// Read view marks the managed values.
	v, err := aptConfigRead(context.Background(), env, raw(struct{}{}))
	if err != nil {
		t.Fatal(err)
	}
	view := v.(aptapply.AptConfigView)
	if !view.ManagedPresent || view.ManagedByHand || view.Managed.Retries == nil || *view.Managed.Retries != 3 || view.Effective == nil {
		t.Fatalf("view %+v", view)
	}
}

func TestAptConfigApplyRefusesChangedFile(t *testing.T) {
	env, f := aptEnv(t)
	two := 2
	tun := aptapply.Tunables{Retries: &two}
	got, err := aptConfigPlan(context.Background(), env, raw(map[string]any{"tunables": tun}))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.root+aptapply.ConfPath, []byte("// hand edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := aptConfigApply(context.Background(), env, raw(map[string]any{"tunables": tun, "token": got.(aptapply.AptPlan).Token})); code(err) != proto.CodeConflict {
		t.Fatalf("changed file: %v", err)
	}
	// Different settings than reviewed are refused too.
	got, _ = aptConfigPlan(context.Background(), env, raw(map[string]any{"tunables": tun}))
	five := 5
	if _, err := aptConfigApply(context.Background(), env, raw(map[string]any{"tunables": aptapply.Tunables{Retries: &five}, "token": got.(aptapply.AptPlan).Token})); code(err) != proto.CodeConflict {
		t.Fatalf("changed request: %v", err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("helper called %d times", len(f.calls))
	}
	if _, err := aptConfigPlan(context.Background(), env, raw(map[string]any{"reset": true, "tunables": tun})); code(err) != proto.CodeBadRequest {
		t.Fatalf("reset with settings: %v", err)
	}
	for _, bad := range []string{`{"tunables":{"retries":99}}`, `{"tunables":{"http_proxy":"http://u:p@h:1"}}`, `{"tunables":{},"shell":"x"}`, `{"tunables":{},"token":"x"}`} {
		if _, err := aptConfigPlan(context.Background(), env, json.RawMessage(bad)); code(err) != proto.CodeBadRequest {
			t.Errorf("%s: %v", bad, err)
		}
	}
}

func TestAptConfigResetAndHelperDown(t *testing.T) {
	env, f := aptEnv(t)
	if _, err := aptConfigPlan(context.Background(), env, raw(map[string]any{"reset": true})); code(err) != proto.CodeConflict {
		t.Fatalf("reset without a managed file: %v", err)
	}
	if err := os.WriteFile(f.root+aptapply.ConfPath, []byte("// x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := aptConfigPlan(context.Background(), env, raw(map[string]any{"reset": true}))
	if err != nil {
		t.Fatal(err)
	}
	f.fail = errors.New("connection refused")
	out, err := aptConfigApply(context.Background(), env, raw(map[string]any{"reset": true, "token": got.(aptapply.AptPlan).Token}))
	if err != nil {
		t.Fatal(err)
	}
	if o := out.(aptapply.AptOutcome); o.Outcome != OutcomeUncertain || !strings.Contains(o.Message, "opt-in") {
		t.Fatalf("%+v", o)
	}
	if f.calls[0].Action != aptapply.ActionConfRemove || f.calls[0].Digest != aptapply.Digest([]byte("// x\n")) {
		t.Fatalf("request %+v", f.calls[0])
	}
}

func TestAptMirrorInventoryPlanApply(t *testing.T) {
	env, f := aptEnv(t)
	v, err := aptMirrorInventory(context.Background(), env, raw(struct{}{}))
	if err != nil {
		t.Fatal(err)
	}
	inv := v.(aptapply.MirrorInventory)
	if len(inv.Files) != 1 || len(inv.Entries) != 2 || !inv.Entries[0].Switchable || inv.Entries[1].Role != aptapply.RoleSecurity {
		t.Fatalf("%+v", inv)
	}
	args := map[string]any{"file": aptapply.UbuntuSources, "from": "http://us.archive.ubuntu.com/ubuntu/", "to": "http://mirror.example.edu/ubuntu/"}
	got, err := aptMirrorPlan(context.Background(), env, raw(args))
	if err != nil {
		t.Fatal(err)
	}
	p := got.(aptapply.AptPlan)
	if len(p.Lines) != 1 || p.Lines[0].Line != 2 || p.Lines[0].After != "URIs: http://mirror.example.edu/ubuntu/" || p.StepUpRequired {
		t.Fatalf("plan %+v", p)
	}
	args["token"] = p.Token
	out, err := aptMirrorApply(context.Background(), env, raw(args))
	if err != nil {
		t.Fatal(err)
	}
	if o := out.(aptapply.AptOutcome); o.Outcome != OutcomeSucceeded || len(f.calls) != 1 || f.calls[0].Digest != aptapply.Digest([]byte(aptTestSources)) {
		t.Fatalf("%+v %+v", o, f.calls)
	}
	// Security entries and other files are refused at plan time.
	for _, bad := range []map[string]any{
		{"file": aptapply.UbuntuSources, "from": "http://security.ubuntu.com/ubuntu/", "to": "http://mirror.example.edu/ubuntu/"},
		{"file": "/etc/apt/sources.list.d/docker.sources", "from": "http://a.example/", "to": "http://b.example/"},
		{"file": aptapply.UbuntuSources, "from": "http://mirror.example.edu/ubuntu/", "to": "http://u:p@m.example/ubuntu/"},
	} {
		if _, err := aptMirrorPlan(context.Background(), env, raw(bad)); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
	if got := aptMirrorAuditArgs(raw(args)); got != "file=ubuntu.sources to=mirror.example.edu" {
		t.Fatalf("audit %q", got)
	}
	if got := aptConfigAuditArgs(raw(map[string]any{"tunables": map[string]any{"http_proxy": "http://cache.lan:3142"}})); strings.Contains(got, "cache.lan") {
		t.Fatalf("audit leaked the proxy: %q", got)
	}
}

func TestAptOpsRefuseOtherDistributions(t *testing.T) {
	env, f := aptEnv(t)
	if err := os.WriteFile(f.root+"/usr/lib/os-release", []byte("ID=fedora\nVERSION_ID=42\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := aptMirrorInventory(context.Background(), env, raw(struct{}{})); code(err) != proto.CodeUnavailable {
		t.Fatalf("inventory on fedora: %v", err)
	}
	if _, err := aptConfigRead(context.Background(), env, raw(struct{}{})); code(err) != proto.CodeUnavailable {
		t.Fatalf("read on fedora: %v", err)
	}
}
