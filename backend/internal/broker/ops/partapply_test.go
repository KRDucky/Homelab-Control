// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/runner"
	. "homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/broker/writes"
	"homelab-control/backend/internal/partplan"
)

func newPartEnv(t *testing.T) *Env {
	t.Helper()
	jp := filepath.Join(t.TempDir(), "operations.jsonl")
	l, err := audit.Open(jp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	fw, err := writes.New(l, jp)
	if err != nil {
		t.Fatal(err)
	}
	return &Env{
		Writes:       fw,
		PartDisk:     &fakeExt4View{fact: ext4ReadyFact()},
		PartTooling:  &partToolFact{Family: "debian", Installed: true},
		PartGeometry: func(string) (uint64, uint64, error) { return 512, 8 << 20, nil },
		Actor:        proto.Actor{User: "alice", Session: "sess-a", RequestID: "r1", StepUpAt: time.Now().Unix()},
		PartSfdisk: func(context.Context, []string, []byte) (runner.Result, error) {
			return runner.Result{ExitCode: 0}, nil
		},
		PartReadback: func(context.Context, []string) (runner.Result, error) {
			return runner.Result{ExitCode: 0, Stdout: sfdiskJSON(t, partSlice())}, nil
		},
	}
}

// sfdiskJSON is `sfdisk --json` output for the table GPTScript writes for parts.
func sfdiskJSON(t *testing.T, parts []partplan.Slice) []byte {
	t.Helper()
	rows, err := partplan.GPTRows(partplan.Request{DiskBytes: 8 << 20, Parts: parts}, 512)
	if err != nil {
		t.Fatal(err)
	}
	type entry struct {
		Start uint64 `json:"start"`
		Size  uint64 `json:"size"`
		Type  string `json:"type"`
	}
	tbl := struct {
		Label      string  `json:"label"`
		Partitions []entry `json:"partitions"`
	}{Label: "gpt"}
	for _, r := range rows {
		tbl.Partitions = append(tbl.Partitions, entry{r.Start, r.Size, r.Type})
	}
	b, _ := json.Marshal(map[string]any{"partitiontable": tbl})
	return b
}

// partApplyOnce plans and applies partSlice() on env and returns the outcome.
func partApplyOnce(t *testing.T, env *Env) PartitionWriteOutcome {
	t.Helper()
	plan, err := partApplyPlan(context.Background(), env, partRaw(partSlice()))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(partApplyApplyArgs{
		ByID: ext4TestByID, IdentityKind: "wwn", IdentityValue: ext4TestWWN,
		Confirm: []string{"partition"}, DiskBytes: 8 << 20, Parts: partSlice(), Token: plan.(PartitionWritePlan).Token,
	})
	got, err := partApplyApply(context.Background(), env, raw)
	if err != nil {
		t.Fatal(err)
	}
	return got.(PartitionWriteOutcome)
}

func TestPartitionReadBackConfirmsTheTable(t *testing.T) {
	env := newPartEnv(t)
	var gotArgv []string
	env.PartReadback = func(_ context.Context, argv []string) (runner.Result, error) {
		gotArgv = append([]string{}, argv...)
		return runner.Result{ExitCode: 0, Stdout: sfdiskJSON(t, partSlice())}, nil
	}
	out := partApplyOnce(t, env)
	if out.Outcome != OutcomeSucceeded || !strings.Contains(out.Message, "reads back the planned table (1 partitions)") {
		t.Fatalf("%+v", out)
	}
	if strings.Join(gotArgv, " ") != "--json -- "+ext4TestByID {
		t.Fatalf("read-back argv %v", gotArgv)
	}
}

func TestPartitionReadBackMismatchOrFailureIsUncertain(t *testing.T) {
	other := []partplan.Slice{{StartBytes: 1 << 20, SizeBytes: 1 << 20, FSType: "ext4"}}
	for name, rb := range map[string]func(context.Context, []string) (runner.Result, error){
		"different table": func(context.Context, []string) (runner.Result, error) {
			return runner.Result{ExitCode: 0, Stdout: sfdiskJSON(t, other)}, nil
		},
		"non-zero": func(context.Context, []string) (runner.Result, error) { return runner.Result{ExitCode: 1}, nil },
		"no run":   func(context.Context, []string) (runner.Result, error) { return runner.Result{}, runner.ErrTimeout },
	} {
		env := newPartEnv(t)
		env.PartReadback = rb
		out := partApplyOnce(t, env)
		if out.Outcome != OutcomeUncertain || !strings.Contains(out.Message, "could not be confirmed") || len(out.Recovery) == 0 {
			t.Errorf("%s: %+v", name, out)
		}
	}
}

func partRaw(parts []partplan.Slice) json.RawMessage {
	b, _ := json.Marshal(partApplyArgs{
		ByID: ext4TestByID, IdentityKind: "wwn", IdentityValue: ext4TestWWN,
		Confirm: []string{"partition"}, DiskBytes: 8 << 20, Parts: parts,
	})
	return b
}

func partSlice() []partplan.Slice {
	return []partplan.Slice{{StartBytes: 1 << 20, SizeBytes: 2 << 20, FSType: "ext4"}}
}

func TestPartitionPlanAndApplyUsesClosedScript(t *testing.T) {
	env := newPartEnv(t)
	var gotArgv []string
	var gotStdin string
	env.PartSfdisk = func(_ context.Context, argv []string, stdin []byte) (runner.Result, error) {
		gotArgv = append([]string{}, argv...)
		gotStdin = string(stdin)
		if _, err := runner.RootArgv(runner.RootSfdiskGPT, argv[len(argv)-1]); err != nil {
			t.Errorf("argv: %v", err)
		}
		return runner.Result{ExitCode: 0}, nil
	}
	plan, err := partApplyPlan(context.Background(), env, partRaw(partSlice()))
	if err != nil {
		t.Fatal(err)
	}
	p := plan.(PartitionWritePlan)
	raw, _ := json.Marshal(partApplyApplyArgs{
		ByID: ext4TestByID, IdentityKind: "wwn", IdentityValue: ext4TestWWN,
		Confirm: []string{"partition"}, DiskBytes: 8 << 20, Parts: partSlice(), Token: p.Token,
	})
	got, err := partApplyApply(context.Background(), env, raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.(PartitionWriteOutcome).Outcome != OutcomeSucceeded {
		t.Fatalf("%+v", got)
	}
	if strings.Join(gotArgv, " ") != "--wipe=always --wipe-partitions=always -- "+ext4TestByID {
		t.Fatalf("argv %v", gotArgv)
	}
	want, err := partplan.GPTScript(partplan.Request{DiskBytes: 8 << 20, Parts: partSlice()}, 512)
	if err != nil || gotStdin != want {
		t.Fatalf("stdin %q err %v", gotStdin, err)
	}
	if strings.Contains(gotStdin, "mkfs") {
		t.Fatal(gotStdin)
	}
}

func TestPartitionAliasLockBlocksSecondSfdisk(t *testing.T) {
	env := newPartEnv(t)
	wwnPlan, err := partApplyPlan(context.Background(), env, partRaw(partSlice()))
	if err != nil {
		t.Fatal(err)
	}
	serialRaw, _ := json.Marshal(partApplyArgs{
		ByID: "/dev/disk/by-id/virtio-mmvtestdisk01", IdentityKind: "serial", IdentityValue: "mmvtestdisk01",
		Confirm: []string{"partition"}, DiskBytes: 8 << 20, Parts: partSlice(),
	})
	serialPlan, err := partApplyPlan(context.Background(), env, serialRaw)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	env.PartSfdisk = func(context.Context, []string, []byte) (runner.Result, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return runner.Result{ExitCode: 0}, nil
	}
	done := make(chan error, 1)
	go func() {
		raw, _ := json.Marshal(partApplyApplyArgs{
			ByID: ext4TestByID, IdentityKind: "wwn", IdentityValue: ext4TestWWN,
			Confirm: []string{"partition"}, DiskBytes: 8 << 20, Parts: partSlice(), Token: wwnPlan.(PartitionWritePlan).Token,
		})
		_, err := partApplyApply(context.Background(), env, raw)
		done <- err
	}()
	var once sync.Once
	letGo := func() { once.Do(func() { close(release) }) }
	defer letGo()
	<-entered
	raw, _ := json.Marshal(partApplyApplyArgs{
		ByID: "/dev/disk/by-id/virtio-mmvtestdisk01", IdentityKind: "serial", IdentityValue: "mmvtestdisk01",
		Confirm: []string{"partition"}, DiskBytes: 8 << 20, Parts: partSlice(), Token: serialPlan.(PartitionWritePlan).Token,
	})
	_, err = partApplyApply(context.Background(), env, raw)
	if code(err) != proto.CodeBusy {
		t.Fatalf("alias apply: %v", err)
	}
	letGo()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("sfdisk calls = %d", calls.Load())
	}
}

func TestPartitionLayoutSwapDoesNotRun(t *testing.T) {
	env := newPartEnv(t)
	var calls atomic.Int32
	env.PartSfdisk = func(context.Context, []string, []byte) (runner.Result, error) {
		calls.Add(1)
		return runner.Result{ExitCode: 0}, nil
	}
	plan, err := partApplyPlan(context.Background(), env, partRaw(partSlice()))
	if err != nil {
		t.Fatal(err)
	}
	other := []partplan.Slice{{StartBytes: 1 << 20, SizeBytes: 1 << 20, FSType: "ext4"}}
	raw, _ := json.Marshal(partApplyApplyArgs{
		ByID: ext4TestByID, IdentityKind: "wwn", IdentityValue: ext4TestWWN,
		Confirm: []string{"partition"}, DiskBytes: 8 << 20, Parts: other, Token: plan.(PartitionWritePlan).Token,
	})
	if _, err := partApplyApply(context.Background(), env, raw); code(err) != proto.CodeConflict {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("sfdisk ran")
	}
}

func TestPartitionNonZeroIsUncertain(t *testing.T) {
	env := newPartEnv(t)
	env.PartSfdisk = func(context.Context, []string, []byte) (runner.Result, error) {
		return runner.Result{ExitCode: 1}, nil
	}
	plan, err := partApplyPlan(context.Background(), env, partRaw(partSlice()))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(partApplyApplyArgs{
		ByID: ext4TestByID, IdentityKind: "wwn", IdentityValue: ext4TestWWN,
		Confirm: []string{"partition"}, DiskBytes: 8 << 20, Parts: partSlice(), Token: plan.(PartitionWritePlan).Token,
	})
	got, err := partApplyApply(context.Background(), env, raw)
	if err != nil {
		t.Fatal(err)
	}
	out := got.(PartitionWriteOutcome)
	if out.Outcome != OutcomeUncertain || !strings.Contains(out.Message, "exited with status 1") {
		t.Fatalf("%+v", out)
	}
}

func TestPartitionRefusesAPartitionNode(t *testing.T) {
	env := newPartEnv(t)
	raw, _ := json.Marshal(partApplyArgs{
		ByID: ext4TestByID + "-part1", IdentityKind: "wwn", IdentityValue: ext4TestWWN,
		Confirm: []string{"partition"}, DiskBytes: 8 << 20, Parts: partSlice(),
	})
	if _, err := partApplyPlan(context.Background(), env, raw); code(err) != proto.CodeBadRequest {
		t.Fatal(err)
	}
}
