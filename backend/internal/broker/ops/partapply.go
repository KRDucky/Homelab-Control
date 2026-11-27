// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/runner"
	. "homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/broker/writes"
	"homelab-control/backend/internal/hostinfo"
	"homelab-control/backend/internal/partplan"
	"homelab-control/backend/internal/storagecap"
)

// storage.partition.plan / storage.partition.apply write one GPT label with
// the closed template `sfdisk --wipe=always --wipe-partitions=always -- <by-id>`.
// The script is generated from the reviewed layout. It is not a caller argv.
// Review rules:
//   - one whole-disk by-id path (no -partN, no -F, no --force);
//   - the script is only label, unit, and start/size/type lines;
//   - persist identity matches the by-id name, and the device-number lock
//     is the same lock ext4 format uses, so the two writes cannot overlap;
//   - the disk is a block device, unmounted, and without holders;
//   - logical block size and disk size are read again immediately before exec;
//   - a killed, timed-out, or non-zero sfdisk is uncertain and is not retried;
//   - after exit 0 the table is read back with sfdisk --json and must equal
//     the plan, or the outcome is uncertain;
//   - nothing in this op formats a filesystem.
// The main broker unit stays PrivateDevices=yes.

const partApplyOpName = "storage.partition"

const partApplyAction = "partition"

var partApplyTimeout = 2 * time.Minute

var partRdevRE = regexp.MustCompile(`^[0-9]+:[0-9]+$`)

type partToolFact struct {
	Family    string
	Installed bool
}

type partApplyArgs struct {
	ByID          string           `json:"by_id"`
	IdentityKind  string           `json:"identity_kind"`
	IdentityValue string           `json:"identity_value"`
	Confirm       []string         `json:"confirm"`
	DiskBytes     uint64           `json:"disk_bytes"`
	Parts         []partplan.Slice `json:"parts"`
}

type partApplyApplyArgs struct {
	ByID          string           `json:"by_id"`
	IdentityKind  string           `json:"identity_kind"`
	IdentityValue string           `json:"identity_value"`
	Confirm       []string         `json:"confirm"`
	DiskBytes     uint64           `json:"disk_bytes"`
	Parts         []partplan.Slice `json:"parts"`
	Token         string           `json:"token"`
}

func partApplyAuditArgs(raw json.RawMessage) string {
	var a partApplyApplyArgs
	if proto.DecodeArgs(raw, &a) != nil {
		return "invalid args"
	}
	kind, _, err := canonicalExt4Identity(a.IdentityKind, a.IdentityValue)
	if err != nil {
		return "invalid args"
	}
	return fmt.Sprintf("kind=%s slices=%d", kind, len(a.Parts))
}

func partApplyRequest(a partApplyArgs) (storagecap.IdentityKind, string, string, error) {
	if _, err := runner.RootArgv(runner.RootSfdiskGPT, a.ByID); err != nil {
		return "", "", "", &proto.Error{Code: proto.CodeBadRequest, Message: "by_id must be one whole /dev/disk/by-id path the partition template accepts"}
	}
	kind, value, err := canonicalExt4Identity(a.IdentityKind, a.IdentityValue)
	if err != nil {
		return "", "", "", err
	}
	if err := ext4IdentityAgrees(kind, value, a.ByID); err != nil {
		return "", "", "", err
	}
	if len(a.Confirm) != 1 || a.Confirm[0] != string(storagecap.ConfirmPartition) {
		return "", "", "", &proto.Error{Code: proto.CodeBadRequest, Message: "confirm must be exactly [partition]"}
	}
	script, err := partplan.GPTScript(partplan.Request{DiskBytes: a.DiskBytes, Parts: a.Parts}, 512)
	if err != nil {
		// 512 is only a shape check here. The observed size is checked later.
		script, err = partplan.GPTScript(partplan.Request{DiskBytes: a.DiskBytes, Parts: a.Parts}, 4096)
	}
	if err != nil {
		return "", "", "", &proto.Error{Code: proto.CodeBadRequest, Message: err.Error()}
	}
	return kind, value, script, nil
}

func partApplyLock(target string) string { return "storage-partition:" + target }

func partStateDigest(kind storagecap.IdentityKind, value, byID, rdev string, sector, diskBytes uint64, script string) string {
	sum := sha256.Sum256([]byte(string(kind) + "\n" + value + "\n" + byID + "\n" + rdev + "\n" +
		strconv.FormatUint(sector, 10) + "\n" + strconv.FormatUint(diskBytes, 10) + "\n" + script))
	return hex.EncodeToString(sum[:])
}

func (env *Env) partView() Ext4FormatView {
	if env.PartDisk != nil {
		return env.PartDisk
	}
	return systemExt4View{}
}

func (env *Env) partTooling() (storagecap.Tooling, error) {
	if env.PartTooling != nil {
		return storagecap.Tooling{
			Family: env.PartTooling.Family,
			Required: []storagecap.ToolItem{{
				ID: "sfdisk", Package: sfdiskPackage(env.PartTooling.Family), Installed: env.PartTooling.Installed, Binary: "/usr/sbin/sfdisk",
			}},
		}, nil
	}
	family := "unknown"
	if rel, _, err := hostinfo.ReadOSRelease(env.Roots.Path("/etc")); err == nil {
		family = hostinfo.DistroFamily(rel)
	}
	return storagecap.Probe(family, []storagecap.Spec{{
		ID: "sfdisk", Package: sfdiskPackage(family), Paths: []string{"/usr/sbin/sfdisk", "/usr/bin/sfdisk", "/sbin/sfdisk"}, Required: true,
	}}, nil), nil
}

func sfdiskPackage(family string) string {
	switch family {
	case "debian":
		return "fdisk"
	case "fedora", "suse", "arch":
		return "util-linux"
	default:
		return ""
	}
}

func requirePartApply(env *Env) error {
	if env.Writes == nil {
		return &proto.Error{Code: proto.CodeUnavailable, Message: "the broker operation journal is not available, so write operations are disabled"}
	}
	return nil
}

func partGate(tool storagecap.Tooling, dev storagecap.DeviceRef, fact Ext4DeviceFact) error {
	op := storagecap.Op{
		Phase:          storagecap.PhaseConfirm,
		Tooling:        tool,
		Devices:        []storagecap.DeviceRef{dev},
		Extra:          []storagecap.ExtraConfirm{storagecap.ConfirmPartition},
		Acked:          []storagecap.ExtraConfirm{storagecap.ConfirmPartition},
		IdentityDigest: storagecap.DigestDevices([]storagecap.DeviceRef{dev}),
		ObservedDigest: storagecap.DigestDevices([]storagecap.DeviceRef{dev}),
		PlannedRdev:    []string{fact.Rdev},
		Preflight: []storagecap.DevicePreflight{{
			Device: dev, BlockDevice: fact.BlockDevice, Mounted: fact.Mounted, HasHolders: fact.HasHolders, Rdev: fact.Rdev,
		}},
	}
	next := storagecap.Advance(op)
	if next.Block != "" || next.Phase != storagecap.PhaseExecute {
		msg := next.Reason
		if msg == "" {
			msg = "the disk is not eligible for partitioning"
		}
		return &proto.Error{Code: proto.CodePolicyDenied, Message: msg}
	}
	return nil
}

func partitionWord(err error) error {
	var pe *proto.Error
	if errors.As(err, &pe) {
		pe.Message = strings.ReplaceAll(pe.Message, "formatted", "partitioned")
		pe.Message = strings.ReplaceAll(pe.Message, "ext4 format", "partition")
		return pe
	}
	return err
}

func (env *Env) partGeometry(rdev string) (uint64, uint64, error) {
	if env.PartGeometry != nil {
		return env.PartGeometry(rdev)
	}
	if !partRdevRE.MatchString(rdev) {
		return 0, 0, &proto.Error{Code: proto.CodePolicyDenied, Message: "the disk device number could not be read"}
	}
	sector, err := readSysfsUint(env, "/sys/dev/block/"+rdev+"/queue/logical_block_size")
	if err != nil {
		return 0, 0, &proto.Error{Code: proto.CodeUnavailable, Message: "the logical block size could not be read; nothing was partitioned"}
	}
	sectors, err := readSysfsUint(env, "/sys/dev/block/"+rdev+"/size")
	if err != nil {
		return 0, 0, &proto.Error{Code: proto.CodeUnavailable, Message: "the disk size could not be read; nothing was partitioned"}
	}
	if sectors > (^uint64(0))/512 {
		return 0, 0, &proto.Error{Code: proto.CodePolicyDenied, Message: "the disk size is not usable"}
	}
	return sector, sectors * 512, nil
}

func readSysfsUint(env *Env, rel string) (uint64, error) {
	b, err := readFixed(env.Roots.Path(rel), 64)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(b))
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil || strings.ContainsAny(s, "\n ") {
		return 0, fmt.Errorf("not a number")
	}
	return n, nil
}

func partApplyPlan(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	var a partApplyArgs
	if err := proto.DecodeArgs(raw, &a); err != nil {
		return nil, &proto.Error{Code: proto.CodeBadRequest, Message: "arguments must be by_id, identity, confirm, disk_bytes, and parts"}
	}
	kind, value, _, err := partApplyRequest(a)
	if err != nil {
		return nil, err
	}
	if env.Actor.User == "" || env.Actor.Session == "" {
		return nil, &proto.Error{Code: proto.CodeForbidden, Message: "write operations must be requested from an authenticated session"}
	}
	if err := requirePartApply(env); err != nil {
		return nil, err
	}
	tool, err := env.partTooling()
	if err != nil {
		return nil, err
	}
	fact, err := env.partView().Inspect(ctx, env, a.ByID)
	if err != nil {
		return nil, partitionWord(err)
	}
	sector, diskBytes, err := env.partGeometry(fact.Rdev)
	if err != nil {
		return nil, err
	}
	if diskBytes != a.DiskBytes {
		return nil, &proto.Error{Code: proto.CodeConflict, Message: "disk_bytes does not match the disk; nothing was partitioned"}
	}
	script, err := partplan.GPTScript(partplan.Request{DiskBytes: diskBytes, Parts: a.Parts}, sector)
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeBadRequest, Message: err.Error()}
	}
	dev := storagecap.DeviceRef{Kind: kind, Value: value}
	if err := partGate(tool, dev, fact); err != nil {
		return nil, err
	}
	target := ext4IdentityTarget(kind, value)
	if env.Writes.Locked(partApplyLock(target)) || env.Writes.Locked(ext4DeviceLock(fact.Rdev)) {
		return nil, &proto.Error{Code: proto.CodeBusy, Message: "another partition or ext4 format of this disk is in progress"}
	}
	digest := partStateDigest(kind, value, a.ByID, fact.Rdev, sector, diskBytes, script)
	tok, b, err := env.Writes.Mint(writes.Binding{
		Op: partApplyOpName, Action: partApplyAction, Target: target,
		Actor: env.Actor.User, Session: env.Actor.Session, StateDigest: digest, Risk: RiskDestructive,
	})
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeInternal, Message: "could not issue a plan token"}
	}
	return PartitionWritePlan{
		Action: partApplyAction, Risk: RiskDestructive, StepUpRequired: true, ConfirmationRequired: true,
		Token: tok, ExpiresAt: b.Expires, IdentityKind: string(kind), Rdev: fact.Rdev, SliceCount: len(a.Parts),
		Effect:       "Replace the partition table on this disk with the reviewed GPT layout. Existing contents are destroyed. Filesystems are not formatted.",
		Consequences: []string{"There is no undo.", "The disk is checked again immediately before sfdisk runs.", "A partition write that is killed is not retried."},
		Warnings:     []string{"storage.read does not authorize this.", "ext4.format does not authorize this.", "The main broker does not gain device access. If this broker cannot see the disk, the write is refused."},
	}, nil
}

func (env *Env) runPartApply(ctx context.Context, byID, script string) (runner.Result, error) {
	argv, err := runner.RootArgv(runner.RootSfdiskGPT, byID)
	if err != nil {
		return runner.Result{}, err
	}
	stdin := []byte(script)
	if env.PartSfdisk != nil {
		return env.PartSfdisk(ctx, argv, stdin)
	}
	if env.RootExec == nil {
		return runner.Result{}, &proto.Error{Code: proto.CodeForbidden, Message: "root execution is not available to this operation"}
	}
	return env.RootExec.RunStdin(ctx, runner.RootSfdiskGPT, stdin, byID)
}

// readPartTable runs the closed read-only sfdisk --json template on the disk
// just written and compares the table with the planned rows.
func (env *Env) readPartTable(ctx context.Context, byID string, want []partplan.Row) error {
	argv, err := runner.RootArgv(runner.RootSfdiskJSON, byID)
	if err != nil {
		return err
	}
	var res runner.Result
	switch {
	case env.PartReadback != nil:
		res, err = env.PartReadback(ctx, argv)
	case env.RootExec != nil:
		res, err = env.RootExec.Run(ctx, runner.RootSfdiskJSON, byID)
	default:
		return errors.New("root execution is not available for the read-back")
	}
	if err != nil {
		return fmt.Errorf("sfdisk --json could not run (%s)", execClass(err))
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("sfdisk --json exited with status %d", res.ExitCode)
	}
	return partplan.CheckSfdiskJSON(res.Stdout, want)
}

const partRecovery = "Do not retry blindly. The partition table may be partial. Inspect the disk, then review a new plan."

func partApplyApply(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	var a partApplyApplyArgs
	if err := proto.DecodeArgs(raw, &a); err != nil {
		return nil, &proto.Error{Code: proto.CodeBadRequest, Message: "arguments must be the reviewed fields plus token"}
	}
	req := partApplyArgs{ByID: a.ByID, IdentityKind: a.IdentityKind, IdentityValue: a.IdentityValue, Confirm: a.Confirm, DiskBytes: a.DiskBytes, Parts: a.Parts}
	kind, value, _, err := partApplyRequest(req)
	if err != nil {
		return nil, err
	}
	if err := requirePartApply(env); err != nil {
		return nil, err
	}
	target := ext4IdentityTarget(kind, value)
	b, err := env.Writes.Consume(a.Token, writes.Request{
		Op: partApplyOpName, Action: partApplyAction, Target: target,
		Actor: env.Actor.User, Session: env.Actor.Session, StepUpAt: env.Actor.StepUpAt,
	})
	switch {
	case errors.Is(err, writes.ErrStepUp):
		return nil, &proto.Error{Code: proto.CodeStepUpRequired, Message: "re-enter your password to confirm this partition"}
	case err != nil:
		return nil, &proto.Error{Code: proto.CodeInvalidToken, Message: "the plan is invalid, expired or already used; review the partition again"}
	}
	unlock, err := env.Writes.Lock(partApplyLock(target))
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeBusy, Message: "another partition of this disk is in progress"}
	}
	defer unlock()
	tool, err := env.partTooling()
	if err != nil {
		return nil, err
	}
	fact, err := env.partView().Inspect(ctx, env, a.ByID)
	if err != nil {
		return nil, partitionWord(err)
	}
	if fact.Rdev != "" {
		unlockDev, err := env.Writes.Lock(ext4DeviceLock(fact.Rdev))
		if err != nil {
			return nil, &proto.Error{Code: proto.CodeBusy, Message: "another partition or ext4 format of this disk is in progress"}
		}
		defer unlockDev()
	}
	dev := storagecap.DeviceRef{Kind: kind, Value: value}
	if err := partGate(tool, dev, fact); err != nil {
		return nil, err
	}
	sector, diskBytes, err := env.partGeometry(fact.Rdev)
	if err != nil {
		return nil, err
	}
	script, scriptErr := partplan.GPTScript(partplan.Request{DiskBytes: diskBytes, Parts: a.Parts}, sector)
	digest := ""
	if scriptErr == nil && diskBytes == a.DiskBytes {
		digest = partStateDigest(kind, value, a.ByID, fact.Rdev, sector, diskBytes, script)
	}
	if digest == "" || digest != b.StateDigest {
		return nil, &proto.Error{Code: proto.CodeConflict, Message: "the disk or the layout changed since the partition was reviewed; nothing was partitioned. Review it again."}
	}
	rec := writes.Record{Op: partApplyOpName, Action: partApplyAction, Target: target, Actor: env.Actor.User,
		RequestID: env.Actor.RequestID, TokenID: b.ID, Risk: b.Risk, State: b.StateDigest}
	opID, err := env.Writes.Intent(rec)
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "the operation journal cannot be written; nothing was partitioned"}
	}
	rec.OpID = opID
	outcomeWritten := false
	defer func() {
		if p := recover(); p != nil {
			if !outcomeWritten {
				fail := rec
				fail.Outcome = OutcomeUncertain
				fail.Message = "The partition helper failed while writing; the result is unknown. It was not retried."
				_ = env.Writes.Outcome(fail)
			}
			panic(p)
		}
	}()
	fact2, err2 := env.partView().Inspect(ctx, env, a.ByID)
	sector2, disk2, geoErr := uint64(0), uint64(0), error(nil)
	if err2 == nil {
		sector2, disk2, geoErr = env.partGeometry(fact2.Rdev)
	}
	script2, scriptErr2 := "", error(nil)
	if geoErr == nil && err2 == nil {
		script2, scriptErr2 = partplan.GPTScript(partplan.Request{DiskBytes: disk2, Parts: a.Parts}, sector2)
	}
	fresh := ""
	if scriptErr2 == nil && disk2 == a.DiskBytes {
		fresh = partStateDigest(kind, value, a.ByID, fact2.Rdev, sector2, disk2, script2)
	}
	if err2 != nil || geoErr != nil || partGate(tool, dev, fact2) != nil || fresh != digest || script2 != script {
		out := partFinish(opID, OutcomeFailed, "The disk was not eligible at the moment of partition. Nothing was partitioned.", true)
		rec.Outcome, rec.Message = out.Outcome, out.Message
		outcomeWritten = true
		_ = env.Writes.Outcome(rec)
		return out, nil
	}
	started := time.Now().UTC()
	actx, acancel := context.WithTimeout(context.Background(), partApplyTimeout)
	res, runErr := env.runPartApply(actx, a.ByID, script)
	acancel()
	after, afterErr := env.partView().Inspect(context.Background(), env, a.ByID)
	outcome, msg := classifyPartApply(res, runErr, after, afterErr, fact.Rdev)
	if outcome == OutcomeSucceeded {
		rows, rowsErr := partplan.GPTRows(partplan.Request{DiskBytes: diskBytes, Parts: a.Parts}, sector)
		if rowsErr == nil {
			rctx, rcancel := context.WithTimeout(context.Background(), 30*time.Second)
			rowsErr = env.readPartTable(rctx, a.ByID, rows)
			rcancel()
		}
		if rowsErr != nil {
			outcome = OutcomeUncertain
			msg = "sfdisk exited 0, but the new partition table could not be confirmed: " + rowsErr.Error() + ". No filesystem was formatted. Nothing further was run."
		} else {
			msg = fmt.Sprintf("sfdisk exited 0, the device identity still matches the plan, and sfdisk --json reads back the planned table (%d partitions). No filesystem was formatted.", len(rows))
		}
	}
	out := PartitionWriteOutcome{OperationID: opID, Action: partApplyAction, Outcome: outcome, Message: msg,
		StartedAt: started, FinishedAt: time.Now().UTC()}
	if outcome != OutcomeSucceeded {
		out.Recovery = []string{partRecovery, "On the host, do not run sfdisk again until the device identity and mounts have been checked."}
	}
	rec.Outcome, rec.Message = outcome, msg
	if runErr == nil {
		code := res.ExitCode
		rec.ExitCode = &code
	}
	outcomeWritten = true
	if err := env.Writes.Outcome(rec); err != nil {
		out.Message += " Warning: the outcome could not be written to the operation journal."
	}
	return out, nil
}

func partFinish(opID, outcome, msg string, recovery bool) PartitionWriteOutcome {
	out := PartitionWriteOutcome{OperationID: opID, Action: partApplyAction, Outcome: outcome, Message: msg,
		StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()}
	if recovery {
		out.Recovery = []string{partRecovery}
	}
	return out
}

func classifyPartApply(res runner.Result, runErr error, after Ext4DeviceFact, afterErr error, rdev string) (string, string) {
	same := afterErr == nil && after.BlockDevice && after.Rdev == rdev && !after.Mounted && !after.HasHolders
	switch {
	case errors.Is(runErr, runner.ErrTimeout):
		return OutcomeUncertain, "sfdisk did not finish within " + partApplyTimeout.String() + ". The result is not verified. It was not retried."
	case runErr != nil:
		return OutcomeUncertain, "sfdisk could not be started or did not return. The result is not verified. It was not retried. (" + execClass(runErr) + ")"
	case !same:
		if res.ExitCode == 0 {
			return OutcomeUncertain, "sfdisk exited 0, but the device identity could not be read back, so the result is not verified."
		}
		return OutcomeUncertain, "sfdisk failed and the device identity could not be read back, so the result is not verified."
	case res.ExitCode != 0:
		return OutcomeUncertain, fmt.Sprintf("sfdisk exited with status %d. The disk may already have been written. Nothing further was run.", res.ExitCode)
	default:
		return OutcomeSucceeded, "sfdisk exited 0 and the device identity still matches the plan."
	}
}
