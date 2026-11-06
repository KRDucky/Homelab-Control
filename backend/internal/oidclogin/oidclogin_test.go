// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package oidclogin

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A failed discovery journals a fixed category, never text the provider sent.
func TestDiscoveryFailureLogOmitsProviderText(t *testing.T) {
	const sentinel = "SENTINEL-provider-text-7f3a"
	for name, tc := range map[string]struct {
		handler  http.HandlerFunc
		category string
	}{
		"error status": {func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, sentinel, http.StatusInternalServerError)
		}, "provider rejected"},
		"issuer mismatch": {func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"issuer":"https://` + sentinel + `.test"}`))
		}, "issuer mismatch"},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewTLSServer(tc.handler)
			defer srv.Close()
			pool := x509.NewCertPool()
			pool.AddCert(srv.Certificate())
			f := New(Config{Version: 1, Issuer: srv.URL, ClientID: "c", RedirectURL: "https://hc.test" + CallbackPath}, pool)

			var buf bytes.Buffer
			prev, flags := log.Writer(), log.Flags()
			log.SetOutput(&buf)
			defer func() { log.SetOutput(prev); log.SetFlags(flags) }()

			_, _, err := f.Begin(context.Background())
			var fe *Failure
			if !errors.As(err, &fe) || fe.Reason != "provider discovery failed" {
				t.Fatalf("err: %v", err)
			}
			if out := buf.String(); strings.Contains(out, sentinel) || !strings.Contains(out, tc.category) {
				t.Fatalf("journal: %q", out)
			}
		})
	}
}
