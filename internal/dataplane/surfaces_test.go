package dataplane_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"

	"altalune.id/yasaku/internal/controlplane"
	"altalune.id/yasaku/internal/dataplane"
	"altalune.id/yasaku/internal/platform/surfaces"
)

// TestOneVerbOnePlane enforces R1: a verb lives on exactly one machine plane, S2 or S3.
func TestOneVerbOnePlane(t *testing.T) {
	allowed := map[string]bool{"blog": true}

	seen := map[surfaces.Verb]string{}
	for _, v := range controlplane.Verbs() {
		seen[v] = "control"
	}

	dataVerbs := dataplane.Verbs()
	if len(dataVerbs) == 0 {
		t.Fatal("dataplane.Verbs() is empty — this guard would pass vacuously")
	}
	if len(seen) == 0 {
		t.Fatal("controlplane.Verbs() is empty — this guard would pass vacuously")
	}

	for _, v := range dataVerbs {
		t.Run(v.Module+"."+v.Aggregate+"."+v.Operation, func(t *testing.T) {
			plane, ok := seen[v]
			if !ok || allowed[v.Module] {
				return
			}
			t.Errorf("R1: a verb lives on exactly one machine plane, but %+v is registered on "+
				"both the %s plane and the data plane. Either drop one, or add %q to the "+
				"allowlist in this test and say why in ../../docs/surfaces/README.md.", v, plane, v.Module)
		})
	}
}

// TestDataPlaneVerbTableMatchesRegisteredRoutes is R1's registry half for S3.
func TestDataPlaneVerbTableMatchesRegisteredRoutes(t *testing.T) {
	registered := registeredRoutes(t)
	declared := dataplane.VerbTable()

	for _, route := range registered {
		t.Run("registered/"+route, func(t *testing.T) {
			if _, ok := declared[route]; !ok {
				t.Errorf("route %q is registered by dataplane.NewHandler but names no verb in "+
					"dataplane.VerbTable() — add it, or R1 cannot see this route", route)
			}
		})
	}
	for route := range declared {
		t.Run("declared/"+route, func(t *testing.T) {
			if !slices.Contains(registered, route) {
				t.Errorf("dataplane.VerbTable() declares %q but NewHandler registers no such "+
					"route — the table is stale; registered routes are %v", route, registered)
			}
		})
	}
}

// TestControlPlaneVerbTableMatchesScopeTable is R1's registry half for S2.
func TestControlPlaneVerbTableMatchesScopeTable(t *testing.T) {
	scoped := controlplane.ScopeTable()
	declared := controlplane.VerbTable()

	if len(scoped) == 0 {
		t.Fatal("controlplane.ScopeTable() is empty — this guard needs updating")
	}
	for procedure := range scoped {
		t.Run("scoped/"+procedure, func(t *testing.T) {
			if _, ok := declared[procedure]; !ok {
				t.Errorf("procedure %s has a scope entry but names no verb in "+
					"controlplane.VerbTable() — add it, or R1 cannot see this procedure", procedure)
			}
		})
	}
	for procedure := range declared {
		t.Run("declared/"+procedure, func(t *testing.T) {
			if _, ok := scoped[procedure]; !ok {
				t.Errorf("controlplane.VerbTable() declares %s but controlplane.ScopeTable() "+
					"does not — one of the two is stale", procedure)
			}
		})
	}
}

func registeredRoutes(t *testing.T) []string {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "dataplane.go", nil, 0)
	if err != nil {
		t.Fatalf("parse dataplane.go: %v — this guard needs updating", err)
	}

	methods := map[string]bool{
		"GET": true, "HEAD": true, "POST": true, "PUT": true,
		"PATCH": true, "DELETE": true, "OPTIONS": true,
	}
	routes := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "HandleFunc" && sel.Sel.Name != "Handle" {
			return true
		}
		lits := stringLiterals(call.Args[0])
		if len(lits) == 0 {
			return true
		}
		method := strings.TrimSpace(lits[0])
		if !methods[method] {
			t.Fatalf("mux registration %q does not start with an HTTP method — this guard "+
				"needs updating", strings.Join(lits, ""))
		}
		routes[strings.TrimSpace(method+" "+strings.Join(lits[1:], ""))] = true
		return true
	})

	out := slices.Sorted(maps.Keys(routes))
	if len(out) < 6 {
		t.Fatalf("found only %d mux registrations in NewHandler (%v); the registration shape "+
			"changed and this guard needs updating", len(out), out)
	}
	return out
}

func stringLiterals(expr ast.Expr) []string {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return nil
		}
		s, err := strconv.Unquote(e.Value)
		if err != nil {
			return nil
		}
		return []string{s}
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return nil
		}
		return append(stringLiterals(e.X), stringLiterals(e.Y)...)
	}
	return nil
}
