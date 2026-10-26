// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package guard contains repository-wide static security guards.
//
// TestNoProcessExecution enforces ADR 0001/0008: no package may spawn
// processes except internal/broker/runner, which runs allow-listed binaries
// with fixed argv, no shell, as the unprivileged service user.
package guard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var forbiddenImports = map[string]string{
	"os/exec":   "process execution",
	"plugin":    "dynamic Go plugin loading",
	"unsafe":    "memory-unsafe operations",
	"net/rpc":   "generic RPC (arbitrary method invocation)",
	"debug/elf": "binary inspection is not needed by the API",
}

var forbiddenCalls = map[string]bool{
	"syscall.Exec": true, "syscall.ForkExec": true, "syscall.StartProcess": true,
	"os.StartProcess": true, "syscall.Setuid": true, "syscall.Setgid": true,
}

// allowedExecRoots lists the only directories allowed to execute programs.
// Adding an entry requires security review (ADR 0008).
var allowedExecRoots = []string{"internal/broker/runner/"}

func TestNoProcessExecution(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "testdata" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		for _, a := range allowedExecRoots {
			if strings.HasPrefix(rel, a) {
				return nil
			}
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly|parser.ParseComments)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if why, bad := forbiddenImports[p]; bad && why != "" {
				t.Errorf("%s imports %q (%s) — forbidden by ADR 0001", rel, p, why)
			}
		}
		full, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(full, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && forbiddenCalls[id.Name+"."+sel.Sel.Name] {
				t.Errorf("%s: call to %s.%s is forbidden by ADR 0001", fset.Position(sel.Pos()), id.Name, sel.Sel.Name)
			}
			return true
		})
		checked++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 10 {
		t.Fatalf("guard scanned only %d files; walk root wrong?", checked)
	}
}

// TestEveryAPIRouteIsWrapped asserts that HTTP handlers in package api are
// only registered through Server.handle (which enforces authn/authz/CSRF), with
// the two documented exceptions: the /api/ 404 catch-all and the UI handler.
func TestEveryAPIRouteIsWrapped(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "../api/server.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	direct := 0
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "HandleFunc" && sel.Sel.Name != "Handle") {
			return true
		}
		if x, ok := sel.X.(*ast.SelectorExpr); ok && x.Sel.Name == "mux" && len(call.Args) > 0 {
			if lit, ok := call.Args[0].(*ast.BasicLit); ok {
				v, _ := strconv.Unquote(lit.Value)
				if v != "/api/" && v != "/" {
					t.Errorf("%s: route %q registered directly on mux, bypassing Server.handle", fset.Position(call.Pos()), v)
				}
				direct++
			}
		}
		return true
	})
	if direct != 2 {
		t.Errorf("expected exactly 2 direct mux registrations (catch-alls), found %d", direct)
	}
}

// privileged packages that must never be linked into hc-api or hlctl.
var privilegedPkgs = []string{
	"homelab-control/backend/internal/broker/runner",
	"homelab-control/backend/internal/broker/ops",
	"homelab-control/backend/internal/broker/server",
}

// moduleImports returns the module-local and std imports of the non-test
// files in a package directory.
func moduleImports(t *testing.T, root, pkg string) []string {
	t.Helper()
	dir := filepath.Join(root, strings.TrimPrefix(pkg, "homelab-control/backend"))
	fset := token.NewFileSet()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("%s: %v", pkg, err)
	}
	seen := map[string]bool{}
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			seen[p] = true
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	return out
}

// TestUnprivilegedBinariesDoNotLinkPrivilegedCode walks the module-local
// import graph of hc-api and hlctl.
func TestUnprivilegedBinariesDoNotLinkPrivilegedCode(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, main := range []string{"homelab-control/backend/cmd/hc-api", "homelab-control/backend/cmd/hlctl"} {
		visited := map[string]bool{}
		queue := []string{main}
		for len(queue) > 0 {
			p := queue[0]
			queue = queue[1:]
			if visited[p] {
				continue
			}
			visited[p] = true
			for _, imp := range moduleImports(t, root, p) {
				if imp == "os/exec" {
					t.Errorf("%s (linked by %s) imports os/exec", p, main)
				}
				for _, bad := range privilegedPkgs {
					if imp == bad {
						t.Errorf("%s links privileged package %s via %s", main, bad, p)
					}
				}
				if strings.HasPrefix(imp, "homelab-control/backend/") {
					queue = append(queue, imp)
				}
			}
		}
		if len(visited) < 3 {
			t.Fatalf("import walk for %s visited only %d packages", main, len(visited))
		}
	}
}

// rootExecFiles are the only non-test files that may reference the root-exec
// handle or root templates (ADR 0008 Amendment 2). Adding one requires review.
var rootExecFiles = map[string]bool{
	"internal/broker/runner/runner.go": true, // defines the templates and the handle
	"internal/broker/ops/ops.go":       true, // registry lists apply's templates (W1)
	"internal/broker/server/server.go": true, // passes the handle to root-exec ops only
	"cmd/hc-broker/main.go":            true, // creates the single handle
}

var rootExecNames = map[string]bool{
	"RootExec": true, "RootArgv": true, "rootTemplates": true, "RootSystemctlStart": true,
	"RootSystemctlStop": true, "RootSystemctlRestart": true, "RootTemplate": true,
}

func TestRootExecConfined(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	found := map[string]bool{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "testdata" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			var name string
			switch x := n.(type) {
			case *ast.SelectorExpr:
				name = x.Sel.Name
			case *ast.Ident:
				name = x.Name
			}
			if rootExecNames[name] {
				found[rel] = true
				if !rootExecFiles[rel] {
					t.Errorf("%s references %s; root execution is confined to reviewed files (ADR 0008 Amendment 2)", fset.Position(n.Pos()), name)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for f := range rootExecFiles {
		if !found[f] {
			t.Errorf("%s is listed as a root-exec file but no longer references it; shrink the list", f)
		}
	}
}
