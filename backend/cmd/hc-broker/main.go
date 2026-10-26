// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Command hc-broker is the privileged Homelab Control broker (ADR 0001/0008).
//
// It runs as root under systemd socket activation, accepts requests only from
// the homelab-control service user (and root), and executes a closed list of
// typed operations: read-only ones, and (M2-2) systemd service start/stop/
// restart through the plan → confirm → execute framework (ADR 0011). It has
// no generic command, file or RPC operation.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"syscall"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/broker/ops"
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/runner"
	"homelab-control/backend/internal/broker/server"
	"homelab-control/backend/internal/broker/writes"
	"homelab-control/backend/internal/version"
)

func main() {
	socket := flag.String("socket", proto.DefaultSocket, "socket path when not socket-activated")
	serviceUser := flag.String("service-user", "homelab-control", "account allowed to call the broker and used for child processes")
	auditFile := flag.String("audit-log", "/var/log/homelab-control-broker/broker-audit.jsonl", "broker audit log")
	opsFile := flag.String("operations-journal", "/var/log/homelab-control-broker/operations.jsonl", "broker operation journal (write operations)")
	devNonRoot := flag.Bool("dev-non-root", false, "development only: run without root (root-only ops report unreadable data)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version.Version)
		return
	}
	if os.Geteuid() != 0 && !*devNonRoot {
		log.Fatal("hc-broker must run as root (use -dev-non-root only for development)")
	}
	u, err := user.Lookup(*serviceUser)
	if err != nil {
		log.Fatalf("service user %q: %v", *serviceUser, err)
	}
	uid, _ := strconv.ParseUint(u.Uid, 10, 32)
	gid, _ := strconv.ParseUint(u.Gid, 10, 32)
	if uid == 0 {
		log.Fatal("service user must not be root")
	}
	rn, err := runner.New(runner.Identity{UID: uint32(uid), GID: uint32(gid)})
	if err != nil {
		log.Fatalf("runner: %v", err)
	}
	al, err := audit.Open(*auditFile)
	if err != nil {
		log.Fatalf("audit log: %v", err)
	}
	// Checkpoints go to the journal (stderr), a store separate from the audit
	// file; `hlctl audit verify --checkpoint` uses them to detect truncation
	// or rollback (docs/security/AUDIT-LOG.md).
	al.Checkpoint = func(seq uint64, hash string) {
		log.Printf("audit checkpoint log=%s seq=%d hash=%s", *auditFile, seq, hash)
	}
	al.EmitCheckpoint()
	defer al.Close()

	allowed := []uint32{uint32(uid)}
	if os.Geteuid() == 0 {
		allowed = append(allowed, 0)
	} else {
		allowed = append(allowed, uint32(os.Geteuid()))
	}
	env := &ops.Env{Runner: rn, Roots: ops.Roots{Root: "/"}, Version: version.Version}
	// Journal reader group for services.unit.logs (absent on hosts without
	// persistent journald access control; the op then reports unavailable).
	if g, err := user.LookupGroup("systemd-journal"); err == nil {
		if n, err := strconv.ParseUint(g.Gid, 10, 32); err == nil && n != 0 {
			env.JournalGID = uint32(n)
		}
	}
	// Operation journal (ADR 0011). If it cannot be opened, write operations
	// are disabled (fail closed); read-only operations keep working.
	if jl, err := audit.Open(*opsFile); err != nil {
		log.Printf("operation journal %s unavailable, write operations disabled: %v", *opsFile, err)
	} else {
		jl.Checkpoint = func(seq uint64, hash string) {
			log.Printf("audit checkpoint log=%s seq=%d hash=%s", *opsFile, seq, hash)
		}
		jl.EmitCheckpoint()
		defer jl.Close()
		fw, err := writes.New(jl, *opsFile)
		if err != nil {
			log.Fatalf("write framework: %v", err)
		}
		env.Writes = fw
		// Intents without an outcome mean the broker stopped mid-operation:
		// record them as interrupted with the unit's current state. Never retried.
		// The state of interrupted units is read with the read-only root
		// template, like apply's own reads (never by a child that shares hc-api's uid).
		ienv := *env
		ienv.RootExec = rn.RootExec()
		if n, err := fw.Recover(ops.InspectUnit(&ienv)); err != nil {
			log.Printf("operation journal recovery failed, write operations disabled: %v", err)
			env.Writes = nil
		} else if n > 0 {
			log.Printf("recorded %d interrupted operation(s) from before the last broker stop", n)
		}
	}
	srv := server.New(env, al, allowed...)
	srv.RootExec = rn.RootExec()
	l, activated, err := server.Listener(*socket)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	if !activated && os.Geteuid() == 0 {
		// Same ownership the systemd socket unit sets: root:<service group>, 0660.
		if err := os.Chown(*socket, 0, int(gid)); err != nil {
			log.Fatalf("socket ownership: %v", err)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	log.Printf("hc-broker %s ready (socket-activated=%v, peers uid %v, %d ops)", version.Version, activated, allowed, len(ops.Registry()))
	if err := srv.Serve(ctx, l); err != nil {
		log.Fatal(err)
	}
}
