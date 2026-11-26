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
	. "homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/broker/writes"
	"homelab-control/backend/internal/mergerfsapply"
)

func mergerfsEnv(t *testing.T) *Env {
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
	return &Env{Writes: fw, Actor: proto.Actor{User: "alice", Session: "sess-a", RequestID: "r1"}}
}

func TestMergerFSPlanAndApplySyncBlock(t *testing.T) {
	env := mergerfsEnv(t)
	calls := 0
	env.MergerFSApply = func(context.Context, mergerfsapply.Request) (mergerfsapply.Result, error) {
		calls++
		return mergerfsapply.Result{Version: 1, OK: false, Blocked: mergerfsapply.BlockMissingDisk, Summary: "operation refused before it started"}, nil
	}
	raw := json.RawMessage(`{"action":"sync"}`)
	got, err := mergerfsPlan(mergerfsKeepPlan)(context.Background(), env, raw)
	if err != nil {
		t.Fatal(err)
	}
	plan := got.(MergerFSPlan)
	if !plan.StepUpRequired || strings.Join(plan.Argv, " ") != "/usr/bin/snapraid sync" || strings.Contains(plan.Effect, "/") {
		t.Fatalf("%+v", plan)
	}
	applyRaw := json.RawMessage(`{"action":"sync","token":"` + plan.Token + `"}`)
	if _, err := mergerfsApply(mergerfsMaintainOp)(context.Background(), env, applyRaw); code(err) != proto.CodeStepUpRequired || calls != 0 {
		t.Fatalf("step-up: %v calls=%d", err, calls)
	}
	env.Actor.StepUpAt = time.Now().Unix()
	out, err := mergerfsApply(mergerfsMaintainOp)(context.Background(), env, applyRaw)
	if err != nil || calls != 1 {
		t.Fatalf("apply: %v calls=%d", err, calls)
	}
	gotOut := out.(MergerFSOutcome)
	if gotOut.Outcome != OutcomeFailed || !strings.Contains(gotOut.Message, "missing") || strings.Contains(gotOut.Message, "/") {
		t.Fatalf("%+v", gotOut)
	}
	if _, err := mergerfsApply(mergerfsMaintainOp)(context.Background(), env, applyRaw); code(err) != proto.CodeInvalidToken {
		t.Fatalf("replay: %v", err)
	}
}

func TestMergerFSMaintainRejectsMount(t *testing.T) {
	env := mergerfsEnv(t)
	raw := json.RawMessage(`{"action":"mount","mountpoint":"/mnt/pool","branches":["/mnt/disk1","/mnt/disk2"],"policy":"epmfs"}`)
	if _, err := mergerfsPlan(mergerfsKeepPlan)(context.Background(), env, raw); code(err) != proto.CodeBadRequest {
		t.Fatalf("maintain accepted mount: %v", err)
	}
}

func TestMergerFSFixPreviewHasNoUndeleteFlag(t *testing.T) {
	env := mergerfsEnv(t)
	raw := json.RawMessage(`{"action":"fix","files":["movies/a.mkv"]}`)
	got, err := mergerfsPlan(mergerfsKeepPlan)(context.Background(), env, raw)
	if err != nil {
		t.Fatal(err)
	}
	plan := got.(MergerFSPlan)
	joined := strings.Join(plan.Argv, " ")
	if joined != "/usr/bin/snapraid fix -m -f /movies/a.mkv" || strings.Contains(joined, " -e") {
		t.Fatalf("argv %s", joined)
	}
	if mergerfsAuditArgs(raw) != "action=fix files=1" {
		t.Fatalf("audit %s", mergerfsAuditArgs(raw))
	}
}

// status is the helper's read action; the write ops must not accept it.
func TestMergerFSWriteOpsRefuseStatus(t *testing.T) {
	for _, op := range []string{mergerfsPoolPlan, mergerfsPoolOp, mergerfsKeepPlan, mergerfsMaintainOp} {
		if err := mergerfsFamily(op, mergerfsapply.Spec{Action: mergerfsapply.ActionStatus}); err == nil {
			t.Fatalf("%s accepted status", op)
		}
	}
}

// A sync that is still running when the helper replies is reported as
// started, with no exit status, and says where its end is recorded.
func TestMergerFSApplyReportsARunningSync(t *testing.T) {
	env := mergerfsEnv(t)
	env.MergerFSApply = func(context.Context, mergerfsapply.Request) (mergerfsapply.Result, error) {
		return mergerfsapply.Result{Version: 1, OK: true, Running: true, Summary: "sync started and continues in the background"}, nil
	}
	got, err := mergerfsPlan(mergerfsKeepPlan)(context.Background(), env, json.RawMessage(`{"action":"sync"}`))
	if err != nil {
		t.Fatal(err)
	}
	env.Actor.StepUpAt = time.Now().Unix()
	out, err := mergerfsApply(mergerfsMaintainOp)(context.Background(), env,
		json.RawMessage(`{"action":"sync","token":"`+got.(MergerFSPlan).Token+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	o := out.(MergerFSOutcome)
	if o.Outcome != OutcomeSucceeded || o.ExitCode != nil || !strings.HasPrefix(o.Message, "Started. Sync continues in the background.") || !strings.Contains(o.Message, "journal") {
		t.Fatalf("%+v", o)
	}
}

func TestMergerFSApplyNamesTheHeldLock(t *testing.T) {
	if m := blockedMessage(mergerfsapply.BlockLocked); !strings.Contains(m, "lock") || !strings.Contains(m, "Nothing was started") {
		t.Fatal(m)
	}
}
