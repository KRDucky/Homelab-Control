// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Command fixture is a development-only browser fixture (never packaged): it
// runs the isolated test IdP on https://127.0.0.1 and the real hc-api Server
// with the embedded UI on http://localhost (loopback, development cookies), so
// a real browser crosses sites exactly as with a real provider. It prints one
// JSON line with the URLs and serves until stdin closes. The account and IdP
// are throwaway and in-memory; the audit log goes to stdout's sibling stderr.
package main

import (
	"bufio"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"homelab-control/backend/internal/api"
	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/hostinfo"
	"homelab-control/backend/internal/oidclogin"
	"homelab-control/backend/internal/oidclogin/testidp"
	"homelab-control/backend/internal/webui"
)

func main() {
	if webui.FS() == nil {
		log.Fatal("UI not built: run make frontend")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	base := fmt.Sprintf("http://localhost:%d", ln.Addr().(*net.TCPAddr).Port)
	idp := testidp.Start(base + oidclogin.CallbackPath)
	idp.Interactive = true
	defer idp.Close()
	pool := x509.NewCertPool()
	pool.AddCert(idp.Certificate())
	hash, _ := auth.HashPassword(testidp.RandomText(), auth.MinIterations) // unknown password: SSO only
	users := auth.NewStore(&auth.UsersFile{Version: 1, Users: []auth.User{{Username: "alice", PasswordHash: hash, Scopes: []string{auth.ScopeSystemRead}}}})
	cfg := oidclogin.Config{Version: 1, Issuer: idp.URL, ClientID: idp.ClientID, ClientSecret: idp.ClientSecret,
		RedirectURL: base + oidclogin.CallbackPath, Accounts: []oidclogin.Binding{{Subject: "sub-alice", Username: "alice"}}}
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}
	srv := api.New(api.Config{}, users, audit.New(os.Stderr), hostinfo.NewCollector(""), webui.FS()).
		WithOIDC(oidclogin.New(cfg, pool))
	go func() { log.Fatal(http.Serve(ln, srv)) }()
	_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"url": base, "idp": idp.URL})
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}
