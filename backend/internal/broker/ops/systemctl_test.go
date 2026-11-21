// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/runner"
	. "homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/broker/writes"
	"homelab-control/backend/internal/servicepolicy"
)

type fakeSystemctl struct {
	tpl     runner.RootTemplate
	targets []string
	stdin   []byte
	calls   int
	res     runner.Result
	err     error
}

func (f *fakeSystemctl) Run(_ context.Context, t runner.RootTemplate, targets ...string) (runner.Result, error) {
	f.calls++
	f.tpl = t
	f.targets = append([]string{}, targets...)
	f.stdin = nil
	return f.res, f.err
}

func (f *fakeSystemctl) RunStdin(_ context.Context, t runner.RootTemplate, stdin []byte, targets ...string) (runner.Result, error) {
	f.calls++
	f.tpl = t
	f.targets = append([]string{}, targets...)
	f.stdin = append([]byte{}, stdin...)
	return f.res, f.err
}

func systemctlEnv(t *testing.T) (*Env, *fakeSystemctl) {
	t.Helper()
	jp := t.TempDir() + "/operations.jsonl"
	l, err := audit.Open(jp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	fw, err := writes.New(l, jp)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeSystemctl{}
	env := &Env{Writes: fw, Systemctl: fake, Actor: proto.Actor{User: "alice", Session: "sess-a", RequestID: "r1"}}
	return env, fake
}

func ctlJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func TestSystemctlPlanAndApply(t *testing.T) {
	env, fake := systemctlEnv(t)
	raw := ctlJSON(map[string]string{"action": "mask", "unit": "nginx.service"})
	got, err := systemctlPlan(context.Background(), env, raw)
	if err != nil {
		t.Fatal(err)
	}
	plan := got.(SystemctlPlan)
	if !plan.StepUpRequired || plan.Risk != RiskDisruptive || plan.Token == "" || strings.Contains(plan.Effect, "nginx.service") == false {
		t.Fatalf("plan: %+v", plan)
	}
	if _, err := systemctlApply(context.Background(), env, ctlJSON(map[string]string{"action": "mask", "unit": "nginx.service", "token": plan.Token})); code(err) != proto.CodeStepUpRequired || fake.calls != 0 {
		t.Fatalf("step-up: %v calls=%d", err, fake.calls)
	}
	// The refused apply consumed nothing: the same token still works after step-up.
	out, err := systemctlApply(context.Background(), withStepUp(env, time.Now()), ctlJSON(map[string]string{"action": "mask", "unit": "nginx.service", "token": plan.Token}))
	if err != nil {
		t.Fatal(err)
	}
	outcome := out.(SystemctlOutcome)
	if outcome.Outcome != OutcomeSucceeded || fake.calls != 1 || fake.tpl != runner.RootSystemctlMask || len(fake.targets) != 1 || fake.targets[0] != "nginx.service" {
		t.Fatalf("apply: %+v fake=%+v", outcome, fake)
	}
	if _, err := systemctlApply(context.Background(), withStepUp(env, time.Now()), ctlJSON(map[string]string{"action": "mask", "unit": "nginx.service", "token": plan.Token})); code(err) != proto.CodeInvalidToken {
		t.Fatalf("reuse: %v", err)
	}
}

func TestSystemctlRefusesProtectedAndDangerousUnits(t *testing.T) {
	env, fake := systemctlEnv(t)
	for _, raw := range []json.RawMessage{
		ctlJSON(map[string]string{"action": "mask", "unit": "ssh.service"}),
		ctlJSON(map[string]string{"action": "stop", "unit": "sshd.socket"}),
		ctlJSON(map[string]string{"action": "start", "unit": "reboot.target"}),
		ctlJSON(map[string]string{"action": "isolate", "unit": "suspend.target"}),
		ctlJSON(map[string]string{"action": "set-default", "unit": "poweroff.target"}),
		ctlJSON(map[string]string{"action": "add-wants", "unit": "nginx.service", "wants": "reboot.target"}),
	} {
		if _, err := systemctlPlan(context.Background(), env, raw); code(err) != proto.CodePolicyDenied {
			t.Errorf("%s: %v", raw, err)
		}
	}
	if fake.calls != 0 {
		t.Fatal("protected plan executed")
	}
	if servicepolicy.ValidUnit("nginx.socket") {
		t.Fatal("ValidUnit must stay .service-only")
	}
	if !servicepolicy.ValidQueryUnit("nginx.socket") || !servicepolicy.ValidQueryUnit("backup.timer") {
		t.Fatal("query units must accept socket and timer")
	}
}

func TestSystemctlSocketStartAndHostPower(t *testing.T) {
	env, fake := systemctlEnv(t)
	plan, err := systemctlPlan(context.Background(), env, ctlJSON(map[string]string{"action": "start", "unit": "nginx.socket"}))
	if err != nil {
		t.Fatal(err)
	}
	out, err := systemctlApply(context.Background(), withStepUp(env, time.Now()), ctlJSON(map[string]string{
		"action": "start", "unit": "nginx.socket", "token": plan.(SystemctlPlan).Token,
	}))
	if err != nil || out.(SystemctlOutcome).Outcome != OutcomeSucceeded || fake.tpl != runner.RootSystemctlStartUnit || fake.targets[0] != "nginx.socket" {
		t.Fatalf("socket start: %v %+v %+v", err, out, fake)
	}
	env, fake = systemctlEnv(t)
	plan, err = systemctlPlan(context.Background(), env, ctlJSON(map[string]string{"action": "reboot"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := systemctlPlan(context.Background(), env, ctlJSON(map[string]string{"action": "reboot", "unit": "nginx.service"})); code(err) != proto.CodeBadRequest {
		t.Fatalf("reboot with unit: %v", err)
	}
	out, err = systemctlApply(context.Background(), withStepUp(env, time.Now()), ctlJSON(map[string]string{"action": "reboot", "token": plan.(SystemctlPlan).Token}))
	if err != nil || fake.tpl != runner.RootSystemctlReboot || len(fake.targets) != 0 || !strings.Contains(out.(SystemctlOutcome).Message, "drop") {
		t.Fatalf("reboot: %v %+v %+v", err, out, fake)
	}
}

func TestSystemctlDropInBindsContent(t *testing.T) {
	env, fake := systemctlEnv(t)
	secret := "Environment=TOKEN=super-secret-value\n"
	body := map[string]string{"action": "dropin", "unit": "nginx.service", "name": "override.conf", "content": secret}
	got, err := systemctlPlan(context.Background(), env, ctlJSON(body))
	if err != nil {
		t.Fatal(err)
	}
	plan := got.(SystemctlPlan)
	enc, _ := json.Marshal(plan)
	if strings.Contains(string(enc), "super-secret-value") || strings.Contains(systemctlAuditArgs(ctlJSON(body)), "super-secret-value") {
		t.Fatal("drop-in content leaked into the plan or the audit summary")
	}
	body["content"] = secret + "X"
	body["token"] = plan.Token
	if _, err := systemctlApply(context.Background(), withStepUp(env, time.Now()), ctlJSON(body)); code(err) != proto.CodeConflict || fake.calls != 0 {
		t.Fatalf("changed content: %v calls=%d", err, fake.calls)
	}
	env, fake = systemctlEnv(t)
	body["content"] = secret
	delete(body, "token")
	got, err = systemctlPlan(context.Background(), env, ctlJSON(body))
	if err != nil {
		t.Fatal(err)
	}
	body["token"] = got.(SystemctlPlan).Token
	out, err := systemctlApply(context.Background(), withStepUp(env, time.Now()), ctlJSON(body))
	if err != nil || fake.tpl != runner.RootSystemctlEdit || string(fake.stdin) != secret || fake.targets[0] != "override.conf" || fake.targets[1] != "nginx.service" {
		t.Fatalf("drop-in: %v %+v %+v", err, out, fake)
	}
	if _, err := systemctlPlan(context.Background(), env, ctlJSON(map[string]string{"action": "dropin", "unit": "nginx.service", "name": "../x.conf", "content": "a"})); code(err) != proto.CodeBadRequest {
		t.Fatalf("bad name: %v", err)
	}
	if _, err := systemctlPlan(context.Background(), env, json.RawMessage(`{"path":"/etc/shadow"}`)); code(err) != proto.CodeBadRequest {
		t.Fatalf("path: %v", err)
	}
}

func TestSystemctlApplyWithoutRootDoesNotRun(t *testing.T) {
	env, _ := systemctlEnv(t)
	env.Systemctl = nil
	env.RootExec = nil
	plan, err := systemctlPlan(context.Background(), env, ctlJSON(map[string]string{"action": "daemon-reload"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := systemctlApply(context.Background(), withStepUp(env, time.Now()), ctlJSON(map[string]string{"action": "daemon-reload", "token": plan.(SystemctlPlan).Token})); code(err) != proto.CodeForbidden {
		t.Fatalf("no root: %v", err)
	}
}

func TestSystemctlTemplatesMatchTheVerbTable(t *testing.T) {
	seen := map[runner.RootTemplate]bool{}
	for _, spec := range ctlSpecs {
		seen[spec.tpl] = true
	}
	if len(seen) != len(systemctlTemplates) {
		t.Fatalf("verb table %d templates, registry %d", len(seen), len(systemctlTemplates))
	}
	for _, tpl := range systemctlTemplates {
		if !seen[tpl] {
			t.Errorf("registry template %s has no verb", tpl)
		}
	}
}
