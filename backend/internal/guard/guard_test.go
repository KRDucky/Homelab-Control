// Package guard contains repository-wide static security guards.
//
// TestNoProcessExecution enforces ADR 0001: the unprivileged API, its
// libraries and the CLI must not spawn processes. Process execution will only
// ever be permitted inside the future privileged broker's typed operation
// adapters (backend/broker/ops/...), which do not exist in M0, and which must
// use fixed argv arrays with no shell.
package guard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
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

// allowedExecRoots lists directories where process execution may later be
// introduced under review. Empty in M0.
var allowedExecRoots = []string{}

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
