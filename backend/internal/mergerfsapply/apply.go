// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package mergerfsapply validates MergerFS and SnapRAID write requests and
// builds the closed argv the helper may execute. It does not import os/exec.
// hc-api may import it. The helper re-validates before it runs anything.
package mergerfsapply

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	ActionMount   = "mount"
	ActionPersist = "persist"
	ActionSync    = "sync"
	ActionScrub   = "scrub"
	ActionFix     = "fix"
	// ActionStatus is read-only: snapraid status. It is here, in the root
	// helper, because SnapRAID takes its lock file (next to the first content
	// file) for status too, and content files are root-only.
	ActionStatus = "status"

	MergerFSBin = "/usr/bin/mergerfs"
	SnapRAIDBin = "/usr/bin/snapraid"

	ProtocolVersion = 1

	maxBranches = 16
	maxParity   = 6
	maxContent  = 6
	maxFiles    = 8

	// MaxStatusOutput caps the status text the helper returns.
	MaxStatusOutput = 48 << 10
)

// Request is the closed helper request. Clients do not choose the binary,
// a configuration path, or extra arguments.
type Request struct {
	Version      int      `json:"version"`
	Action       string   `json:"action"`
	Mountpoint   string   `json:"mountpoint,omitempty"`
	Branches     []string `json:"branches,omitempty"`
	Policy       string   `json:"policy,omitempty"`
	MinFree      string   `json:"minFree,omitempty"`
	Parity       []string `json:"parity,omitempty"`
	Content      []string `json:"content,omitempty"`
	ScrubPercent int      `json:"scrubPercent,omitempty"`
	Files        []string `json:"files,omitempty"`
}

// Result is the helper's typed reply. Summary is a fixed sentence. It must
// not contain a path.
type Result struct {
	Version  int    `json:"version"`
	OK       bool   `json:"ok"`
	ExitCode int    `json:"exitCode"`
	Summary  string `json:"summary"`
	Blocked  string `json:"blocked,omitempty"`
	// Output is set only for ActionStatus: snapraid's own text, which can name
	// paths. The broker keeps recognised lines and redacts paths.
	Output string `json:"output,omitempty"`
	// OutputTruncated: Output is a prefix cut at MaxStatusOutput.
	OutputTruncated bool `json:"outputTruncated,omitempty"`
	// Running: sync, scrub or fix was still running when the helper replied.
	// It continues; its exit status goes to the journal, not to this reply.
	Running bool `json:"running,omitempty"`
	// Checking: with Running, snapraid diff (the check before sync or scrub)
	// had not finished when the helper replied. It continues, and sync or
	// scrub starts only if it passes; either result goes to the journal.
	Checking bool `json:"checking,omitempty"`
	// LastRun is set only for ActionStatus: the latest sync, scrub or fix.
	LastRun *RunRecord `json:"lastRun,omitempty"`
}

// Spec is a request that passed Validate.
type Spec struct {
	Action       string
	Mountpoint   string
	Branches     []string
	Policy       string
	MinFree      string
	Parity       []string
	Content      []string
	ScrubPercent int
	Files        []string
}

var (
	mountRE   = regexp.MustCompile(`^/mnt/[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	branchRE  = regexp.MustCompile(`^/mnt/[A-Za-z0-9][A-Za-z0-9._/-]{0,200}$`)
	fileRE    = regexp.MustCompile(`^/mnt/[A-Za-z0-9][A-Za-z0-9._-]{0,63}/[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	contentRE = regexp.MustCompile(`^/var/snapraid/[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	relRE     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,199}$`)
	minFreeRE = regexp.MustCompile(`^[1-9][0-9]{0,5}[KMGT]$`)
	policies  = map[string]bool{"epmfs": true, "mfs": true, "ff": true, "lfs": true, "pfrd": true, "rand": true}
)

// Validate checks one closed action. Pool create does not format disks.
// Branches are existing directories. Parity must not sit on a branch or on
// the pool mount. Fix is snapraid fix -m -f only.
func Validate(req Request) (Spec, error) {
	if req.Version != 0 && req.Version != ProtocolVersion {
		return Spec{}, errors.New("unsupported request version")
	}
	switch req.Action {
	case ActionMount, ActionPersist:
		return validatePool(req)
	case ActionSync:
		if extra(req, false) || len(req.Files) != 0 || req.ScrubPercent != 0 {
			return Spec{}, errors.New("sync takes no path, percent, or file")
		}
		return Spec{Action: ActionSync}, nil
	case ActionScrub:
		if extra(req, false) || len(req.Files) != 0 {
			return Spec{}, errors.New("scrub takes only an optional percent")
		}
		if req.ScrubPercent < 0 || req.ScrubPercent > 100 {
			return Spec{}, errors.New("scrub percent must be from 1 to 100")
		}
		return Spec{Action: ActionScrub, ScrubPercent: req.ScrubPercent}, nil
	case ActionStatus:
		if extra(req, false) || len(req.Files) != 0 || req.ScrubPercent != 0 {
			return Spec{}, errors.New("status takes no argument")
		}
		return Spec{Action: ActionStatus}, nil
	case ActionFix:
		if req.Mountpoint != "" || len(req.Branches) != 0 || req.Policy != "" || req.MinFree != "" || len(req.Parity) != 0 || len(req.Content) != 0 || req.ScrubPercent != 0 {
			return Spec{}, errors.New("recovery takes only relative file paths")
		}
		files, err := relativeFiles(req.Files)
		if err != nil {
			return Spec{}, err
		}
		return Spec{Action: ActionFix, Files: files}, nil
	default:
		return Spec{}, errors.New("unknown action")
	}
}

func extra(req Request, allowPool bool) bool {
	if allowPool {
		return false
	}
	return req.Mountpoint != "" || len(req.Branches) != 0 || req.Policy != "" || req.MinFree != "" || len(req.Parity) != 0 || len(req.Content) != 0
}

func validatePool(req Request) (Spec, error) {
	if req.ScrubPercent != 0 || len(req.Files) != 0 {
		return Spec{}, errors.New("pool actions do not take a scrub percent or recovery path")
	}
	if !mountRE.MatchString(req.Mountpoint) || badPath(req.Mountpoint) {
		return Spec{}, errors.New("mountpoint must be one directory under /mnt")
	}
	if len(req.Branches) < 2 || len(req.Branches) > maxBranches {
		return Spec{}, errors.New("a pool needs 2 to 16 branches")
	}
	if !policies[req.Policy] {
		return Spec{}, errors.New("policy must be one of epmfs, mfs, ff, lfs, pfrd, rand")
	}
	if req.MinFree != "" && !minFreeRE.MatchString(req.MinFree) {
		return Spec{}, errors.New("min free space must look like 20G")
	}
	seen := map[string]bool{}
	branches := make([]string, 0, len(req.Branches))
	for _, b := range req.Branches {
		if !branchRE.MatchString(b) || badPath(b) || b == req.Mountpoint || seen[b] {
			return Spec{}, errors.New("each branch must be a distinct directory under /mnt and not the mountpoint")
		}
		seen[b] = true
		branches = append(branches, b)
	}
	spec := Spec{Action: req.Action, Mountpoint: req.Mountpoint, Branches: branches, Policy: req.Policy, MinFree: req.MinFree}
	if req.Action == ActionMount {
		if len(req.Parity) != 0 || len(req.Content) != 0 {
			return Spec{}, errors.New("mount does not write snapraid.conf; use persist")
		}
		return spec, nil
	}
	parity, err := parityFiles(req.Parity, spec)
	if err != nil {
		return Spec{}, err
	}
	content, err := contentFiles(req.Content, spec, parity)
	if err != nil {
		return Spec{}, err
	}
	spec.Parity = parity
	spec.Content = content
	return spec, nil
}

func parityFiles(in []string, spec Spec) ([]string, error) {
	if len(in) < 1 || len(in) > maxParity {
		return nil, errors.New("persist needs 1 to 6 parity files")
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, p := range in {
		if !fileRE.MatchString(p) || badPath(p) || seen[p] || strings.HasPrefix(p, spec.Mountpoint+"/") {
			return nil, errors.New("parity must be a file under /mnt and not on the pool")
		}
		for _, b := range spec.Branches {
			if p == b || strings.HasPrefix(p, b+"/") {
				return nil, errors.New("parity must not sit on a mergerfs branch")
			}
		}
		seen[p] = true
		out = append(out, p)
	}
	return out, nil
}

func contentFiles(in []string, spec Spec, parity []string) ([]string, error) {
	if len(in) < 2 || len(in) > maxContent {
		return nil, errors.New("persist needs 2 to 6 content files")
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, c := range in {
		ok := contentRE.MatchString(c) || fileRE.MatchString(c)
		if !ok || badPath(c) || seen[c] || c == spec.Mountpoint || strings.HasPrefix(c, spec.Mountpoint+"/") {
			return nil, errors.New("content must be under /var/snapraid or a file under /mnt, not on the pool")
		}
		for _, p := range parity {
			dir := p[:strings.LastIndex(p, "/")]
			if c == p || strings.HasPrefix(c, dir+"/") {
				return nil, errors.New("content must not sit on a parity path")
			}
		}
		seen[c] = true
		out = append(out, c)
	}
	return out, nil
}

func relativeFiles(in []string) ([]string, error) {
	if len(in) < 1 || len(in) > maxFiles {
		return nil, errors.New("recovery needs 1 to 8 relative paths")
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, f := range in {
		if !relRE.MatchString(f) || badPath(f) || strings.HasPrefix(f, "/") || seen[f] {
			return nil, errors.New("each recovery path must be relative, with no ..")
		}
		seen[f] = true
		out = append(out, f)
	}
	return out, nil
}

func badPath(p string) bool {
	if strings.Contains(p, `\`) || strings.Contains(p, "..") || strings.Contains(p, "//") || strings.Contains(p, ":") || strings.Contains(p, ",") {
		return true
	}
	return strings.HasSuffix(p, "/") || strings.Contains(p, " ")
}

// Target is the lock and token target. It is not a filesystem path.
func (s Spec) Target() string {
	switch s.Action {
	case ActionMount:
		return "pool"
	case ActionPersist:
		return "array"
	case ActionSync:
		return "sync"
	case ActionScrub:
		return "scrub"
	case ActionFix:
		return "recover"
	default:
		return ""
	}
}

// Digest binds a plan token to the validated request.
func (s Spec) Digest() string {
	b, _ := json.Marshal(s)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Argv is the only command the helper may run for this spec.
// Fix is exactly snapraid fix -m -f REL. It never adds -e.
func (s Spec) Argv() ([]string, error) {
	switch s.Action {
	case ActionMount:
		opt := "category.create=" + s.Policy + ",allow_other"
		if s.MinFree != "" {
			opt += ",minfreespace=" + s.MinFree
		}
		return []string{MergerFSBin, "-o", opt, strings.Join(s.Branches, ":"), s.Mountpoint}, nil
	case ActionSync:
		return []string{SnapRAIDBin, "sync"}, nil
	case ActionStatus:
		return []string{SnapRAIDBin, "status"}, nil
	case ActionScrub:
		if s.ScrubPercent == 0 {
			return []string{SnapRAIDBin, "scrub"}, nil
		}
		return []string{SnapRAIDBin, "scrub", "-p", strconv.Itoa(s.ScrubPercent)}, nil
	case ActionFix:
		argv := []string{SnapRAIDBin, "fix", "-m"}
		for _, f := range s.Files {
			// SnapRAID -f is disk-root relative. The stored path has no leading
			// slash, so the filter cannot be a host absolute path.
			argv = append(argv, "-f", "/"+f)
		}
		return argv, nil
	case ActionPersist:
		return nil, nil
	default:
		return nil, fmt.Errorf("no argv for %s", s.Action)
	}
}

// Effect is the review sentence. It does not include a filesystem path.
func (s Spec) Effect() string {
	switch s.Action {
	case ActionMount:
		return fmt.Sprintf("Mount %d existing directories with policy %s. Disks are not formatted.", len(s.Branches), s.Policy)
	case ActionPersist:
		return fmt.Sprintf("Write the fstab line and snapraid.conf for %d branches and %d parity files, after a backup of each file.", len(s.Branches), len(s.Parity))
	case ActionSync:
		return "Run snapraid sync only if diff shows no missing disk and no removed files."
	case ActionScrub:
		if s.ScrubPercent == 0 {
			return "Run snapraid scrub only if diff shows no missing disk and no removed files."
		}
		return fmt.Sprintf("Run snapraid scrub -p %d only if diff shows no missing disk and no removed files.", s.ScrubPercent)
	case ActionFix:
		return fmt.Sprintf("Run snapraid fix -m -f for %d relative path(s). This is not a full fix and it does not use -e.", len(s.Files))
	default:
		return ""
	}
}

// Consequences are the fixed warnings shown before confirm.
func (s Spec) Consequences() []string {
	common := []string{
		"The exact command is in this review. There is no extra argument and no --force.",
		"Pooling is not RAID. Writes between syncs are not protected.",
	}
	switch s.Action {
	case ActionMount:
		return append(common,
			"This does not format disks and does not write fstab. The mount is lost on reboot until you save the array definition.",
			"mergerfs is started in its own systemd scope so the mount remains after the helper exits.",
		)
	case ActionPersist:
		return append(common,
			"The previous fstab and snapraid.conf are copied to fixed backup names first.",
			"An existing snapraid.conf that Homelab Control did not write is not replaced, and nothing is saved.",
			"An fstab line for this mountpoint that is not fuse.mergerfs is refused.",
			"Parity is not placed on a branch. Content files are not placed on the pool or on parity.",
		)
	case ActionSync, ActionScrub:
		return append(common,
			"A missing disk or a removed file blocks this operation. Nothing is synced or scrubbed.",
			"There is no way to continue past that block. Restore the disk or the files first.",
			"On a large array that check can outlast the reply. It then continues in the background, and the block or the run's result is recorded in the system journal.",
			"A run longer than a few seconds continues in the background. SnapRAID status shows the array as locked until it ends.",
		)
	case ActionFix:
		return append(common,
			"The command is snapraid fix -m -f for the reviewed relative paths only.",
			"-e is not used. A full unfiltered fix is not used.",
			"A missing data disk does not block this recovery. Sync and scrub still refuse a missing disk.",
			"A run longer than a few seconds continues in the background. SnapRAID status shows the array as locked until it ends.",
		)
	default:
		return common
	}
}

// AuditDetail is safe to write to the audit log: counts and the action, never a path.
func (s Spec) AuditDetail() string {
	switch s.Action {
	case ActionMount:
		return fmt.Sprintf("action=mount branches=%d", len(s.Branches))
	case ActionPersist:
		return fmt.Sprintf("action=persist branches=%d parity=%d content=%d", len(s.Branches), len(s.Parity), len(s.Content))
	case ActionScrub:
		return fmt.Sprintf("action=scrub percent=%d", s.ScrubPercent)
	case ActionFix:
		return fmt.Sprintf("action=fix files=%d", len(s.Files))
	default:
		return "action=" + s.Action
	}
}
