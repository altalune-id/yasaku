package boot

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

var limitReadOnlyChecks = map[string]string{ //nolint:gochecknoglobals // Immutable manifest; not runtime state.
	"org/service.go:CreateAllowed": "a read for the console's disabled reason; it saves nothing, so it needs no lock",
}

// TestEveryLimitedSaveRunsInAUnitOfWork scans every limit check outside internal/limits. SECURITY: a check must follow a Lock in the same function, and Lock refuses to run outside a unit of work, so two concurrent creates cannot both pass.
func TestEveryLimitedSaveRunsInAUnitOfWork(t *testing.T) {
	fset := token.NewFileSet()
	found := 0
	used := map[string]bool{}
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel("..", path)
		if d.IsDir() && (rel == "limits" || rel == "testutil") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			checkAt, lockAt := callPositions(fn.Body)
			if checkAt == token.NoPos {
				continue
			}
			found++
			key := filepath.ToSlash(rel) + ":" + fn.Name.Name
			if _, ok := limitReadOnlyChecks[key]; ok {
				used[key] = true
				continue
			}
			if lockAt == token.NoPos || lockAt > checkAt {
				t.Errorf("%s: %s checks a limit without a Lock before it; the check races a concurrent create", fset.Position(checkAt), key)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == 0 {
		t.Fatal("no limit check found; the guard would pass vacuously")
	}
	for key := range limitReadOnlyChecks {
		if !used[key] {
			t.Errorf("limitReadOnlyChecks lists %q but it checks no limit any more; remove the entry", key)
		}
	}
}

func callPositions(body *ast.BlockStmt) (checkAt, lockAt token.Pos) {
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch {
		case sel.Sel.Name == "Check" && len(call.Args) >= 2 && isLimitsKey(call.Args[1]):
			if checkAt == token.NoPos {
				checkAt = call.Pos()
			}
		case sel.Sel.Name == "Lock" && lockAt == token.NoPos:
			lockAt = call.Pos()
		}
		return true
	})
	return checkAt, lockAt
}

func isLimitsKey(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "limits"
}
