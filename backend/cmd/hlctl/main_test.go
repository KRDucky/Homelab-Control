// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"homelab-control/backend/internal/auth"
)

func TestUserLifecycle(t *testing.T) {
	p := filepath.Join(t.TempDir(), "users.json")
	var out bytes.Buffer
	if err := run([]string{"user", "add", "--file", p, "--username", "admin"}, strings.NewReader("short\n"), &out); err == nil {
		t.Fatal("short password accepted")
	}
	if err := run([]string{"user", "add", "--file", p, "--username", "admin"}, strings.NewReader("a-long-enough-password\n"), &out); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("users file mode %#o", fi.Mode().Perm())
	}
	if err := run([]string{"user", "add", "--file", p, "--username", "admin"}, strings.NewReader("a-long-enough-password\n"), &out); err == nil {
		t.Fatal("duplicate accepted")
	}
	if err := run([]string{"user", "add", "--file", p, "--username", "x", "--scopes", "storage.execute"}, strings.NewReader("a-long-enough-password\n"), &out); err == nil {
		t.Fatal("unknown scope accepted")
	}
	f, err := auth.LoadUsersFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := auth.NewStore(f).Authenticate("admin", "a-long-enough-password"); !ok {
		t.Fatal("created user cannot authenticate")
	}
	if err := run([]string{"user", "disable", "--file", p, "--username", "admin"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run([]string{"user", "list", "--file", p}, nil, &out); err != nil || !strings.Contains(out.String(), "admin\tdisabled") {
		t.Fatalf("%v %q", err, out.String())
	}
}
