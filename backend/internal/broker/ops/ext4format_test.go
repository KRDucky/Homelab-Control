// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/runner"
	. "homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/broker/writes"
	"homelab-control/backend/internal/storagecap"
)

const ext4TestByID = "/dev/disk/by-id/virtio-mmvtestdisk01"

const ext4TestWWN = "naa.5000cca252c8d2a1"

type fakeExt4View struct {
	fact Ext4DeviceFact
	err  error
	seen []string
}

func (f *fakeExt4View) Inspect(_ context.Context, _ *Env, byID string) (Ext4DeviceFact, error) {
	f.seen = append(f.seen, byID)
	if f.err != nil {
		return Ext4DeviceFact{}, f.err
	}
	return f.fact, nil
}

func ext4ReadyFact() Ext4DeviceFact {
	return Ext4DeviceFact{BlockDevice: true, Rdev: "254:0"}
}

func ext4ReadyTool() *ext4ToolFact {
	return &ext4ToolFact{Family: "debian", Installed: true}
}

type ext4Env struct {
	env  *Env
	view *fakeExt4View
	argv [][]string
}

func newExt4Env(t *testing.T) *ext4Env {
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
	view := &fakeExt4View{fact: ext4ReadyFact()}
	e := &ext4Env{view: view}
	env := &Env{
		Writes: fw, Ext4Format: view, Ext4Tooling: ext4ReadyTool(),
		Actor: proto.Actor{User: "alice", Session: "sess-a", RequestID: "r1", StepUpAt: time.Now().Unix()},
		Ext4Mkfs: func(_ context.Context, argv []string) (runner.Result, error) {
			e.argv = append(e.argv, append([]string{}, argv...))
			if _, err := runner.RootArgv(runner.RootMkfsExt4, argv[len(argv)-1]); err != nil {
				t.Errorf("hook argv rejected by the closed template: %v", err)
			}
			return runner.Result{ExitCode: 0}, nil
		},
	}
	e.env = env
	return e
}

func ext4Raw(byID, kind, value string) json.RawMessage {
	b, _ := json.Marshal(ext4FormatArgs{ByID: byID, IdentityKind: kind, IdentityValue: value, Confirm: []string{"format"}})
	return b
}

func ext4ApplyRaw(byID, kind, value, token string) json.RawMessage {
	b, _ := json.Marshal(ext4FormatApplyArgs{ByID: byID, IdentityKind: kind, IdentityValue: value, Confirm: []string{"format"}, Token: token})
	return b
}

func ext4Plan(t *testing.T, e *ext4Env) Ext4FormatPlan {
	t.Helper()
	got, err := ext4FormatPlan(context.Background(), e.env, ext4Raw(ext4TestByID, "wwn", ext4TestWWN))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	return got.(Ext4FormatPlan)
}

func TestExt4FormatPlanAndApplyUsesClosedArgv(t *testing.T) {
	e := newExt4Env(t)
	p := ext4Plan(t, e)
	if !p.StepUpRequired || p.Risk != RiskDestructive || p.Token == "" || p.Rdev != "254:0" || p.IdentityKind != "wwn" {
		t.Fatalf("%+v", p)
	}
	got, err := ext4FormatApply(context.Background(), e.env, ext4ApplyRaw(ext4TestByID, "wwn", ext4TestWWN, p.Token))
	if err != nil {
		t.Fatal(err)
	}
	out := got.(Ext4FormatOutcome)
	if out.Outcome != OutcomeSucceeded || len(e.argv) != 1 {
		t.Fatalf("outcome %+v argv %v", out, e.argv)
	}
	if strings.Join(e.argv[0], " ") != "-q -- "+ext4TestByID {
		t.Fatalf("argv %q", e.argv[0])
	}
	if _, err := ext4FormatApply(context.Background(), e.env, ext4ApplyRaw(ext4TestByID, "wwn", ext4TestWWN, p.Token)); code(err) != proto.CodeInvalidToken {
		t.Fatalf("replay: %v", err)
	}
}

func TestExt4FormatStepUpDoesNotConsume(t *testing.T) {
	e := newExt4Env(t)
	p := ext4Plan(t, e)
	e.env.Actor.StepUpAt = 0
	if _, err := ext4FormatApply(context.Background(), e.env, ext4ApplyRaw(ext4TestByID, "wwn", ext4TestWWN, p.Token)); code(err) != proto.CodeStepUpRequired {
		t.Fatalf("step-up: %v", err)
	}
	if e.env.Writes.Outstanding() != 1 || len(e.argv) != 0 {
		t.Fatalf("token consumed or mkfs ran: outstanding %d argv %d", e.env.Writes.Outstanding(), len(e.argv))
	}
	e.env.Actor.StepUpAt = time.Now().Unix()
	got, err := ext4FormatApply(context.Background(), e.env, ext4ApplyRaw(ext4TestByID, "wwn", ext4TestWWN, p.Token))
	if err != nil || got.(Ext4FormatOutcome).Outcome != OutcomeSucceeded {
		t.Fatalf("retry after step-up: %v %+v", err, got)
	}
}

func TestExt4FormatRefusesWeakIdentityAndBadPath(t *testing.T) {
	e := newExt4Env(t)
	for _, raw := range []json.RawMessage{
		ext4Raw(ext4TestByID, "by-id", ext4TestByID),
		ext4Raw(ext4TestByID, "kernel", "vda"),
		ext4Raw("/dev/vda", "wwn", ext4TestWWN),
		ext4Raw(ext4TestByID, "serial", "ab"),
	} {
		if _, err := ext4FormatPlan(context.Background(), e.env, raw); code(err) != proto.CodeBadRequest {
			t.Fatalf("accepted %s: %v", raw, err)
		}
	}
	bad := ext4FormatArgs{ByID: ext4TestByID, IdentityKind: "wwn", IdentityValue: ext4TestWWN, Confirm: []string{"force"}}
	b, _ := json.Marshal(bad)
	if _, err := ext4FormatPlan(context.Background(), e.env, b); code(err) != proto.CodeBadRequest {
		t.Fatal(err)
	}
	if len(e.argv) != 0 {
		t.Fatal("mkfs ran")
	}
}

func TestExt4FormatPathSwapDoesNotFormat(t *testing.T) {
	e := newExt4Env(t)
	p := ext4Plan(t, e)
	other := "/dev/disk/by-id/virtio-mmvtestdisk02"
	if _, err := ext4FormatApply(context.Background(), e.env, ext4ApplyRaw(other, "wwn", ext4TestWWN, p.Token)); code(err) != proto.CodeConflict {
		t.Fatalf("swap: %v", err)
	}
	if len(e.argv) != 0 {
		t.Fatal("mkfs ran after a path swap")
	}
}

func TestExt4FormatIdentitySwapDoesNotFormat(t *testing.T) {
	e := newExt4Env(t)
	p := ext4Plan(t, e)
	if _, err := ext4FormatApply(context.Background(), e.env, ext4ApplyRaw(ext4TestByID, "wwn", "naa.5000cca2deadbeef", p.Token)); code(err) != proto.CodeInvalidToken {
		t.Fatalf("identity swap: %v", err)
	}
	if e.env.Writes.Outstanding() != 1 || len(e.argv) != 0 {
		t.Fatal("token consumed or mkfs ran")
	}
}

func TestExt4FormatRefusesMountedHolderAndFile(t *testing.T) {
	e := newExt4Env(t)
	for _, fact := range []Ext4DeviceFact{
		{BlockDevice: false},
		{BlockDevice: true, Mounted: true, Rdev: "254:0"},
		{BlockDevice: true, HasHolders: true, Rdev: "254:0"},
		{BlockDevice: true, Rdev: "0:1"},
	} {
		e.view.fact = fact
		if _, err := ext4FormatPlan(context.Background(), e.env, ext4Raw(ext4TestByID, "wwn", ext4TestWWN)); code(err) != proto.CodePolicyDenied {
			t.Fatalf("fact %+v: %v", fact, err)
		}
	}
	e.view.fact = ext4ReadyFact()
	p := ext4Plan(t, e)
	e.view.fact = Ext4DeviceFact{BlockDevice: true, Mounted: true, Rdev: "254:0"}
	if _, err := ext4FormatApply(context.Background(), e.env, ext4ApplyRaw(ext4TestByID, "wwn", ext4TestWWN, p.Token)); code(err) != proto.CodePolicyDenied {
		t.Fatalf("mounted at revalidation: %v", err)
	}
	if len(e.argv) != 0 {
		t.Fatal("mkfs ran on a mounted disk")
	}
	e.view.fact = ext4ReadyFact()
	p = ext4Plan(t, e)
	e.env.Ext4Format = &flipExt4View{ready: ext4ReadyFact(), later: Ext4DeviceFact{BlockDevice: true, Mounted: true, Rdev: "254:0"}}
	got, err := ext4FormatApply(context.Background(), e.env, ext4ApplyRaw(ext4TestByID, "wwn", ext4TestWWN, p.Token))
	if err != nil {
		t.Fatal(err)
	}
	out := got.(Ext4FormatOutcome)
	if out.Outcome != OutcomeFailed || len(e.argv) != 0 || !strings.Contains(out.Message, "Nothing was formatted") {
		t.Fatalf("%+v argv %d", out, len(e.argv))
	}
}

type flipExt4View struct {
	ready Ext4DeviceFact
	later Ext4DeviceFact
	n     int
}

func (f *flipExt4View) Inspect(context.Context, *Env, string) (Ext4DeviceFact, error) {
	f.n++
	if f.n == 1 {
		return f.ready, nil
	}
	return f.later, nil
}

func TestExt4FormatJournalBeforeExec(t *testing.T) {
	e := newExt4Env(t)
	_ = ext4Plan(t, e)
	fw, err := writes.New(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	e.env.Writes = fw
	digest := ext4StateDigest(storagecap.IdentityWWN, ext4TestWWN, ext4TestByID, "254:0")
	tok, _, err := fw.Mint(writes.Binding{
		Op: ext4FormatOpName, Action: ext4FormatAction, Target: "wwn:" + ext4TestWWN,
		Actor: "alice", Session: "sess-a", StateDigest: digest, Risk: RiskDestructive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ext4FormatApply(context.Background(), e.env, ext4ApplyRaw(ext4TestByID, "wwn", ext4TestWWN, tok)); code(err) != proto.CodeUnavailable {
		t.Fatalf("journal: %v", err)
	}
	if len(e.argv) != 0 {
		t.Fatal("mkfs ran without a journal")
	}
}

func TestExt4FormatTimeoutIsUncertain(t *testing.T) {
	e := newExt4Env(t)
	p := ext4Plan(t, e)
	e.env.Ext4Mkfs = func(context.Context, []string) (runner.Result, error) {
		return runner.Result{}, runner.ErrTimeout
	}
	got, err := ext4FormatApply(context.Background(), e.env, ext4ApplyRaw(ext4TestByID, "wwn", ext4TestWWN, p.Token))
	if err != nil {
		t.Fatal(err)
	}
	out := got.(Ext4FormatOutcome)
	if out.Outcome != OutcomeUncertain || len(out.Recovery) == 0 || !strings.Contains(out.Message, "not retried") {
		t.Fatalf("%+v", out)
	}
}

func TestExt4FormatPanicRecordsUncertain(t *testing.T) {
	e := newExt4Env(t)
	p := ext4Plan(t, e)
	e.env.Ext4Mkfs = func(context.Context, []string) (runner.Result, error) { panic("format helper") }
	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("panic was swallowed")
		}
		entries, err := e.env.Writes.Read()
		if err != nil || len(entries) != 1 || entries[0].Final == nil || entries[0].Final.Outcome != OutcomeUncertain {
			t.Fatalf("journal: %v %+v", err, entries)
		}
	}()
	_, _ = ext4FormatApply(context.Background(), e.env, ext4ApplyRaw(ext4TestByID, "wwn", ext4TestWWN, p.Token))
}

func TestExt4FormatMissingBinaryRefuses(t *testing.T) {
	e := newExt4Env(t)
	e.env.Ext4Tooling = &ext4ToolFact{Family: "debian", Installed: false}
	if _, err := ext4FormatPlan(context.Background(), e.env, ext4Raw(ext4TestByID, "wwn", ext4TestWWN)); code(err) != proto.CodePolicyDenied {
		t.Fatal(err)
	}
	e.env.Ext4Tooling = &ext4ToolFact{Family: "unknown", Installed: true}
	if _, err := ext4FormatPlan(context.Background(), e.env, ext4Raw(ext4TestByID, "serial", "WD-WCAY12345678")); code(err) != proto.CodePolicyDenied {
		t.Fatal(err)
	}
}

func TestExt4FormatNoRootExecDoesNotCallMkfs(t *testing.T) {
	e := newExt4Env(t)
	p := ext4Plan(t, e)
	e.env.Ext4Mkfs = nil
	e.env.RootExec = nil
	got, err := ext4FormatApply(context.Background(), e.env, ext4ApplyRaw(ext4TestByID, "wwn", ext4TestWWN, p.Token))
	if err != nil {
		t.Fatal(err)
	}
	out := got.(Ext4FormatOutcome)
	if out.Outcome != OutcomeUncertain || !strings.Contains(out.Message, "not verified") {
		t.Fatalf("%+v", out)
	}
}

func TestExt4MountBusy(t *testing.T) {
	parts := map[string]bool{"254:1": true}
	body := "36 35 254:0 / /mnt rw - ext4 /dev/vda rw\n"
	if !ext4MountBusy(body, "254:0", nil) {
		t.Fatal("disk mount missed")
	}
	if !ext4MountBusy("36 35 254:1 / /mnt rw - ext4 /dev/vda1 rw\n", "254:0", parts) {
		t.Fatal("partition mount missed")
	}
	if ext4MountBusy("36 35 8:0 / / rw - ext4 /dev/sda rw\n", "254:0", parts) {
		t.Fatal("unrelated mount treated as busy")
	}
}

func TestLinuxRdevMatchesDevNull(t *testing.T) {
	fi, err := os.Stat("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := linuxRdev(fi)
	if !ok || got != "1:3" {
		t.Fatalf("rdev %q ok %v", got, ok)
	}
}

func TestSystemExt4ViewRefusesRegularFile(t *testing.T) {
	root := t.TempDir()
	byID := "dev/disk/by-id/virtio-mmvtestdisk01"
	if err := os.MkdirAll(filepath.Join(root, "dev/disk/by-id"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dev/vda"), []byte("not a disk"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../vda", filepath.Join(root, byID)); err != nil {
		t.Fatal(err)
	}
	env := &Env{Roots: Roots{Root: root}}
	fact, err := systemExt4View{}.Inspect(context.Background(), env, "/"+byID)
	if err != nil {
		t.Fatal(err)
	}
	if fact.BlockDevice {
		t.Fatal("regular file looked like a block device")
	}
}

func TestExt4FormatAuditOmitsPath(t *testing.T) {
	got := ext4FormatAuditArgs(ext4Raw(ext4TestByID, "wwn", ext4TestWWN))
	if got != "kind=wwn" || strings.Contains(got, "virtio") || strings.Contains(got, ext4TestWWN) {
		t.Fatal(got)
	}
}

func TestExt4FormatGateUsesStoragecap(t *testing.T) {
	dev := storagecap.DeviceRef{Kind: storagecap.IdentityWWN, Value: ext4TestWWN}
	tool := storagecap.Tooling{Family: "debian", Required: []storagecap.ToolItem{{ID: "mkfs.ext4", Installed: true}}}
	if err := ext4Gate(tool, dev, Ext4DeviceFact{BlockDevice: true, Mounted: true, Rdev: "254:0"}); code(err) != proto.CodePolicyDenied {
		t.Fatal(err)
	}
	if err := ext4Gate(tool, dev, ext4ReadyFact()); err != nil {
		t.Fatal(err)
	}
}
