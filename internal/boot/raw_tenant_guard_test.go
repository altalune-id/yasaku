package boot

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var rawTenantScopes = map[string]string{ //nolint:gochecknoglobals // Immutable manifest; not runtime state.
	"web/handlers/auth.go:resolveActiveTenant":  "sign-in picks the active project; reads the project list only",
	"web/handlers/handlers.go:layout":           "the layout's project list; reads only",
	"web/handlers/invite.go:GetAccept":          "picks a project after accept; reads the project list only",
	"web/handlers/onboard.go:projectForOnboard": "BootstrapSystem at genesis onboarding is not Can-gated",
	"web/handlers/scope.go:orgScopedRequest":    "the membership check before Bind, and MemberOrgScopes listing orgs; neither reaches a gated method",
	"web/middleware/tenant.go:Tenant":           "wired on no surface",
	"cli/admin.go:audited":                      "operator limits writes under the system org; limits and audit are not Can-gated",
	"controlplane/blog_service.go:scopeToPost":  "blog is mounted on no yasaku surface",
	"controlplane/interceptor/tenant.go:Tenant": "the base scope; every gated handler binds again through bindTo",
	"controlplane/reach.go:projectInOrg":        "looks the project up before the bind",
	"controlplane/target.go:targetOrgs":         "lists a key's own org before the bind",
	"controlplane/target.go:reachableProjects":  "lists reachable projects before the bind",
	"dataplane/scope.go:resolve":                "yasaku mounts no data plane",
}

// TestNoRawTenantIntoOnSurfaces scans the surface packages. SECURITY: a surface binds a tenant scope only through authz.Resolver.Bind, which sets the caller's held set; a raw tenant.Into leaves it empty or stale, so each one left must reach no Can-gated method.
func TestNoRawTenantIntoOnSurfaces(t *testing.T) {
	fset := token.NewFileSet()
	used := map[string]bool{}
	for _, dir := range []string{"web", "cli", "controlplane", "mcp", "dataplane"} {
		err := filepath.WalkDir(filepath.Join("..", dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel("..", path)
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil || !callsPkgFunc(fn.Body, importName(f, tenantPkg), "Into", "WithOrg") {
					continue
				}
				key := filepath.ToSlash(rel) + ":" + fn.Name.Name
				if _, ok := rawTenantScopes[key]; ok {
					used[key] = true
					continue
				}
				t.Errorf("%s calls tenant.Into or tenant.WithOrg; bind through authz.Resolver.Bind, or allowlist it with the reason it reaches no Can-gated method", key)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for key := range rawTenantScopes {
		if !used[key] {
			t.Errorf("rawTenantScopes lists %q but it sets a raw tenant scope no more; remove the entry", key)
		}
	}
}

const (
	tenantPkg = "altalune.id/yasaku/internal/platform/tenant"
	authzPkg  = "altalune.id/yasaku/internal/platform/authz"
)

func importName(f *ast.File, path string) string {
	for _, imp := range f.Imports {
		if strings.Trim(imp.Path.Value, `"`) != path {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return path[strings.LastIndex(path, "/")+1:]
	}
	return ""
}

func callsPkgFunc(node ast.Node, pkg string, funcs ...string) bool {
	if pkg == "" {
		return false
	}
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return !found
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == pkg && slices.Contains(funcs, sel.Sel.Name) {
			found = true
		}
		return !found
	})
	return found
}
