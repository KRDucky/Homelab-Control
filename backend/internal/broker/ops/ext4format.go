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
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/runner"
	. "homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/broker/writes"
	"homelab-control/backend/internal/hostinfo"
	"homelab-control/backend/internal/storagecap"
)

// ext4.format.plan / ext4.format.apply — quiet mkfs.ext4 of one by-id path.
// Review rules:
//   - the only argv is runner.RootMkfsExt4 (mkfs.ext4 -q -- <one by-id>);
//   - no -F, no extra flags, no caller argv;
//   - persist identity is wwn, nvme-uuid, or serial, and the by-id basename
//     must name that same identity; the by-id string is also inside the
//     state digest, not the token target (G4);
//   - risk is destructive, so a recent password re-entry is required;
//   - the device must be a block device, unmounted (including swap and
//     partitions listed as sysfs symlinks), and without holders or slaves,
//     under the format lock;
//   - that check runs again immediately before exec (G2);
//   - the child uses the runner's non-TTY stdin (G1);
//   - a killed, timed-out, or non-zero mkfs is uncertain and is not retried (G5);
//   - the main broker unit is not widened (G3). A broker that cannot see
//     the device fails closed.

const ext4FormatOpName = "ext4.format"

const ext4FormatAction = "format"

// ext4FormatTimeout is the mkfs child budget, independent of the request
// context. The apply op timeout is longer than this.
var ext4FormatTimeout = 8 * time.Minute

// ext4ToolFact is the mkfs.ext4 presence probe the format gate reads.
type ext4ToolFact struct {
	Family    string
	Installed bool
}

// Ext4DeviceFact is observed device state. This is not a format.
type Ext4DeviceFact struct {
	BlockDevice bool
	Mounted     bool
	HasHolders  bool
	Rdev        string
}

// Ext4FormatView inspects one by-id path. It must not format.
type Ext4FormatView interface {
	Inspect(ctx context.Context, env *Env, byID string) (Ext4DeviceFact, error)
}

type ext4FormatArgs struct {
	ByID          string   `json:"by_id"`
	IdentityKind  string   `json:"identity_kind"`
	IdentityValue string   `json:"identity_value"`
	Confirm       []string `json:"confirm"`
}

type ext4FormatApplyArgs struct {
	ByID          string   `json:"by_id"`
	IdentityKind  string   `json:"identity_kind"`
	IdentityValue string   `json:"identity_value"`
	Confirm       []string `json:"confirm"`
	Token         string   `json:"token"`
}

var (
	ext4WWNValueRE  = regexp.MustCompile(`^(?:naa\.[0-9a-fA-F]{16}|0x[0-9a-fA-F]{16}|0x[0-9a-fA-F]{32}|[0-9a-fA-F]{16}|[0-9a-fA-F]{32})$`)
	ext4UUIDValueRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	ext4SerialRE    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{2,63}$`)
	ext4PartSuffix  = regexp.MustCompile(`^-part[0-9]{1,3}$`)
)

func ext4FormatAuditArgs(raw json.RawMessage) string {
	var a ext4FormatApplyArgs
	if proto.DecodeArgs(raw, &a) != nil {
		return "invalid args"
	}
	kind, _, err := canonicalExt4Identity(a.IdentityKind, a.IdentityValue)
	if err != nil {
		return "invalid args"
	}
	return "kind=" + string(kind)
}

func canonicalExt4Identity(kind, value string) (storagecap.IdentityKind, string, error) {
	k := storagecap.IdentityKind(kind)
	if !k.Strongest() {
		return "", "", &proto.Error{Code: proto.CodeBadRequest, Message: "persist identity must be wwn, nvme-uuid, or serial"}
	}
	switch k {
	case storagecap.IdentityWWN:
		if !ext4WWNValueRE.MatchString(value) {
			return "", "", &proto.Error{Code: proto.CodeBadRequest, Message: "wwn identity is not a 16- or 32-digit hex value"}
		}
		value = strings.ToLower(value)
	case storagecap.IdentityNVMeUUID:
		if !ext4UUIDValueRE.MatchString(value) {
			return "", "", &proto.Error{Code: proto.CodeBadRequest, Message: "nvme-uuid identity is not a UUID"}
		}
		value = strings.ToLower(value)
	default:
		if !ext4SerialRE.MatchString(value) {
			return "", "", &proto.Error{Code: proto.CodeBadRequest, Message: "serial identity is not an acceptable disk serial"}
		}
	}
	return k, value, nil
}

func ext4FormatRequest(a ext4FormatArgs) (storagecap.IdentityKind, string, error) {
	if _, err := runner.RootArgv(runner.RootMkfsExt4, a.ByID); err != nil {
		return "", "", &proto.Error{Code: proto.CodeBadRequest, Message: "by_id must be one /dev/disk/by-id path the ext4 format template accepts"}
	}
	kind, value, err := canonicalExt4Identity(a.IdentityKind, a.IdentityValue)
	if err != nil {
		return "", "", err
	}
	if err := ext4IdentityAgrees(kind, value, a.ByID); err != nil {
		return "", "", err
	}
	if len(a.Confirm) != 1 || a.Confirm[0] != string(storagecap.ConfirmFormat) {
		return "", "", &proto.Error{Code: proto.CodeBadRequest, Message: "confirm must be exactly [format]"}
	}
	return kind, value, nil
}

// ext4IdentityAgrees requires the by-id basename to name the same disk as the
// persist identity. A virtio path with some other disk's WWN is refused.
func ext4IdentityAgrees(kind storagecap.IdentityKind, value, byID string) error {
	base := filepath.Base(byID)
	if i := strings.LastIndex(base, "-part"); i > 0 && ext4PartSuffix.MatchString(base[i:]) {
		base = base[:i]
	}
	var ok bool
	switch {
	case strings.HasPrefix(base, "wwn-0x"):
		ok = kind == storagecap.IdentityWWN && ext4WWNHex(value) == strings.ToLower(strings.TrimPrefix(base, "wwn-0x"))
	case strings.HasPrefix(base, "nvme-eui."):
		ok = kind == storagecap.IdentityWWN && ext4WWNHex(value) == strings.ToLower(strings.TrimPrefix(base, "nvme-eui."))
	case strings.HasPrefix(base, "nvme-uuid."):
		ok = kind == storagecap.IdentityNVMeUUID && value == strings.ToLower(strings.TrimPrefix(base, "nvme-uuid."))
	case strings.HasPrefix(base, "ata-"), strings.HasPrefix(base, "scsi-"), strings.HasPrefix(base, "virtio-"):
		rest := base[strings.IndexByte(base, '-')+1:]
		ok = kind == storagecap.IdentitySerial && ext4SerialInName(rest, value)
	}
	if !ok {
		return &proto.Error{Code: proto.CodeBadRequest, Message: "by_id does not match the persist identity"}
	}
	return nil
}

func ext4WWNHex(value string) string {
	value = strings.ToLower(value)
	value = strings.TrimPrefix(value, "naa.")
	return strings.TrimPrefix(value, "0x")
}

func ext4SerialInName(rest, serial string) bool {
	return rest == serial || strings.HasSuffix(rest, "_"+serial) || strings.HasSuffix(rest, "-"+serial)
}

func ext4IdentityTarget(kind storagecap.IdentityKind, value string) string {
	return string(kind) + ":" + value
}

func ext4FormatLock(target string) string { return "ext4-format:" + target }

func ext4StateDigest(kind storagecap.IdentityKind, value, byID, rdev string) string {
	sum := sha256.Sum256([]byte(string(kind) + "\n" + value + "\n" + byID + "\n" + rdev + "\nformat"))
	return hex.EncodeToString(sum[:])
}

func (env *Env) ext4View() Ext4FormatView {
	if env.Ext4Format != nil {
		return env.Ext4Format
	}
	return systemExt4View{}
}

func (env *Env) ext4Tooling() (storagecap.Tooling, error) {
	if env.Ext4Tooling != nil {
		installed := env.Ext4Tooling.Installed
		return storagecap.Tooling{
			Family: env.Ext4Tooling.Family,
			Required: []storagecap.ToolItem{{
				ID: "mkfs.ext4", Package: ext4MkfsPackage(env.Ext4Tooling.Family), Installed: installed, Binary: "/usr/sbin/mkfs.ext4",
			}},
		}, nil
	}
	family := "unknown"
	if rel, _, err := hostinfo.ReadOSRelease(env.Roots.Path("/etc")); err == nil {
		family = hostinfo.DistroFamily(rel)
	}
	tool := storagecap.Probe(family, []storagecap.Spec{{
		ID: "mkfs.ext4", Package: ext4MkfsPackage(family), Paths: []string{"/usr/sbin/mkfs.ext4", "/sbin/mkfs.ext4"}, Required: true,
	}}, nil)
	return tool, nil
}

func ext4MkfsPackage(family string) string {
	switch family {
	case "debian", "fedora", "suse", "arch":
		return "e2fsprogs"
	default:
		return ""
	}
}

func requireExt4Format(env *Env) error {
	if env.Writes == nil {
		return &proto.Error{Code: proto.CodeUnavailable, Message: "the broker operation journal is not available, so write operations are disabled"}
	}
	return nil
}

func ext4Gate(tool storagecap.Tooling, dev storagecap.DeviceRef, fact Ext4DeviceFact) error {
	op := storagecap.Op{
		Phase:          storagecap.PhaseConfirm,
		Tooling:        tool,
		Devices:        []storagecap.DeviceRef{dev},
		Extra:          []storagecap.ExtraConfirm{storagecap.ConfirmFormat},
		Acked:          []storagecap.ExtraConfirm{storagecap.ConfirmFormat},
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
			msg = "the disk is not eligible for ext4 format"
		}
		return &proto.Error{Code: proto.CodePolicyDenied, Message: msg}
	}
	return nil
}

func ext4FormatPlan(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	var a ext4FormatArgs
	if err := proto.DecodeArgs(raw, &a); err != nil {
		return nil, &proto.Error{Code: proto.CodeBadRequest, Message: "arguments must be by_id, identity_kind, identity_value, and confirm"}
	}
	kind, value, err := ext4FormatRequest(a)
	if err != nil {
		return nil, err
	}
	if env.Actor.User == "" || env.Actor.Session == "" {
		return nil, &proto.Error{Code: proto.CodeForbidden, Message: "write operations must be requested from an authenticated session"}
	}
	if err := requireExt4Format(env); err != nil {
		return nil, err
	}
	tool, err := env.ext4Tooling()
	if err != nil {
		return nil, err
	}
	fact, err := env.ext4View().Inspect(ctx, env, a.ByID)
	if err != nil {
		return nil, err
	}
	dev := storagecap.DeviceRef{Kind: kind, Value: value}
	if err := ext4Gate(tool, dev, fact); err != nil {
		return nil, err
	}
	target := ext4IdentityTarget(kind, value)
	if env.Writes.Locked(ext4FormatLock(target)) {
		return nil, &proto.Error{Code: proto.CodeBusy, Message: "another ext4 format of this disk is in progress"}
	}
	digest := ext4StateDigest(kind, value, a.ByID, fact.Rdev)
	tok, b, err := env.Writes.Mint(writes.Binding{
		Op: ext4FormatOpName, Action: ext4FormatAction, Target: target,
		Actor: env.Actor.User, Session: env.Actor.Session, StateDigest: digest, Risk: RiskDestructive,
	})
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeInternal, Message: "could not issue a plan token"}
	}
	return Ext4FormatPlan{
		Action: ext4FormatAction, Risk: RiskDestructive, StepUpRequired: true, ConfirmationRequired: true,
		Token: tok, ExpiresAt: b.Expires, IdentityKind: string(kind), Rdev: fact.Rdev,
		Effect:       "Format this disk as ext4. Existing contents are destroyed.",
		Consequences: []string{"There is no undo.", "The by-id path is checked again immediately before mkfs.ext4 runs.", "A format that is killed is not retried."},
		Warnings:     []string{"ext4.read does not authorize this.", "The main broker does not gain device access. If this broker cannot see the disk, the format is refused."},
	}, nil
}

func (env *Env) formatExt4(ctx context.Context, byID string) (runner.Result, error) {
	argv, err := runner.RootArgv(runner.RootMkfsExt4, byID)
	if err != nil {
		return runner.Result{}, err
	}
	if env.Ext4Mkfs != nil {
		return env.Ext4Mkfs(ctx, argv)
	}
	if env.RootExec == nil {
		return runner.Result{}, &proto.Error{Code: proto.CodeForbidden, Message: "root execution is not available to this operation"}
	}
	return env.RootExec.Run(ctx, runner.RootMkfsExt4, byID)
}

const ext4Recovery = "Do not retry blindly. The disk may be partially formatted. Inspect it, then review a new plan."

func ext4FormatApply(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	var a ext4FormatApplyArgs
	if err := proto.DecodeArgs(raw, &a); err != nil {
		return nil, &proto.Error{Code: proto.CodeBadRequest, Message: "arguments must be the reviewed fields plus token"}
	}
	req := ext4FormatArgs{ByID: a.ByID, IdentityKind: a.IdentityKind, IdentityValue: a.IdentityValue, Confirm: a.Confirm}
	kind, value, err := ext4FormatRequest(req)
	if err != nil {
		return nil, err
	}
	if err := requireExt4Format(env); err != nil {
		return nil, err
	}
	target := ext4IdentityTarget(kind, value)
	b, err := env.Writes.Consume(a.Token, writes.Request{
		Op: ext4FormatOpName, Action: ext4FormatAction, Target: target,
		Actor: env.Actor.User, Session: env.Actor.Session, StepUpAt: env.Actor.StepUpAt,
	})
	switch {
	case errors.Is(err, writes.ErrStepUp):
		return nil, &proto.Error{Code: proto.CodeStepUpRequired, Message: "re-enter your password to confirm this format"}
	case err != nil:
		return nil, &proto.Error{Code: proto.CodeInvalidToken, Message: "the plan is invalid, expired or already used; review the format again"}
	}
	unlock, err := env.Writes.Lock(ext4FormatLock(target))
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeBusy, Message: "another ext4 format of this disk is in progress"}
	}
	defer unlock()
	tool, err := env.ext4Tooling()
	if err != nil {
		return nil, err
	}
	fact, err := env.ext4View().Inspect(ctx, env, a.ByID)
	if err != nil {
		return nil, err
	}
	dev := storagecap.DeviceRef{Kind: kind, Value: value}
	if err := ext4Gate(tool, dev, fact); err != nil {
		return nil, err
	}
	digest := ext4StateDigest(kind, value, a.ByID, fact.Rdev)
	if digest != b.StateDigest {
		return nil, &proto.Error{Code: proto.CodeConflict, Message: "the disk identity changed since the format was reviewed; nothing was formatted. Review it again."}
	}
	rec := writes.Record{Op: ext4FormatOpName, Action: ext4FormatAction, Target: target, Actor: env.Actor.User,
		RequestID: env.Actor.RequestID, TokenID: b.ID, Risk: b.Risk, State: b.StateDigest}
	opID, err := env.Writes.Intent(rec)
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "the operation journal cannot be written; nothing was formatted"}
	}
	rec.OpID = opID
	outcomeWritten := false
	defer func() {
		if p := recover(); p != nil {
			if !outcomeWritten {
				fail := rec
				fail.Outcome = OutcomeUncertain
				fail.Message = "The format helper failed while formatting; the result is unknown. It was not retried."
				_ = env.Writes.Outcome(fail)
			}
			panic(p)
		}
	}()
	// Re-check immediately before exec. A failure here has a journal intent
	// and must record an outcome. mkfs is not started.
	fact2, err2 := env.ext4View().Inspect(ctx, env, a.ByID)
	if err2 != nil || ext4Gate(tool, dev, fact2) != nil || ext4StateDigest(kind, value, a.ByID, fact2.Rdev) != digest {
		out := ext4Finish(opID, OutcomeFailed, "The disk was not eligible at the moment of format. Nothing was formatted.", true)
		rec.Outcome, rec.Message = out.Outcome, out.Message
		outcomeWritten = true
		_ = env.Writes.Outcome(rec)
		return out, nil
	}
	started := time.Now().UTC()
	actx, acancel := context.WithTimeout(context.Background(), ext4FormatTimeout)
	res, runErr := env.formatExt4(actx, a.ByID)
	acancel()
	after, afterErr := env.ext4View().Inspect(context.Background(), env, a.ByID)
	outcome, msg := classifyExt4Format(res, runErr, after, afterErr, fact.Rdev)
	out := Ext4FormatOutcome{OperationID: opID, Action: ext4FormatAction, Outcome: outcome, Message: msg,
		StartedAt: started, FinishedAt: time.Now().UTC()}
	if outcome != OutcomeSucceeded {
		out.Recovery = []string{ext4Recovery, "On the host, do not run mkfs again until the device identity and mounts have been checked."}
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

func ext4Finish(opID, outcome, msg string, recovery bool) Ext4FormatOutcome {
	out := Ext4FormatOutcome{OperationID: opID, Action: ext4FormatAction, Outcome: outcome, Message: msg,
		StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()}
	if recovery {
		out.Recovery = []string{ext4Recovery}
	}
	return out
}

func classifyExt4Format(res runner.Result, runErr error, after Ext4DeviceFact, afterErr error, rdev string) (string, string) {
	same := afterErr == nil && after.BlockDevice && after.Rdev == rdev && !after.Mounted && !after.HasHolders
	switch {
	case errors.Is(runErr, runner.ErrTimeout):
		return OutcomeUncertain, "mkfs.ext4 did not finish within " + ext4FormatTimeout.String() + ". The result is not verified. It was not retried."
	case runErr != nil:
		return OutcomeUncertain, "mkfs.ext4 could not be started or did not return. The result is not verified. It was not retried. (" + execClass(runErr) + ")"
	case !same:
		if res.ExitCode == 0 {
			return OutcomeUncertain, "mkfs.ext4 exited 0, but the device identity could not be read back, so the result is not verified."
		}
		return OutcomeUncertain, "mkfs.ext4 failed and the device identity could not be read back, so the result is not verified."
	case res.ExitCode != 0:
		return OutcomeUncertain, fmt.Sprintf("mkfs.ext4 exited with status %d. The disk may already have been written. Nothing further was run.", res.ExitCode)
	default:
		return OutcomeSucceeded, "mkfs.ext4 exited 0 and the device identity still matches the plan. The superblock was not read back."
	}
}

func execClass(err error) string {
	var ex *runner.ExecError
	if errors.As(err, &ex) && ex.Class != "" {
		return ex.Class
	}
	return "exec"
}
