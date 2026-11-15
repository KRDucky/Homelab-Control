// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Command hlctl is the Homelab Control administration CLI.
//
// M0 implements local account management only. Theme and plugin commands
// described in docs/project/06-THEME-AND-PLUGIN-SDK.md are planned (M3).
package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"homelab-control/backend/internal/api"
	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/brokerclient"
	"homelab-control/backend/internal/state"
)

const usage = `hlctl - Homelab Control administration CLI (M0)

Usage:
  hlctl user add     --file PATH --username NAME [--scopes system.read]   (password read from stdin)
  hlctl user passwd  --file PATH --username NAME                          (password read from stdin)
  hlctl user disable --file PATH --username NAME
  hlctl user scopes  --file PATH --username NAME --scopes A,B              (replaces the account's scopes)
  hlctl user list    --file PATH
  hlctl broker info  [--socket PATH]                                       (as root or homelab-control)
  hlctl audit verify (--file PATH | --db PATH) [--checkpoint SEQ:HASH]... [--anchor SEQ:HASH]
  hlctl audit stats  (--file PATH | --db PATH)                             (aggregate counts only)
  hlctl audit export --db PATH                                             (JSON Lines to stdout)
  hlctl version
`

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "hlctl:", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return errors.New("missing command")
	}
	if args[0] == "version" {
		fmt.Fprintln(stdout, api.Version)
		return nil
	}
	if args[0] == "broker" {
		return brokerCmd(args[1:], stdout)
	}
	if args[0] == "audit" {
		return auditCmd(args[1:], stdout)
	}
	if args[0] != "user" || len(args) < 2 {
		fmt.Fprint(stdout, usage)
		return fmt.Errorf("unknown command %q", strings.Join(args, " "))
	}
	sub := args[1]
	fs := flag.NewFlagSet("user "+sub, flag.ContinueOnError)
	file := fs.String("file", "/etc/homelab-control/users.json", "users file")
	username := fs.String("username", "", "account name")
	scopes := fs.String("scopes", auth.DefaultScopes, "comma-separated scopes")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}
	f, err := loadOrNew(*file, sub == "add")
	if err != nil {
		return err
	}
	switch sub {
	case "list":
		for _, u := range f.Users {
			state := "enabled"
			if u.Disabled {
				state = "disabled"
			}
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", u.Username, state, strings.Join(u.Scopes, ","))
		}
		return nil
	case "add", "passwd", "disable", "scopes":
	default:
		return fmt.Errorf("unknown user subcommand %q", sub)
	}
	if err := auth.ValidateUsername(*username); err != nil {
		return err
	}
	idx := -1
	for i, u := range f.Users {
		if u.Username == *username {
			idx = i
		}
	}
	switch sub {
	case "add":
		if idx >= 0 {
			return fmt.Errorf("user %q already exists", *username)
		}
		pw, err := readPassword(stdin)
		if err != nil {
			return err
		}
		h, err := auth.HashPassword(pw, auth.DefaultIterations)
		if err != nil {
			return err
		}
		f.Users = append(f.Users, auth.User{Username: *username, PasswordHash: h, Scopes: splitScopes(*scopes)})
	case "passwd":
		if idx < 0 {
			return fmt.Errorf("no such user %q", *username)
		}
		pw, err := readPassword(stdin)
		if err != nil {
			return err
		}
		if f.Users[idx].PasswordHash, err = auth.HashPassword(pw, auth.DefaultIterations); err != nil {
			return err
		}
	case "disable":
		if idx < 0 {
			return fmt.Errorf("no such user %q", *username)
		}
		f.Users[idx].Disabled = true
	case "scopes":
		// Scopes are only ever granted explicitly (owner decision D-7): an
		// upgrade never adds new scopes such as services.logs to accounts.
		if idx < 0 {
			return fmt.Errorf("no such user %q", *username)
		}
		set := false
		fs.Visit(func(fl *flag.Flag) { set = set || fl.Name == "scopes" })
		if !set {
			return errors.New("--scopes is required")
		}
		f.Users[idx].Scopes = splitScopes(*scopes)
	}
	if err := auth.SaveUsersFile(*file, f); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "updated %s (restart hc-api to apply)\n", *file)
	return nil
}

func splitScopes(v string) []string {
	var sc []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			sc = append(sc, s)
		}
	}
	return sc
}

func loadOrNew(path string, allowCreate bool) (*auth.UsersFile, error) {
	f, err := auth.LoadUsersFile(path)
	if err == nil {
		return f, nil
	}
	if allowCreate && errors.Is(err, os.ErrNotExist) {
		return &auth.UsersFile{Version: 1}, nil
	}
	return nil, err
}

// readPassword reads one line from stdin. Interactive no-echo input is left to
// the caller's shell (e.g. `read -rs`), keeping hlctl free of terminal and
// process-execution dependencies.
func readPassword(r io.Reader) (string, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	pw := strings.TrimRight(line, "\r\n")
	if len(pw) < auth.MinPasswordLength {
		return "", fmt.Errorf("password must be at least %d characters", auth.MinPasswordLength)
	}
	return pw, nil
}

// brokerCmd queries the broker's self-description. It has no scopes, so it can
// only call scope-free ops (broker.info).
func brokerCmd(args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] != "info" {
		return errors.New("usage: hlctl broker info [--socket PATH]")
	}
	fs := flag.NewFlagSet("broker info", flag.ContinueOnError)
	sock := fs.String("socket", proto.DefaultSocket, "broker socket")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	var info struct {
		Version  string `json:"version"`
		Protocol int    `json:"protocol"`
		Ops      []struct {
			Name, Scope, Privilege, Description string
		} `json:"ops"`
		Exec []string `json:"exec_available"`
	}
	if err := brokerclient.New(*sock).Call(context.Background(), "broker.info", proto.Actor{User: "hlctl"}, &info); err != nil {
		return fmt.Errorf("broker: %w", err)
	}
	fmt.Fprintf(stdout, "broker %s (protocol %d), exec available: %s\n", info.Version, info.Protocol, strings.Join(info.Exec, ","))
	for _, o := range info.Ops {
		scope := o.Scope
		if scope == "" {
			scope = "-"
		}
		fmt.Fprintf(stdout, "  %-24s %-14s %-18s %s\n", o.Name, scope, o.Privilege, o.Description)
	}
	return nil
}

type checkpointFlags []audit.Checkpoint

func (c *checkpointFlags) String() string { return fmt.Sprint(len(*c)) }
func (c *checkpointFlags) Set(v string) error {
	cp, err := audit.ParseCheckpoint(v)
	if err != nil {
		return err
	}
	*c = append(*c, cp)
	return nil
}

// auditCmd verifies or summarises an audit log. Verify checks the hash chain
// and, with --anchor, can accept a log whose beginning was pruned away;
// without --anchor a partial log fails. Stats reports aggregate counts only
// (never user, remote, detail or data).
func auditCmd(args []string, stdout io.Writer) error {
	const use = "usage: hlctl audit verify (--file PATH | --db PATH) [--checkpoint SEQ:HASH]... [--anchor SEQ:HASH]\n       hlctl audit stats  (--file PATH | --db PATH)\n       hlctl audit export --db PATH"
	if len(args) == 0 || (args[0] != "verify" && args[0] != "export" && args[0] != "stats") {
		return errors.New(use)
	}
	fs := flag.NewFlagSet("audit "+args[0], flag.ContinueOnError)
	file := fs.String("file", "", "JSON Lines audit log (e.g. /var/log/homelab-control-broker/broker-audit.jsonl)")
	dbPath := fs.String("db", "", "hc-api state database (e.g. /var/lib/homelab-control/state.db), opened read-only")
	var cps checkpointFlags
	fs.Var(&cps, "checkpoint", "SEQ:HASH from a journal checkpoint line (repeatable)")
	anchor := fs.String("anchor", "", "SEQ:HASH of the record immediately before the first remaining record (anchored verification of a pruned log)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if (*file == "") == (*dbPath == "") || (args[0] == "export" && *dbPath == "") {
		return errors.New(use)
	}
	if args[0] == "stats" {
		return auditStats(file, dbPath, stdout)
	}
	var anchorCP *audit.Checkpoint
	if *anchor != "" {
		cp, err := audit.ParseCheckpoint(*anchor)
		if err != nil {
			return fmt.Errorf("anchor: %w", err)
		}
		anchorCP = &cp
	}
	var r io.Reader
	if *dbPath != "" {
		db, err := state.OpenReadOnly(*dbPath)
		if err != nil {
			return err
		}
		defer db.Close()
		if args[0] == "export" {
			return db.Audit().Export(stdout)
		}
		var buf bytes.Buffer
		if err := db.Audit().Export(&buf); err != nil {
			return err
		}
		r = &buf
	} else {
		f, err := os.Open(*file)
		if err != nil {
			return err
		}
		defer f.Close()
		r = f
	}
	var rep audit.Report
	if anchorCP != nil {
		rep = audit.VerifyAnchored(r, *anchorCP, cps...)
	} else {
		rep = audit.Verify(r, cps...)
	}
	fmt.Fprintf(stdout, "records %d (legacy %d), segments %d, head seq=%d hash=%s\n", rep.Records, rep.Legacy, rep.Segments, rep.HeadSeq, rep.HeadHash)
	for _, w := range rep.Warnings {
		fmt.Fprintln(stdout, "WARN ", w)
	}
	for _, p := range rep.Problems {
		fmt.Fprintln(stdout, "FAIL ", p)
	}
	if len(cps) == 0 {
		fmt.Fprintln(stdout, "note: without --checkpoint a truncated tail or a rolled-back file cannot be detected")
	}
	if !rep.OK() {
		return fmt.Errorf("audit log verification failed (%d problems)", len(rep.Problems))
	}
	fmt.Fprintln(stdout, "OK: chain verified")
	return nil
}

// auditStats prints aggregate audit-log statistics. It never prints user,
// remote, detail or data fields.
func auditStats(file, dbPath *string, stdout io.Writer) error {
	var r io.Reader
	var srcBytes int64
	if *dbPath != "" {
		db, err := state.OpenReadOnly(*dbPath)
		if err != nil {
			return err
		}
		defer db.Close()
		var buf bytes.Buffer
		if err := db.Audit().Export(&buf); err != nil {
			return err
		}
		srcBytes = int64(buf.Len())
		r = &buf
	} else {
		f, err := os.Open(*file)
		if err != nil {
			return err
		}
		defer f.Close()
		fi, err := f.Stat()
		if err != nil {
			return err
		}
		srcBytes = fi.Size()
		r = f
	}
	s, err := audit.Summarize(r)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "records %d (legacy %d), segments %d, bytes %d\n", s.Records, s.Legacy, s.Segments, srcBytes)
	if s.Oldest.IsZero() {
		fmt.Fprintln(stdout, "oldest -")
		fmt.Fprintln(stdout, "newest -")
	} else {
		fmt.Fprintf(stdout, "oldest %s\nnewest %s\n", s.Oldest.UTC().Format(time.RFC3339), s.Newest.UTC().Format(time.RFC3339))
	}
	if len(s.ReadEvents) == 0 {
		fmt.Fprintln(stdout, "read events: none")
		return nil
	}
	fmt.Fprintln(stdout, "read events (family/day):")
	fams := make([]string, 0, len(s.ReadEvents))
	for f := range s.ReadEvents {
		fams = append(fams, f)
	}
	sort.Strings(fams)
	for _, f := range fams {
		days := make([]string, 0, len(s.ReadEvents[f]))
		for d := range s.ReadEvents[f] {
			days = append(days, d)
		}
		sort.Strings(days)
		fmt.Fprintf(stdout, "  %s:", f)
		for _, d := range days {
			fmt.Fprintf(stdout, " %s=%d", d, s.ReadEvents[f][d])
		}
		fmt.Fprintln(stdout)
	}
	return nil
}
