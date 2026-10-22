// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Command hc-api is the unprivileged Homelab Control API and UI server.
//
// It must not run as root (see docs/adr/0001-privilege-boundary.md). In M0 it
// serves only read-only data that an unprivileged user can observe.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"homelab-control/backend/internal/api"
	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/brokerclient"
	"homelab-control/backend/internal/hostinfo"
	"homelab-control/backend/internal/hwinfo"
	"homelab-control/backend/internal/state"
	"homelab-control/backend/internal/webui"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:9180", "listen address")
	usersFile := flag.String("users-file", "/etc/homelab-control/users.json", "local account store (mode 0600)")
	auditFile := flag.String("audit-log", "/var/log/homelab-control/audit.jsonl", "audit log path (JSON Lines); only used with -state-db=\"\"")
	stateDB := flag.String("state-db", "/var/lib/homelab-control/state.db", "state database (sessions, audit trail); empty = in-memory sessions and the -audit-log file")
	tlsCert := flag.String("tls-cert", "", "TLS certificate (PEM)")
	tlsKey := flag.String("tls-key", "", "TLS private key (PEM)")
	behindTLSProxy := flag.Bool("behind-tls-proxy", false, "TLS is terminated by a trusted reverse proxy on this host; sets Secure cookies")
	devHTTP := flag.Bool("dev-insecure-http", false, "allow plain HTTP; only permitted on loopback addresses")
	origins := flag.String("allowed-origins", "", "comma-separated extra origins allowed for unsafe requests")
	legacyURL := flag.String("legacy-url", "", "URL of the legacy Webmin UI to link to (optional)")
	brokerSocket := flag.String("broker-socket", proto.DefaultSocket, "privileged broker socket (empty disables broker features)")
	allowRoot := flag.Bool("i-understand-running-as-root-is-unsupported", false, "permit running as root (development containers only)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(api.Version)
		return
	}
	if os.Geteuid() == 0 && !*allowRoot {
		log.Fatal("refusing to run as root: hc-api is unprivileged by design (ADR 0001)")
	}
	useTLS := *tlsCert != "" || *tlsKey != ""
	if useTLS && (*tlsCert == "" || *tlsKey == "") {
		log.Fatal("both -tls-cert and -tls-key are required")
	}
	if !useTLS && !*behindTLSProxy {
		if !*devHTTP {
			log.Fatal("no TLS configured: pass -tls-cert/-tls-key, -behind-tls-proxy, or -dev-insecure-http (loopback only)")
		}
		if !isLoopback(*listen) {
			log.Fatal("-dev-insecure-http is only permitted on loopback listen addresses")
		}
	}
	if *behindTLSProxy && !isLoopback(*listen) {
		log.Fatal("-behind-tls-proxy requires a loopback listen address so the proxy is the only client")
	}

	if err := validateLegacyURL(*legacyURL); err != nil {
		log.Fatalf("-legacy-url: %v", err)
	}
	uf, err := auth.LoadUsersFile(*usersFile)
	if err != nil {
		log.Fatalf("users file: %v (create one with `hlctl user add`)", err)
	}
	if len(uf.Users) == 0 {
		log.Fatal("users file contains no accounts")
	}
	var (
		al     *audit.Logger
		db     *state.DB
		logRef string
	)
	if *stateDB != "" {
		db, err = state.Open(*stateDB)
		if err != nil {
			log.Fatalf("state database: %v", err)
		}
		defer db.Close()
		if db.Backup != "" {
			log.Printf("state database migrated to schema %d; previous copy kept at %s", state.SchemaVersion(), db.Backup)
		}
		flag.Visit(func(f *flag.Flag) {
			if f.Name == "audit-log" {
				log.Printf("note: -audit-log is ignored; the audit trail is in %s", *stateDB)
			}
		})
		al, err = audit.OpenStore(db.Audit())
		logRef = db.Path()
	} else {
		log.Print("warning: no state database: sessions end on restart; audit goes to " + *auditFile)
		al, err = audit.Open(*auditFile)
		logRef = *auditFile
	}
	if err != nil {
		log.Fatalf("audit log: %v", err)
	}
	// Checkpoints go to the journal (stderr); see docs/security/AUDIT-LOG.md.
	al.Checkpoint = func(seq uint64, hash string) {
		log.Printf("audit checkpoint log=%s seq=%d hash=%s", logRef, seq, hash)
	}
	al.EmitCheckpoint()
	defer al.Close()

	var extra []string
	for _, o := range strings.Split(*origins, ",") {
		if o = strings.TrimSpace(o); o != "" {
			extra = append(extra, o)
		}
	}
	srv := api.New(api.Config{SecureCookies: useTLS || *behindTLSProxy, AllowedOrigins: extra, LegacyURL: *legacyURL},
		auth.NewStore(uf), al, hostinfo.NewCollector(*legacyURL), webui.FS()).
		WithHardware(hwinfo.New())
	if db != nil {
		srv.WithSessionStore(db.Sessions()).WithAuditPager(db.Audit())
	}
	if *brokerSocket != "" {
		srv.WithBroker(brokerclient.New(*brokerSocket))
	}
	if webui.FS() == nil {
		log.Print("warning: UI not embedded; serving API only")
	}

	hs := &http.Server{
		Addr:              *listen,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		sh, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = hs.Shutdown(sh)
	}()
	log.Printf("hc-api %s listening on %s (tls=%v)", api.Version, *listen, useTLS)
	if useTLS {
		err = hs.ListenAndServeTLS(*tlsCert, *tlsKey)
	} else {
		err = hs.ListenAndServe()
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// validateLegacyURL accepts only absolute http(s) URLs without credentials, so
// the value can be rendered as a link without enabling script URLs.
func validateLegacyURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("must be an absolute http(s) URL, got %q", raw)
	}
	if u.User != nil {
		return errors.New("must not contain credentials")
	}
	return nil
}
