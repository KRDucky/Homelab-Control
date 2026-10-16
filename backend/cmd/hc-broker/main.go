// Command hc-broker is the privileged Homelab Control broker (ADR 0001/0008).
//
// It runs as root under systemd socket activation, accepts requests only from
// the homelab-control service user (and root), and executes a closed list of
// read-only, typed operations. It has no generic command, file or RPC operation.
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
	"homelab-control/backend/internal/version"
)

func main() {
	socket := flag.String("socket", proto.DefaultSocket, "socket path when not socket-activated")
	serviceUser := flag.String("service-user", "homelab-control", "account allowed to call the broker and used for child processes")
	auditFile := flag.String("audit-log", "/var/log/homelab-control-broker/broker-audit.jsonl", "broker audit log")
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
	defer al.Close()

	allowed := []uint32{uint32(uid)}
	if os.Geteuid() == 0 {
		allowed = append(allowed, 0)
	} else {
		allowed = append(allowed, uint32(os.Geteuid()))
	}
	srv := server.New(&ops.Env{Runner: rn, Roots: ops.Roots{Root: "/"}, Version: version.Version}, al, allowed...)
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
