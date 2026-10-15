package guard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestOpenAPIMatchesRoutes is a contract test: every route registered via
// Server.handle must be documented in schemas/openapi/homelab-control.v1.yaml
// with the same method and required scope, and the spec must not document
// routes that do not exist. (Stdlib-only: the YAML is scanned, not fully parsed.)
func TestOpenAPIMatchesRoutes(t *testing.T) {
	scopes := map[string]string{"scopePublic": "", "sessionOnly": "session", "auth.ScopeSystemRead": "system.read",
		"auth.ScopeServicesRead": "services.read", "auth.ScopeHardwareRead": "hardware.read"}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "../api/server.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	code := map[string]string{} // "GET /path" -> scope
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "handle" || len(call.Args) != 3 {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok {
			return true
		}
		pat, _ := strconv.Unquote(lit.Value)
		var scopeName string
		switch a := call.Args[1].(type) {
		case *ast.Ident:
			scopeName = a.Name
		case *ast.SelectorExpr:
			scopeName = a.X.(*ast.Ident).Name + "." + a.Sel.Name
		}
		sc, known := scopes[scopeName]
		if !known {
			t.Errorf("route %s uses unknown scope expression %s", pat, scopeName)
		}
		code[strings.Replace(pat, " /api/v1", " ", 1)] = sc
		return true
	})

	b, err := os.ReadFile("../../../schemas/openapi/homelab-control.v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	pathRE := regexp.MustCompile(`^  (/[^:]*):\s*$`)
	methodRE := regexp.MustCompile(`^    (get|post|put|patch|delete):\s*$`)
	scopeRE := regexp.MustCompile(`^      x-required-scope:\s*(\S+)`)
	secNoneRE := regexp.MustCompile(`^      security:\s*\[\]`)
	spec := map[string]string{}
	inPaths := false
	var cur, key string
	for _, line := range strings.Split(string(b), "\n") {
		if line == "paths:" {
			inPaths = true
			continue
		}
		if !inPaths {
			continue
		}
		if m := pathRE.FindStringSubmatch(line); m != nil {
			cur = m[1]
		} else if m := methodRE.FindStringSubmatch(line); m != nil {
			key = strings.ToUpper(m[1]) + " " + cur
			spec[key] = "session"
		} else if m := scopeRE.FindStringSubmatch(line); m != nil && key != "" {
			spec[key] = m[1]
		} else if secNoneRE.MatchString(line) && key != "" {
			spec[key] = ""
		}
	}
	for route, sc := range code {
		got, ok := spec[strings.Replace(route, "  ", " ", 1)]
		if !ok {
			t.Errorf("route %q is not documented in OpenAPI", route)
			continue
		}
		if got != sc {
			t.Errorf("route %q: code requires %q, OpenAPI says %q", route, sc, got)
		}
	}
	if len(spec) != len(code) {
		t.Errorf("OpenAPI documents %d operations, code registers %d: spec=%v code=%v", len(spec), len(code), spec, code)
	}
}
