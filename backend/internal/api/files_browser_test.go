// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/filesinfo"
	"homelab-control/backend/internal/hostinfo"
	"homelab-control/backend/internal/state"
)

// TestFilesBrowserFixture is an opt-in real HTTP/API/database fixture server,
// never a production mode. Its only files source is a labelled synthetic jail
// under HC_FILES_BROWSER_DIR. Credentials live in a private handoff file, never
// command arguments, evidence logs, browser exports or Git. This is not live
// host browse and not acceptance.
func TestFilesBrowserFixture(t *testing.T) {
	runtimeDir := os.Getenv("HC_FILES_BROWSER_DIR")
	if runtimeDir == "" {
		t.Skip("opt-in local files browser fixture; no host data")
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
	uiPath := os.Getenv("HC_FILES_BROWSER_UI")
	if _, e = os.Stat(filepath.Join(uiPath, "index.html")); e != nil {
		t.Fatal("build the frontend before the fixture")
	}

	jail := filepath.Join(runtimeDir, "jail")
	if e = os.Mkdir(jail, 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.Mkdir(filepath.Join(jail, "sub"), 0700); e != nil {
		t.Fatal(e)
	}
	readme := []byte("Example data — isolated browser fixture, not live host browse or acceptance evidence.\n")
	if e = os.WriteFile(filepath.Join(jail, "readme.txt"), readme, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(jail, "sub", "inner.txt"), []byte("example-inner\n"), 0600); e != nil {
		t.Fatal(e)
	}
	jailRel, e := filepath.Rel(runtimeDir, jail)
	if e != nil || jailRel == ".." || strings.HasPrefix(jailRel, ".."+string(filepath.Separator)) {
		t.Fatal("jail path must stay inside the runtime directory")
	}

	setting := filepath.Join(runtimeDir, "files.json")
	body, e := json.Marshal(struct {
		Version int `json:"version"`
		Roots   []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
			Path  string `json:"path"`
		} `json:"roots"`
	}{Version: 1, Roots: []struct {
		ID    string `json:"id"`
		Label string `json:"label"`
		Path  string `json:"path"`
	}{{ID: "lab", Label: "Lab files", Path: jail}}})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(setting, body, 0644); e != nil {
		t.Fatal(e)
	}
	st, e := os.Lstat(setting)
	if e != nil || !st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm()&0o022 != 0 {
		t.Fatal("files.json must be a regular file that is not group or world writable")
	}

	set := filesinfo.LoadOrFailClosed(setting, false)
	if !set.BrowseAvailable() {
		t.Fatal("files.json must load at least one confined root")
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
		{Username: "files", PasswordHash: hash, Scopes: []string{auth.ScopeSystemRead, auth.ScopeFilesRead}},
		{Username: "limited", PasswordHash: hash, Scopes: []string{auth.ScopeSystemRead}},
	}}
	stub := filepath.Join(runtimeDir, "hostinfo-stub")
	for _, d := range []string{"proc", "sys", "etc"} {
		if e = os.MkdirAll(filepath.Join(stub, d), 0700); e != nil {
			t.Fatal(e)
		}
	}
	collector := &hostinfo.Collector{Roots: hostinfo.Roots{
		Proc: filepath.Join(stub, "proc"),
		Sys:  filepath.Join(stub, "sys"),
		Etc:  filepath.Join(stub, "etc"),
	}}
	server := New(Config{}, auth.NewStore(users), al, collector, os.DirFS(uiPath)).
		WithSessionStore(db.Sessions()).
		WithFiles(set)
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
		"username":         "files",
		"limited_username": "limited",
		"password":         password,
	})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(connection, handoff, 0600); e != nil {
		t.Fatal(e)
	}
	t.Log("labelled isolated files fixture ready; credentials withheld; not live host browse or acceptance")
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
				t.Log("fixture shutdown: synthetic jail only; no host browse or privileged operations")
				return
			}
		}
	}
}
