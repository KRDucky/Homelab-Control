// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/broker/types"
	"homelab-control/backend/internal/hostinfo"
	"homelab-control/backend/internal/pkgcenter"
)

// softwareBroker is the labelled in-memory package backend for the opt-in
// Software Center browser fixture. It is never a real package manager.
type softwareBroker struct {
	results map[string]any
}

func (f *softwareBroker) CallWithArgs(_ context.Context, op string, _ any, _ proto.Actor, out any) error {
	return f.Call(context.Background(), op, proto.Actor{}, out)
}

func (f *softwareBroker) Call(_ context.Context, op string, _ proto.Actor, out any) error {
	b, _ := json.Marshal(f.results[op])
	return json.Unmarshal(b, out)
}

func softwareBrokerResults() map[string]any {
	return map[string]any{
		"broker.info": map[string]any{"version": "fixture", "systemd_booted": true,
			"exec_available": []string{"dpkg-query", "apt-cache", "apt"},
			"ops": []map[string]string{{"name": "broker.info"}, {"name": "packages.installed"}, {"name": "packages.updates"}, {"name": "packages.sources"}, {"name": "packages.search"}}},
		"packages.installed": types.PackageList{Packages: []pkgcenter.Package{{
			Name: "nginx", Version: "1.2.3", Arch: "amd64", Installed: true,
		}}, Truncated: false, Source: "labelled fixture"},
		"packages.updates": types.UpdateList{Updates: []pkgcenter.Update{{
			Name: "nginx", Arch: "amd64", Current: "1.2.3", New: "1.2.4",
		}}, Truncated: false, Source: "labelled fixture", AsOf: "2026-11-07T00:00:00Z"},
		"packages.sources": types.SourceList{Sources: []pkgcenter.Source{{
			Name: "example-aur", Origin: "https://aur.example.test", Enabled: true, Signed: false, ThirdParty: true,
		}}, Truncated: false, Source: "labelled fixture"},
		"packages.search": types.PackageList{Packages: []pkgcenter.Package{{
			Name: "nginx", Summary: "example web server", Installed: false,
		}}, Truncated: false, Source: "labelled fixture"},
	}
}

// TestSoftwareBrowserFixture is an opt-in real HTTP/API/browser fixture for the
// read-only Software Center. Its only package data is the labelled synthetic
// table above; no package manager is ever spawned and no host is read.
func TestSoftwareBrowserFixture(t *testing.T) {
	runtimeDir := os.Getenv("HC_SOFTWARE_BROWSER_DIR")
	if runtimeDir == "" {
		t.Skip("opt-in local browser fixture; no package data")
	}
	if os.Geteuid() == 0 {
		t.Fatal("browser fixture must run non-root")
	}
	info, e := os.Lstat(runtimeDir)
	if e != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("runtime directory must already exist with mode0700")
	}
	uiPath := os.Getenv("HC_SOFTWARE_BROWSER_UI")
	if _, e = os.Stat(filepath.Join(uiPath, "index.html")); e != nil {
		t.Fatal("build the frontend before the fixture")
	}
	nonce := make([]byte, 24)
	_, _ = rand.Read(nonce)
	password := hex.EncodeToString(nonce)
	hash, e := auth.HashPassword(password, auth.MinIterations)
	if e != nil {
		t.Fatal(e)
	}
	users := &auth.UsersFile{Version: 1, Users: []auth.User{{Username: "browser", PasswordHash: hash, Scopes: []string{auth.ScopeSystemRead, auth.ScopePackagesRead}}}}
	al := audit.New(io.Discard)
	etc := filepath.Join(runtimeDir, "etc")
	if e = os.MkdirAll(etc, 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(etc, "os-release"), []byte("ID=ubuntu\nID_LIKE=debian\n"), 0600); e != nil {
		t.Fatal(e)
	}
	col := &hostinfo.Collector{Roots: hostinfo.Roots{Proc: filepath.Join(runtimeDir, "proc"), Sys: filepath.Join(runtimeDir, "sys"), Etc: etc},
		Statfs: func(string) (uint64, uint64, uint64, error) { return 1, 1, 1, nil }}
	server := New(Config{}, auth.NewStore(users), al, col, os.DirFS(uiPath)).WithBroker(&softwareBroker{results: softwareBrokerResults()})
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	httpServer := &http.Server{Handler: server, ReadHeaderTimeout: 5 * time.Second}
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()
	defer httpServer.Close()
	connection := filepath.Join(runtimeDir, "connection.json")
	defer os.Remove(connection)
	b, e := json.Marshal(map[string]string{"url": "http://" + listener.Addr().String(), "username": "browser", "password": password})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(connection, b, 0600); e != nil {
		t.Fatal(e)
	}
	t.Log("labelled loopback Software fixture ready; credentials withheld")
	timer := time.NewTimer(150 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case e := <-done:
			if !errors.Is(e, http.ErrServerClosed) {
				t.Fatal(e)
			}
			return
		case <-timer.C:
			t.Fatal("browser fixture timed out awaiting stop marker")
		case <-ticker.C:
			if _, e = os.Stat(filepath.Join(runtimeDir, "stop")); e == nil {
				t.Log("fixture shutdown: no package manager was spawned; no host data")
				return
			}
		}
	}
}
