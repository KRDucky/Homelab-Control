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
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"homelab-control/backend/internal/api"
	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/brokerclient"
	"homelab-control/backend/internal/composediscovery"
	"homelab-control/backend/internal/containeridentity"
	"homelab-control/backend/internal/dockerbroker"
	"homelab-control/backend/internal/filesinfo"
	"homelab-control/backend/internal/hostinfo"
	"homelab-control/backend/internal/hwinfo"
	"homelab-control/backend/internal/mergerfsinfo"
	"homelab-control/backend/internal/modules"
	"homelab-control/backend/internal/oidclogin"
	"homelab-control/backend/internal/platform"
	"homelab-control/backend/internal/platform/composeadapter"
	"homelab-control/backend/internal/platform/kubernetes"
	"homelab-control/backend/internal/platform/podman"
	"homelab-control/backend/internal/state"
	"homelab-control/backend/internal/traefikclient"
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
	metadataSocket := flag.String("docker-metadata-socket", "", "fixed private metadata broker Unix socket; default disabled")
	metadataToken := flag.String("docker-metadata-token-file", "", "private metadata service credential; never browser-visible")
	metadataUID := flag.Uint("docker-metadata-server-uid", 0, "expected server UID in CORE's user namespace (host UID 1000 maps to 0)")
	metadataRouting := flag.String("docker-metadata-routing-config", "", "read-only administrator-approved preview context JSON")
	composeFixture := flag.Bool("compose-discovery-fixture", false, "development-only demonstration Compose metadata; no Docker access")
	platformFixtures := flag.Bool("platform-fixtures", false, "development-only labeled Podman and K3s demonstration platforms; no runtime access")
	identityConfig := flag.String("container-identity-config", "", "optional private JSON with per-platform container identity defaults, ID maps, group tables and roles")
	devHTTP := flag.Bool("dev-insecure-http", false, "allow plain HTTP; only permitted on loopback addresses")
	origins := flag.String("allowed-origins", "", "comma-separated extra origins allowed for unsafe requests")
	legacyURL := flag.String("legacy-url", "", "URL of the legacy Webmin UI to link to (optional)")
	brokerSocket := flag.String("broker-socket", proto.DefaultSocket, "privileged broker socket (empty disables broker features)")
	allowRoot := flag.Bool("i-understand-running-as-root-is-unsupported", false, "permit running as root (development containers only)")
	modulesFile := flag.String("modules-config", modules.DefaultPath, "administrator's module setting (root-owned; missing = all modules enabled)")
	filesFile := flag.String("files-config", filesinfo.DefaultPath, "allow-listed file-manager roots (root-owned; missing/empty = no browse)")
	snapraidConfig := flag.String("snapraid-config", mergerfsinfo.DefaultSnapRAIDPath, "snapraid.conf path for the read-only MergerFS page (missing = SnapRAID not present)")
	oidcConfig := flag.String("oidc-config", "", "single sign-on provider setting (mode 0600, may hold the client secret); empty disables OIDC")
	traefikOrigin := flag.String("traefik-helper-origin", "", "fixed private Traefik extension origin; empty disables integration")
	traefikTokenFile := flag.String("traefik-helper-token-file", "", "private extension token file; never returned to browser")
	traefikDynamicDir := flag.String("traefik-dynamic-dir", "", "administrator Traefik file-provider directory for CORE-generated YAML; empty disables file apply")
	composeApplyDir := flag.String("compose-apply-dir", "", "allowlisted directory of Compose files for Traefik label apply; empty disables compose-label apply")
	traefikSetupDir := flag.String("traefik-setup-dir", "", "allowlisted directory for official Traefik easy-setup compose; empty disables easy-setup writes")
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
	users := auth.NewStore(uf)
	srv := api.New(api.Config{SecureCookies: useTLS || *behindTLSProxy, AllowedOrigins: extra, LegacyURL: *legacyURL},
		users, al, hostinfo.NewCollector(*legacyURL), webui.FS()).
		WithHardware(hwinfo.New())
	if db != nil {
		srv.WithSessionStore(db.Sessions()).WithAuditPager(db.Audit()).WithStorageSnapshots(db.StorageSnapshots()).WithLayouts(db.Layouts())
	}
	if *oidcConfig != "" {
		oc, err := oidclogin.LoadConfig(*oidcConfig)
		if err != nil {
			log.Fatalf("oidc config: %v", err)
		}
		for _, b := range oc.Accounts {
			if u, ok := users.Lookup(b.Username); !ok || u.Disabled {
				log.Printf("warning: oidc subject %q is bound to %q, which is not an enabled local account; that login will be refused", b.Subject, b.Username)
			}
		}
		srv.WithOIDC(oidclogin.New(*oc, nil))
		log.Printf("oidc single sign-on enabled for issuer %s (%d bound accounts)", oc.Issuer, len(oc.Accounts))
	}
	if *traefikOrigin != "" || *traefikTokenFile != "" {
		if *traefikOrigin == "" || *traefikTokenFile == "" {
			log.Fatal("Traefik requires both helper origin and token file")
		}
		token, err := traefikclient.LoadToken(*traefikTokenFile)
		if err != nil {
			log.Fatal("Traefik helper token is unavailable or unsafe")
		}
		client, err := traefikclient.New(*traefikOrigin, token)
		if err != nil {
			log.Fatal("Traefik helper configuration is invalid")
		}
		srv.WithTraefik(client)
	}
	if *traefikDynamicDir != "" {
		abs, err := filepath.Abs(*traefikDynamicDir)
		if err != nil {
			log.Fatal("Traefik dynamic directory is invalid")
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			log.Fatal("Traefik dynamic directory is missing or not a directory")
		}
		srv.WithTraefikDynamicDir(abs)
	}
	if *composeApplyDir != "" {
		abs, err := filepath.Abs(*composeApplyDir)
		if err != nil {
			log.Fatal("Compose apply directory is invalid")
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			log.Fatal("Compose apply directory is missing or not a directory")
		}
		srv.WithComposeApplyDir(abs)
	}
	if *traefikSetupDir != "" {
		abs, err := filepath.Abs(*traefikSetupDir)
		if err != nil {
			log.Fatal("Traefik setup directory is invalid")
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			log.Fatal("Traefik setup directory is missing or not a directory")
		}
		srv.WithTraefikSetupDir(abs)
	}
	// Container platforms are registered only from these startup flags; no API
	// can add a socket, API endpoint or credential.
	var registrations []platform.Registration
	if *metadataSocket != "" || *metadataToken != "" || *metadataRouting != "" {
		if *composeFixture || *metadataSocket == "" || *metadataToken == "" || *metadataUID > 0xffffffff {
			log.Fatal("metadata broker configuration is incomplete or conflicts with fixture mode")
		}
		token, err := traefikclient.LoadToken(*metadataToken)
		if err != nil {
			log.Fatal("private metadata credential unavailable")
		}
		routing := composediscovery.Context{Networks: []string{}, Entrypoints: []string{}, Resolvers: []string{}}
		if *metadataRouting != "" {
			routing, err = dockerbroker.LoadRouting(*metadataRouting)
			if err != nil {
				log.Fatal("metadata preview context invalid")
			}
		}
		client, err := dockerbroker.NewClient(*metadataSocket, token, uint32(*metadataUID), routing)
		if err != nil {
			log.Fatal("private metadata transport unavailable")
		}
		srv.WithComposeDiscovery(client)
		registrations = append(registrations, composePlatform("docker-local", "Docker (rootless)", "docker-metadata-broker:"+*metadataSocket, client,
			composeadapter.Options{Kind: platform.KindDocker, Distribution: platform.DistDockerEngine, Mode: platform.ModeRootless, Auth: platform.AuthBrokerPeerToken, Transport: "unix-metadata-broker", EndpointLabel: "Local rootless Docker metadata broker"}))
	}
	if *composeFixture {
		if !*devHTTP {
			log.Fatal("Compose fixture requires loopback development HTTP mode")
		}
		srv.WithComposeDiscovery(composediscovery.Fixture{})
		registrations = append(registrations, composePlatform("docker-fixture", "Docker (demonstration)", "fixture:docker", composediscovery.Fixture{},
			composeadapter.Options{Kind: platform.KindDocker, Distribution: platform.DistDockerEngine, Mode: platform.ModeRootless, Auth: platform.AuthFixture, Transport: "fixture", EndpointLabel: "Demonstration Docker data"}))
	}
	if *platformFixtures {
		if !*devHTTP {
			log.Fatal("platform fixtures require loopback development HTTP mode")
		}
		registrations = append(registrations, composePlatform("podman-fixture", "Podman (demonstration)", "fixture:podman", podman.Fixture{},
			composeadapter.Options{Kind: platform.KindPodman, Distribution: platform.DistPodman, Mode: platform.ModeRootless, Auth: platform.AuthFixture, Transport: "fixture", EndpointLabel: "Demonstration Podman data", Warnings: podman.Warnings(platform.ModeRootless)}))
		k3s, err := kubernetes.NewFixture()
		if err != nil {
			log.Fatal("K3s fixture invalid")
		}
		registrations = append(registrations, platform.Registration{ID: "k3s-fixture", DisplayName: "K3s (demonstration)", ConnectionRef: "fixture:k3s", Adapter: k3s})
	}
	if len(registrations) > 0 {
		registry, err := platform.NewRegistry(registrations...)
		if err != nil {
			log.Fatal("container platform registry configuration is invalid")
		}
		srv.WithPlatforms(registry)
		if *identityConfig != "" {
			cfg, err := containeridentity.LoadConfig(*identityConfig)
			if err != nil {
				log.Fatal("container identity configuration invalid")
			}
			for id := range cfg.Platforms {
				if !registry.Has(id) {
					log.Fatal("container identity configuration names an unregistered platform")
				}
			}
			srv.WithContainerIdentity(cfg)
		}
	} else if *identityConfig != "" {
		log.Fatal("container identity configuration requires a registered container platform")
	}
	if *brokerSocket != "" {
		srv.WithBroker(brokerclient.New(*brokerSocket))
	}
	// Module registry (M2-3): an invalid setting disables every optional module.
	// Outside development the file must be root-owned (hc-api cannot change it).
	mods := modules.LoadOrFailClosed(*modulesFile, !*devHTTP)
	if mods.Reason != "" {
		log.Print(mods.Reason)
	} else if d := mods.Disabled(); len(d) > 0 {
		log.Printf("modules disabled by %s: %v", *modulesFile, d)
	}
	srv.WithModules(mods)
	files := filesinfo.LoadOrFailClosed(*filesFile, !*devHTTP)
	if files.Reason != "" {
		log.Print(files.Reason)
	} else if files.BrowseAvailable() {
		log.Printf("file-manager roots from %s: %d", *filesFile, len(files.PublicRoots()))
	}
	srv.WithFiles(files)
	srv.WithSnapRAIDConfig(*snapraidConfig)
	if webui.FS() == nil {
		log.Print("warning: UI not embedded; serving API only")
	}

	hs := &http.Server{
		Addr:              *listen,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// Long enough for a service start/stop/restart to finish and be
		// verified (broker op timeout 55 s, client 75 s).
		WriteTimeout:   120 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 32 << 10,
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

func composePlatform(id, name, ref string, p composediscovery.Provider, o composeadapter.Options) platform.Registration {
	a, err := composeadapter.New(p, o)
	if err != nil {
		log.Fatal("container platform adapter configuration is invalid")
	}
	return platform.Registration{ID: id, DisplayName: name, ConnectionRef: ref, Adapter: a}
}
