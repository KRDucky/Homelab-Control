// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"context"
	"encoding/json"
	"errors"
	. "homelab-control/backend/internal/broker/types"
	"os"
	"regexp"
	"sort"
	"strings"

	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/runner"
)

const maxUnits = 5000

// unitTypes listed in M1. Device/slice/scope units are noise for admins.
const unitTypes = "service,socket,timer,mount,automount,path,target,swap"

var unitNameRE = regexp.MustCompile(`^[A-Za-z0-9:_.\\@-]{1,256}$`)
var stateRE = regexp.MustCompile(`^[a-z-]{0,32}$`)

type rawUnit struct {
	Unit        string `json:"unit"`
	Load        string `json:"load"`
	Active      string `json:"active"`
	Sub         string `json:"sub"`
	Description string `json:"description"`
}

// systemd 249 names the field vendor_preset, 255 names it preset.
type rawUnitFile struct {
	UnitFile     string  `json:"unit_file"`
	State        string  `json:"state"`
	Preset       *string `json:"preset"`
	VendorPreset *string `json:"vendor_preset"`
}

func systemdList(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	if err := requireNoArgs(raw); err != nil {
		return nil, err
	}
	if env.Runner == nil || !env.Runner.Available("systemctl") {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "systemctl is not installed; this host does not appear to use systemd"}
	}
	if fi, err := os.Stat(env.Roots.Path("/run/systemd/system")); err != nil || !fi.IsDir() {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "systemd is not running as the init system on this host"}
	}
	res := UnitList{Counts: map[string]int{}, Units: []Unit{}, Source: "systemctl list-units/list-unit-files --output=json (run as homelab-control)"}
	if v, err := env.Runner.Run(ctx, "systemctl", "--version"); err == nil {
		res.SystemdVersion = clean(strings.SplitN(string(v.Stdout), "\n", 2)[0], 80)
	}
	lu, err := env.Runner.Run(ctx, "systemctl", "list-units", "--all", "--output=json", "--no-pager", "--type="+unitTypes)
	if err != nil {
		return nil, runErr(err)
	}
	if lu.ExitCode != 0 {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "systemctl list-units failed: " + clean(lu.Stderr, 200)}
	}
	units, err := ParseListUnits(lu.Stdout)
	if err != nil {
		return nil, &proto.Error{Code: proto.CodeInternal, Message: "unexpected systemctl output: " + err.Error()}
	}
	files := map[string]rawUnitFile{}
	if uf, err := env.Runner.Run(ctx, "systemctl", "list-unit-files", "--output=json", "--no-pager", "--type="+unitTypes); err == nil && uf.ExitCode == 0 {
		files, _ = ParseListUnitFiles(uf.Stdout)
	}
	for i := range units {
		if f, ok := files[units[i].Name]; ok {
			units[i].UnitFileState = f.State
			if f.Preset != nil {
				units[i].Preset = *f.Preset
			} else if f.VendorPreset != nil {
				units[i].Preset = *f.VendorPreset
			}
		}
		res.Counts[units[i].Active]++
		if units[i].Active == "failed" {
			res.Counts["failed_total"]++
		}
	}
	if len(units) > maxUnits {
		units, res.Truncated = units[:maxUnits], true
	}
	res.Units = units
	return res, nil
}

func runErr(err error) error {
	switch {
	case errors.Is(err, runner.ErrTimeout):
		return &proto.Error{Code: proto.CodeTimeout, Message: "systemctl did not answer in time"}
	case errors.Is(err, runner.ErrTooLarge):
		return &proto.Error{Code: proto.CodeInternal, Message: "systemctl output exceeded the size limit"}
	default:
		msg := "systemctl could not be run"
		var ex *runner.ExecError
		if errors.As(err, &ex) && ex.Class != "" {
			msg += " (" + ex.Class + ")"
		}
		return &proto.Error{Code: proto.CodeInternal, Message: msg}
	}
}

// ParseListUnits validates and normalises `systemctl list-units --output=json`.
func ParseListUnits(b []byte) ([]Unit, error) {
	var raw []rawUnit
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]Unit, 0, len(raw))
	for _, r := range raw {
		name := strings.TrimSpace(r.Unit)
		if !unitNameRE.MatchString(name) {
			continue // drop anything that is not a well-formed unit name
		}
		dot := strings.LastIndexByte(name, '.')
		if dot < 0 {
			continue
		}
		u := Unit{Name: name, Type: name[dot+1:], Load: r.Load, Active: r.Active, Sub: r.Sub, Description: clean(r.Description, 256)}
		if !stateRE.MatchString(u.Load) || !stateRE.MatchString(u.Active) || !stateRE.MatchString(u.Sub) {
			continue
		}
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ParseListUnitFiles validates `systemctl list-unit-files --output=json`.
func ParseListUnitFiles(b []byte) (map[string]rawUnitFile, error) {
	var raw []rawUnitFile
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make(map[string]rawUnitFile, len(raw))
	for _, r := range raw {
		if unitNameRE.MatchString(r.UnitFile) && stateRE.MatchString(r.State) {
			out[r.UnitFile] = r
		}
	}
	return out, nil
}
