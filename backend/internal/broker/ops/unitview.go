// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package ops

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"homelab-control/backend/internal/broker/proto"
	. "homelab-control/backend/internal/broker/types"
)

// Read-only unit views (M2-3): unit detail, unit-file contents, timers and
// sockets. Every child runs unprivileged (as homelab-control) with a fixed
// argv; unit names are validated twice and must be in systemd's inventory
// before they reach a per-unit query. Properties are allow-listed: no
// Environment=, Exec* command lines or credentials are ever requested.

type unitArgs struct {
	Unit string `json:"unit"`
}

// ViewUnitRE is the grammar for read-only unit queries: the listed unit types,
// templates allowed ("name@.service"), no leading '-', escapes or separators.
var viewUnitRE = LogsUnitRE

func validViewUnit(u string) bool { return viewUnitRE.MatchString(u) && !strings.Contains(u, "..") }

func viewAuditArgs(raw json.RawMessage) string {
	var a unitArgs
	if proto.DecodeArgs(raw, &a) != nil || !validViewUnit(a.Unit) {
		return "invalid args"
	}
	return "unit=" + a.Unit
}

func decodeUnitArgs(raw json.RawMessage) (string, error) {
	var a unitArgs
	if err := proto.DecodeArgs(raw, &a); err != nil {
		return "", &proto.Error{Code: proto.CodeBadRequest, Message: "arguments must be {\"unit\": string}"}
	}
	if !validViewUnit(a.Unit) {
		return "", &proto.Error{Code: proto.CodeBadRequest, Message: "unit name is not valid"}
	}
	return a.Unit, nil
}

func requireSystemctl(env *Env) error {
	if env.Runner == nil || !env.Runner.Available("systemctl") {
		return &proto.Error{Code: proto.CodeUnavailable, Message: "systemctl is not installed; this host does not appear to use systemd"}
	}
	if !systemdBooted(env) {
		return &proto.Error{Code: proto.CodeUnavailable, Message: "systemd is not running as the init system on this host"}
	}
	return nil
}

// membership looks the literal name up in the runtime list and in the
// installed unit files. The grammar excludes glob characters, so the pattern
// argument matches only this name.
type membership struct {
	runtime  *Unit
	fileOnly *rawUnitFile
}

func lookup(ctx context.Context, env *Env, unit string) (membership, error) {
	var m membership
	lu, err := env.Runner.Run(ctx, "systemctl", "list-units", "--all", "--output=json", "--no-pager", "--", unit)
	if err != nil {
		return m, runErr(err)
	}
	if lu.ExitCode == 0 {
		if us, err := ParseListUnits(lu.Stdout); err == nil {
			for i := range us {
				if us[i].Name == unit {
					m.runtime = &us[i]
				}
			}
		}
	}
	uf, err := env.Runner.Run(ctx, "systemctl", "list-unit-files", "--output=json", "--no-pager", "--", unit)
	if err != nil {
		return m, runErr(err)
	}
	if uf.ExitCode == 0 {
		if fs, err := ParseListUnitFiles(uf.Stdout); err == nil {
			if f, ok := fs[unit]; ok {
				m.fileOnly = &f
			}
		}
	}
	if m.runtime == nil && m.fileOnly == nil {
		return m, &proto.Error{Code: proto.CodeNotFound, Message: "this unit is not in systemd's unit list or installed unit files"}
	}
	return m, nil
}

// detailProps is the fixed property list of services.unit.detail. Reviewed:
// identity, state, unit-file origin, restart counters, last exit and
// dependency names only.
var detailProps = []string{
	"Id", "Names", "Description", "LoadState", "ActiveState", "SubState", "Result",
	"UnitFileState", "UnitFilePreset", "FragmentPath", "SourcePath", "DropInPaths", "NeedDaemonReload", "Transient",
	"Type", "Restart", "NRestarts", "MainPID", "ExecMainCode", "ExecMainStatus", "ExecMainStartTimestamp", "ExecMainExitTimestamp",
	"ActiveEnterTimestamp", "InactiveEnterTimestamp", "StateChangeTimestamp",
	"Requires", "Requisite", "Wants", "BindsTo", "PartOf", "Upholds", "RequiredBy", "RequisiteOf", "WantedBy",
	"BoundBy", "UpheldBy", "ConsistsOf", "Conflicts", "ConflictedBy", "Before", "After", "Triggers", "TriggeredBy",
	"OnFailure", "OnSuccess",
}

var detailDeps = []string{"Requires", "Requisite", "Wants", "BindsTo", "PartOf", "Upholds", "RequiredBy", "RequisiteOf",
	"WantedBy", "BoundBy", "UpheldBy", "ConsistsOf", "Conflicts", "ConflictedBy", "Before", "After", "Triggers", "TriggeredBy",
	"OnFailure", "OnSuccess"}

// DetailShowArgs is the argv of the detail query.
func DetailShowArgs(units ...string) []string {
	a := []string{"show", "--no-pager"}
	for _, p := range detailProps {
		a = append(a, "--property="+p)
	}
	return append(append(a, "--"), units...)
}

// ParseShowBlocks parses (multi-unit) `systemctl show` output into one map per
// block. Repeated keys (TimersCalendar=, TimersMonotonic=) keep every value.
func ParseShowBlocks(b []byte) []map[string][]string {
	var out []map[string][]string
	cur := map[string][]string{}
	flush := func() {
		if len(cur) > 0 {
			out = append(out, cur)
		}
		cur = map[string][]string{}
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r", ""), "\n") {
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || len(cur[k]) >= 32 {
			continue
		}
		cur[k] = append(cur[k], v)
	}
	flush()
	return out
}

func first(m map[string][]string, k string) string {
	if v := m[k]; len(v) > 0 {
		return strings.TrimSpace(v[0])
	}
	return ""
}

// timestamp keeps systemd's own text when it is set ("" or "n/a" = never).
func timestamp(v string) string {
	if v == "" || v == "n/a" || v == "0" {
		return ""
	}
	return clean(v, 64)
}

var exitCodes = map[string]string{"1": "exited", "2": "killed", "3": "dumped core"}

func unitDetail(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	unit, err := decodeUnitArgs(raw)
	if err != nil {
		return nil, err
	}
	if err := requireSystemctl(env); err != nil {
		return nil, err
	}
	m, err := lookup(ctx, env, unit)
	if err != nil {
		return nil, err
	}
	d := UnitDetail{Requested: unit, Kind: "unit", InRuntime: m.runtime != nil,
		Source: "systemctl show with a fixed property list (run as homelab-control)", Dependencies: map[string][]string{}}
	if m.fileOnly != nil {
		d.BootState = m.fileOnly.State
	}
	if strings.Contains(unit, "@.") {
		// Uninstantiated template: systemd cannot report runtime state for it.
		d.Kind = "template"
		d.Notes = append(d.Notes, "This is a template. It has no runtime state of its own; its instances (name@instance) are separate units.")
		d.Source = "systemctl list-unit-files (run as homelab-control)"
		return d, nil
	}
	res, err := env.Runner.Run(ctx, "systemctl", DetailShowArgs(unit)...)
	if err != nil {
		return nil, runErr(err)
	}
	if res.ExitCode != 0 {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "systemctl show failed: " + clean(res.Stderr, 200)}
	}
	blocks := ParseShowBlocks(res.Stdout)
	if len(blocks) != 1 || !unitNameRE.MatchString(first(blocks[0], "Id")) {
		return nil, &proto.Error{Code: proto.CodeInternal, Message: "unexpected systemctl show output"}
	}
	if id := first(blocks[0], "Id"); id != unit {
		// The installed state of the canonical unit, not of the alias link.
		if mc, err := lookup(ctx, env, id); err == nil && mc.fileOnly != nil {
			canon := mc.fileOnly.State
			return BuildDetail(d, blocks[0], &canon), nil
		}
	}
	return BuildDetail(d, blocks[0], nil), nil
}

// BuildDetail fills d from one `systemctl show` block of DetailShowArgs.
// canonicalBootState, when set, is the installed state of the canonical unit
// (the requested name was an alias).
func BuildDetail(d UnitDetail, b map[string][]string, canonicalBootState *string) UnitDetail {
	if d.Dependencies == nil {
		d.Dependencies = map[string][]string{}
	}
	d.ID = first(b, "Id")
	unit := d.Requested
	d.Names = unitList(first(b, "Names"))
	if d.ID != unit {
		d.Kind = "alias"
		d.Notes = append(d.Notes, unit+" is an alias of "+d.ID+".")
		if canonicalBootState != nil {
			d.BootState = *canonicalBootState
		}
	}
	d.Description = clean(first(b, "Description"), 256)
	d.LoadState, d.ActiveState, d.SubState, d.Result = state(first(b, "LoadState")), state(first(b, "ActiveState")), state(first(b, "SubState")), state(first(b, "Result"))
	d.UnitFileState, d.Preset = state(first(b, "UnitFileState")), state(first(b, "UnitFilePreset"))
	d.FragmentPath, d.SourcePath = clean(first(b, "FragmentPath"), 512), clean(first(b, "SourcePath"), 512)
	for _, p := range strings.Fields(first(b, "DropInPaths")) {
		if len(d.DropInPaths) < 32 {
			d.DropInPaths = append(d.DropInPaths, clean(p, 512))
		}
	}
	d.NeedDaemonReload = first(b, "NeedDaemonReload") == "yes"
	d.Transient = first(b, "Transient") == "yes"
	d.Generated = strings.HasPrefix(d.FragmentPath, "/run/systemd/generator") || d.SourcePath != ""
	d.ServiceType = state(first(b, "Type"))
	d.Restart = state(first(b, "Restart"))
	if pid, err := strconv.Atoi(first(b, "MainPID")); err == nil && pid > 0 {
		d.MainPID = pid
	}
	d.ActiveSince, d.InactiveSince, d.StateSince = timestamp(first(b, "ActiveEnterTimestamp")), timestamp(first(b, "InactiveEnterTimestamp")), timestamp(first(b, "StateChangeTimestamp"))
	if strings.HasSuffix(d.ID, ".service") && d.LoadState == "loaded" {
		if n, err := strconv.Atoi(first(b, "NRestarts")); err == nil {
			d.Restarts = &n
		}
		started, exited := timestamp(first(b, "ExecMainStartTimestamp")), timestamp(first(b, "ExecMainExitTimestamp"))
		switch {
		case exited != "":
			st, _ := strconv.Atoi(first(b, "ExecMainStatus"))
			d.LastExit = &UnitExit{Code: exitCodes[first(b, "ExecMainCode")], Status: st, ExitedAt: exited, StartedAt: started}
			if d.LastExit.Code == "" {
				d.LastExit.Code = "unknown"
			}
		case started != "":
			d.Notes = append(d.Notes, "The main process has not exited since it was last started.")
		default:
			d.Notes = append(d.Notes, "systemd has no record of the main process running since the manager started.")
		}
	}
	for _, k := range detailDeps {
		l := unitList(first(b, k))
		if len(l) > 64 {
			l, d.DependenciesTruncated = l[:64], true
		}
		if len(l) > 0 {
			d.Dependencies[k] = l
		}
	}
	switch {
	case d.LoadState == "not-found":
		d.Notes = append(d.Notes, "systemd knows the name (something refers to it) but there is no unit file for it.")
	case d.LoadState == "masked":
		d.Notes = append(d.Notes, "The unit is masked: it cannot be started until it is unmasked on the host.")
	case !d.InRuntime && d.Kind == "unit" && d.LoadState == "loaded":
		d.LoadedForQuery = true
		d.Notes = append(d.Notes, "This unit is installed but was not loaded; systemd loaded its definition only to answer this query. Nothing was changed.")
	}
	if d.Generated {
		d.Notes = append(d.Notes, "This unit is generated (by a systemd generator or from another configuration source), not written as a unit file.")
	}
	if d.Transient {
		d.Notes = append(d.Notes, "This is a transient unit created at runtime.")
	}
	if d.NeedDaemonReload {
		d.Notes = append(d.Notes, "systemd's loaded copy differs from the files on disk (NeedDaemonReload=yes); the dependencies shown are those systemd has loaded.")
	}
	return d
}

// Unit-file contents (services.unit.contents) ------------------------------

const (
	maxContentBytes = 64 << 10
	maxContentLines = 2000
)

var (
	execKeyRE     = regexp.MustCompile(`^Exec[A-Za-z]*$`)
	secretKeyRE   = regexp.MustCompile(`(?i)(passw|passphrase|secret|token|apikey|api_key|privatekey|private_key|credential)`)
	secretFlagRE  = regexp.MustCompile(`(?i)^(-{1,2}[a-z0-9_.-]*(pass|secret|token|apikey|api-key|api_key|auth|key)[a-z0-9_.-]*)=(.+)$`)
	secretBareRE  = regexp.MustCompile(`(?i)^-{1,2}[a-z0-9_.-]*(pass|secret|token|apikey|api-key|api_key)[a-z0-9_.-]*$`)
	secretAssignE = regexp.MustCompile(`(?i)^([a-z0-9_.-]*(pass|secret|token|apikey|api_key)[a-z0-9_.-]*)=(.+)$`)
	userinfoRE    = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/\s@]+@`)
	envNameRE     = regexp.MustCompile(`(?:^|[\s"'])([A-Za-z_][A-Za-z0-9_]*)=`)
	// systemd ≥ 258: `Failed to open "/path": …`; ≤ 255: `Failed to cat /path: …` (probe 6).
	failedOpenRE = regexp.MustCompile(`^Failed to (?:open "(/[^"]{1,500})"|cat (/[^\s:]{1,500})): `)
)

const redacted = "[redacted]"

// redactLine removes values that commonly hold secrets. It is heuristic and
// conservative (it may hide harmless values); the UI says so.
func redactLine(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if t == "" || t[0] == '#' || t[0] == ';' || t[0] == '[' {
		return line, false
	}
	changed := false
	out := userinfoRE.ReplaceAllString(line, "${1}"+redacted+"@")
	if out != line {
		changed = true
	}
	k, v, ok := strings.Cut(out, "=")
	if !ok {
		return out, changed
	}
	key := strings.TrimSpace(k)
	switch {
	case key == "Environment":
		var names []string
		for _, m := range envNameRE.FindAllStringSubmatch(v, 32) {
			names = append(names, m[1]+"="+redacted)
		}
		if len(names) == 0 {
			names = []string{redacted}
		}
		return k + "=" + strings.Join(names, " "), true
	case strings.HasPrefix(key, "SetCredential") || secretKeyRE.MatchString(key):
		return k + "=" + redacted, true
	case execKeyRE.MatchString(key):
		fields := strings.Fields(v)
		for i := 0; i < len(fields); i++ {
			f := fields[i]
			if m := secretFlagRE.FindStringSubmatch(f); m != nil {
				fields[i], changed = m[1]+"="+redacted, true
			} else if m := secretAssignE.FindStringSubmatch(f); m != nil {
				fields[i], changed = m[1]+"="+redacted, true
			} else if secretBareRE.MatchString(f) && i+1 < len(fields) {
				fields[i+1], changed = redacted, true
				i++
			}
		}
		if changed {
			return k + "=" + strings.Join(fields, " "), true
		}
	}
	return out, changed
}

func unitContents(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	unit, err := decodeUnitArgs(raw)
	if err != nil {
		return nil, err
	}
	if err := requireSystemctl(env); err != nil {
		return nil, err
	}
	if _, err := lookup(ctx, env, unit); err != nil {
		return nil, err
	}
	res, err := env.Runner.Run(ctx, "systemctl", "cat", "--no-pager", "--", unit)
	if err != nil {
		return nil, runErr(err)
	}
	return ParseCat(unit, res.Stdout, res.Stderr, res.ExitCode)
}

// ParseCat turns `systemctl cat` output into redacted, bounded file texts.
func ParseCat(unit string, stdout []byte, stderr string, exitCode int) (UnitContents, error) {
	res := struct {
		Stdout   []byte
		Stderr   string
		ExitCode int
	}{stdout, stderr, exitCode}
	c := UnitContents{Unit: unit, Files: []UnitFileText{},
		Source: "systemctl cat (run as homelab-control: files only root can read are not shown); values that look like secrets are redacted"}
	for _, l := range strings.Split(res.Stderr, "\n") {
		if m := failedOpenRE.FindStringSubmatch(strings.TrimSpace(l)); m != nil && len(c.Unreadable) < 32 {
			c.Unreadable = append(c.Unreadable, clean(m[1]+m[2], 500))
		}
	}
	if res.ExitCode != 0 && len(res.Stdout) == 0 {
		if strings.Contains(res.Stderr, "No files found") {
			return c, &proto.Error{Code: proto.CodeNotFound, Message: "systemd has no unit file for this unit"}
		}
		if len(c.Unreadable) == 0 {
			return c, &proto.Error{Code: proto.CodeUnavailable, Message: "systemctl cat failed: " + clean(res.Stderr, 200)}
		}
	}
	var cur *UnitFileText
	var b strings.Builder
	bytes, lines := 0, 0
	flush := func() {
		if cur != nil {
			cur.Text = strings.TrimRight(b.String(), "\n")
			c.Files = append(c.Files, *cur)
		}
		b.Reset()
	}
	for _, raw := range strings.Split(string(res.Stdout), "\n") {
		line := strings.Map(func(r rune) rune {
			if r == '\t' {
				return r
			}
			if r < 0x20 || r == 0x7f {
				return -1
			}
			return r
		}, raw)
		if p, ok := strings.CutPrefix(line, "# /"); ok && !strings.ContainsAny(p, " \t") {
			flush()
			cur = &UnitFileText{Path: clean("/"+p, 500)}
			continue
		}
		if cur == nil {
			// systemctl's own warnings before the first file ("# Warning: ...").
			if t := strings.TrimSpace(strings.TrimPrefix(line, "#")); t != "" && len(c.Notes) < 8 {
				c.Notes = append(c.Notes, clean(t, 300))
			}
			continue
		}
		if bytes+len(line) > maxContentBytes || lines >= maxContentLines {
			c.Truncated = true
			break
		}
		red, did := redactLine(line)
		if did {
			c.Redactions++
		}
		b.WriteString(red + "\n")
		bytes += len(red) + 1
		lines++
	}
	flush()
	listed := map[string]bool{}
	for i := range c.Files {
		listed[c.Files[i].Path] = true
		if c.Files[i].Text == "" && contains(c.Unreadable, c.Files[i].Path) {
			c.Files[i].Unreadable = true
		}
	}
	// Older systemctl prints no header for a file it cannot read: list it too,
	// so the reader sees that part of the unit is not shown.
	for _, p := range c.Unreadable {
		if !listed[p] {
			c.Files = append(c.Files, UnitFileText{Path: p, Unreadable: true})
		}
	}
	if len(c.Files) > 32 {
		c.Files, c.Truncated = c.Files[:32], true
	}
	return c, nil
}

// Timers and sockets ----------------------------------------------------------

const maxListed = 500

type rawTimer struct {
	Next      *int64  `json:"next"`
	Last      *int64  `json:"last"`
	Unit      string  `json:"unit"`
	Activates *string `json:"activates"`
}

type rawSocket struct {
	Listen    string  `json:"listen"`
	Unit      string  `json:"unit"`
	Activates *string `json:"activates"`
}

func usecTime(p *int64) *time.Time {
	if p == nil || *p <= 0 {
		return nil
	}
	t := time.UnixMicro(*p).UTC()
	return &t
}

// schedule extracts "OnCalendar=…" or "OnBootUSec=…" from systemd's
// "{ OnCalendar=*-*-* 00:00:00 ; next_elapse=… }" property syntax.
func schedule(v string) string {
	v = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(v), "{"), "}"))
	s, _, _ := strings.Cut(v, ";")
	return clean(s, 160)
}

// stateOf batches `systemctl show` of fixed state properties for listed units.
func stateOf(ctx context.Context, env *Env, names []string, props []string) map[string]map[string][]string {
	out := map[string]map[string][]string{}
	var q []string
	for _, n := range names {
		if inspectable(n) {
			q = append(q, n)
		}
	}
	for i := 0; i < len(q); i += showBatch {
		batch := q[i:min(i+showBatch, len(q))]
		args := []string{"show", "--no-pager"}
		for _, p := range props {
			args = append(args, "--property="+p)
		}
		args = append(append(args, "--"), batch...)
		res, err := env.Runner.Run(ctx, "systemctl", args...)
		if err != nil || res.ExitCode != 0 {
			continue // state stays unknown; never invented
		}
		for _, blk := range ParseShowBlocks(res.Stdout) {
			if id := first(blk, "Id"); id != "" {
				out[id] = blk
			}
		}
	}
	return out
}

var timerProps = []string{"Id", "Description", "ActiveState", "SubState", "UnitFileState", "Persistent", "TimersCalendar", "TimersMonotonic"}

func timersList(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	if err := requireNoArgs(raw); err != nil {
		return nil, err
	}
	if err := requireSystemctl(env); err != nil {
		return nil, err
	}
	res, err := env.Runner.Run(ctx, "systemctl", "list-timers", "--all", "--output=json", "--no-pager")
	if err != nil {
		return nil, runErr(err)
	}
	if res.ExitCode != 0 {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "systemctl list-timers failed: " + clean(res.Stderr, 200)}
	}
	out, names, err := ParseTimerRows(res.Stdout)
	if err != nil {
		return nil, err
	}
	ApplyTimerStates(&out, stateOf(ctx, env, names, timerProps))
	return out, nil
}

// ParseTimerRows parses `systemctl list-timers --all --output=json`.
func ParseTimerRows(b []byte) (TimerList, []string, error) {
	res := struct{ Stdout []byte }{b}
	var rows []rawTimer
	if err := json.Unmarshal(res.Stdout, &rows); err != nil {
		return TimerList{}, nil, &proto.Error{Code: proto.CodeInternal, Message: "unexpected systemctl list-timers output"}
	}
	out := TimerList{Timers: []TimerInfo{}, Source: "systemctl list-timers --all --output=json and systemctl show (run as homelab-control)"}
	var names []string
	for _, r := range rows {
		if !unitNameRE.MatchString(r.Unit) || !strings.HasSuffix(r.Unit, ".timer") {
			continue
		}
		if len(out.Timers) >= maxListed {
			out.Truncated = true
			break
		}
		t := TimerInfo{Unit: r.Unit, Next: usecTime(r.Next), Last: usecTime(r.Last)}
		if r.Activates != nil && unitNameRE.MatchString(*r.Activates) {
			t.Activates = *r.Activates
		}
		out.Timers = append(out.Timers, t)
		names = append(names, r.Unit)
	}
	return out, names, nil
}

// ApplyTimerStates adds the batched `systemctl show` facts; timers systemd did
// not answer for keep the state "unknown".
func ApplyTimerStates(out *TimerList, st map[string]map[string][]string) {
	for i := range out.Timers {
		t := &out.Timers[i]
		b, ok := st[t.Unit]
		if !ok {
			t.ActiveState = "unknown"
			continue
		}
		t.Description = clean(first(b, "Description"), 256)
		t.ActiveState, t.SubState, t.UnitFileState = state(first(b, "ActiveState")), state(first(b, "SubState")), state(first(b, "UnitFileState"))
		switch first(b, "Persistent") {
		case "yes":
			v := true
			t.Persistent = &v
		case "no":
			v := false
			t.Persistent = &v
		}
		for _, k := range []string{"TimersCalendar", "TimersMonotonic"} {
			for _, v := range b[k] {
				if s := schedule(v); s != "" && len(t.Schedule) < 16 {
					t.Schedule = append(t.Schedule, s)
				}
			}
		}
	}
}

var socketProps = []string{"Id", "Description", "ActiveState", "SubState", "UnitFileState"}

func socketsList(ctx context.Context, env *Env, raw json.RawMessage) (any, error) {
	if err := requireNoArgs(raw); err != nil {
		return nil, err
	}
	if err := requireSystemctl(env); err != nil {
		return nil, err
	}
	res, err := env.Runner.Run(ctx, "systemctl", "list-sockets", "--all", "--output=json", "--no-pager")
	if err != nil {
		return nil, runErr(err)
	}
	if res.ExitCode != 0 {
		return nil, &proto.Error{Code: proto.CodeUnavailable, Message: "systemctl list-sockets failed: " + clean(res.Stderr, 200)}
	}
	out, names, err := ParseSocketRows(res.Stdout)
	if err != nil {
		return nil, err
	}
	ApplySocketStates(&out, stateOf(ctx, env, names, socketProps))
	return out, nil
}

// ParseSocketRows parses `systemctl list-sockets --all --output=json`, one
// entry per socket unit with all its listen addresses.
func ParseSocketRows(b []byte) (SocketList, []string, error) {
	res := struct{ Stdout []byte }{b}
	var rows []rawSocket
	if err := json.Unmarshal(res.Stdout, &rows); err != nil {
		return SocketList{}, nil, &proto.Error{Code: proto.CodeInternal, Message: "unexpected systemctl list-sockets output"}
	}
	out := SocketList{Sockets: []SocketInfo{}, Source: "systemctl list-sockets --all --output=json and systemctl show (run as homelab-control)"}
	idx := map[string]int{}
	for _, r := range rows {
		if !unitNameRE.MatchString(r.Unit) || !strings.HasSuffix(r.Unit, ".socket") {
			continue
		}
		i, ok := idx[r.Unit]
		if !ok {
			if len(out.Sockets) >= maxListed {
				out.Truncated = true
				continue
			}
			out.Sockets = append(out.Sockets, SocketInfo{Unit: r.Unit})
			i = len(out.Sockets) - 1
			idx[r.Unit] = i
		}
		s := &out.Sockets[i]
		if l := clean(r.Listen, 200); l != "" && len(s.Listen) < 32 && !contains(s.Listen, l) {
			s.Listen = append(s.Listen, l)
		}
		if r.Activates != nil && unitNameRE.MatchString(*r.Activates) && !contains(s.Activates, *r.Activates) && len(s.Activates) < 16 {
			s.Activates = append(s.Activates, *r.Activates)
		}
	}
	sort.Slice(out.Sockets, func(i, j int) bool { return out.Sockets[i].Unit < out.Sockets[j].Unit })
	names := make([]string, 0, len(out.Sockets))
	for _, s := range out.Sockets {
		names = append(names, s.Unit)
	}
	return out, names, nil
}

// ApplySocketStates adds the batched `systemctl show` facts.
func ApplySocketStates(out *SocketList, st map[string]map[string][]string) {
	for i := range out.Sockets {
		s := &out.Sockets[i]
		b, ok := st[s.Unit]
		if !ok {
			s.ActiveState = "unknown"
			continue
		}
		s.Description = clean(first(b, "Description"), 256)
		s.ActiveState, s.SubState, s.UnitFileState = state(first(b, "ActiveState")), state(first(b, "SubState")), state(first(b, "UnitFileState"))
	}
}

func systemdBooted(env *Env) bool {
	fi, err := os.Stat(env.Roots.Path("/run/systemd/system"))
	return err == nil && fi.IsDir()
}
