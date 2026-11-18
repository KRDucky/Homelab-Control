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
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/broker/proto"
	"homelab-control/backend/internal/brokerclient"
	"homelab-control/backend/internal/hostinfo"
	"homelab-control/backend/internal/state"
)

// TestHostSettingsBrowserFixture is an opt-in real HTTP/API/database fixture
// server, never a production mode. Its only host-settings source is a private
// copy of the labelled synthetic tree testdata/hostsettings. Credentials live
// in a private handoff file, never command arguments, evidence logs, browser
// exports or Git. A preview-only broker stand-in answers broker.info so an
// apply-capable user sees apply_available, but host.settings.plan/apply fail
// closed: the fixture never runs hostnamectl or timedatectl, and is not
// acceptance.
func TestHostSettingsBrowserFixture(t *testing.T) {
	runtimeDir := os.Getenv("HC_HOST_SETTINGS_BROWSER_DIR")
	if runtimeDir == "" {
		t.Skip("opt-in local host-settings browser fixture; no host apply")
	}
	if os.Geteuid() == 0 {
		t.Fatal("browser fixture must run non-root")
	}
	info, e := os.Lstat(runtimeDir)
	if e != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("runtime directory must already exist with mode0700")
	}
	runtimeDir, e = filepath.Abs(filepath.Clean(runtimeDir))
	if e != nil {
		t.Fatal(e)
	}
	uiPath := os.Getenv("HC_HOST_SETTINGS_BROWSER_UI")
	if _, e = os.Stat(filepath.Join(uiPath, "index.html")); e != nil {
		t.Fatal("build the frontend before the fixture")
	}

	src := filepath.Join("..", "hostinfo", "testdata", "hostsettings")
	if st, e := os.Lstat(src); e != nil || !st.IsDir() {
		t.Fatal("labelled hostsettings testdata missing")
	}
	metadata := filepath.Join(runtimeDir, "metadata")
	if e = copyHostSettingsTree(metadata, src); e != nil {
		t.Fatal(e)
	}
	if e = os.MkdirAll(filepath.Join(metadata, "sys"), 0700); e != nil {
		t.Fatal(e)
	}

	dbdir := filepath.Join(runtimeDir, "state")
	if e = os.Mkdir(dbdir, 0700); e != nil {
		t.Fatal(e)
	}
	db, e := state.Open(filepath.Join(dbdir, "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	al, e := audit.OpenStore(db.Audit())
	if e != nil {
		t.Fatal(e)
	}
	defer al.Close()
	nonce := make([]byte, 24)
	if _, e = rand.Read(nonce); e != nil {
		t.Fatal(e)
	}
	password := hex.EncodeToString(nonce)
	hash, e := auth.HashPassword(password, auth.MinIterations)
	if e != nil {
		t.Fatal(e)
	}
	users := &auth.UsersFile{Version: 1, Users: []auth.User{
		{Username: "preview", PasswordHash: hash, Scopes: []string{auth.ScopeSystemRead, auth.ScopeHostSettingsPreview}},
		{Username: "limited", PasswordHash: hash, Scopes: []string{auth.ScopeSystemRead}},
		{Username: "apply", PasswordHash: hash, Scopes: []string{auth.ScopeSystemRead, auth.ScopeHostSettingsPreview, auth.ScopeHostSettingsApply}},
	}}
	collector := &hostinfo.Collector{Roots: hostinfo.Roots{
		Proc: filepath.Join(metadata, "proc"),
		Sys:  filepath.Join(metadata, "sys"),
		Etc:  filepath.Join(metadata, "etc"),
	}}
	hs := collector.HostSettings()
	if hs.StaticHostname.Value != "fixture-static" || hs.PrettyHostname.Value != "Fixture Lab (example)" ||
		hs.KernelHostname.Value != "fixture-kernel-name" || hs.Timezone.Value != "Europe/Berlin" {
		t.Fatalf("fixture observations: %+v", hs)
	}

	broker := &browserBroker{}
	server := New(Config{}, auth.NewStore(users), al, collector, os.DirFS(uiPath)).
		WithBroker(broker).
		WithSessionStore(db.Sessions())
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
	handoff, e := json.Marshal(map[string]string{
		"url":              "http://" + listener.Addr().String(),
		"username":         "preview",
		"limited_username": "limited",
		"apply_username":   "apply",
		"password":         password,
	})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(connection, handoff, 0600); e != nil {
		t.Fatal(e)
	}
	t.Log("labelled isolated host-settings fixture ready; credentials withheld; preview-only testdata; not live hostnamectl/timedatectl or acceptance")
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
				t.Logf("fixture shutdown: plan_calls=%d apply_calls=%d; synthetic hostsettings testdata only; no hostnamectl/timedatectl", broker.planCalls.Load(), broker.applyCalls.Load())
				return
			}
		}
	}
}

// copyHostSettingsTree copies src into dst, preserving the /etc/localtime
// relative symlink so timezone observation matches the labelled fixture.
func copyHostSettingsTree(dst, src string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		if d.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0600)
	})
}

// browserBroker is a preview-only broker stand-in for the labelled browser
// fixture. It answers broker.info (writes_available, exec, ops) so the API
// reports apply_available to the apply-capable user, but host.settings.plan
// and host.settings.apply always fail closed — the fixture never runs
// hostnamectl or timedatectl. The counters are logged at shutdown so a browser
// exercise can assert the confirm UI never reached plan/apply.
type browserBroker struct {
	planCalls  atomic.Int64
	applyCalls atomic.Int64
}

func (b *browserBroker) Call(_ context.Context, op string, _ proto.Actor, out any) error {
	if op == "broker.info" {
		raw, _ := json.Marshal(map[string]any{
			"version":          "fixture",
			"systemd_booted":   true,
			"writes_available": true,
			"exec_available":   []string{"systemctl", "hostnamectl", "timedatectl"},
			"ops":              []map[string]string{{"name": "broker.info"}, {"name": "host.settings.plan"}, {"name": "host.settings.apply"}},
		})
		return json.Unmarshal(raw, out)
	}
	switch op {
	case "host.settings.plan":
		b.planCalls.Add(1)
	case "host.settings.apply":
		b.applyCalls.Add(1)
	}
	return brokerclient.ErrUnavailable
}

func (b *browserBroker) CallWithArgs(ctx context.Context, op string, args any, actor proto.Actor, out any) error {
	return b.Call(ctx, op, actor, out)
}
