package templates

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/web"
)

func TestNav_RailAndDockLoopOneTable(t *testing.T) {
	root := repoRoot(t)
	src := readSource(t, root, "internal/web/templates/layout.templ")
	for _, it := range projectNav() {
		if it.InDock && strings.Contains(src, `"`+it.LabelKey+`"`) {
			t.Errorf("layout.templ names %s directly; loop projectNav instead", it.LabelKey)
		}
	}
	out := renderShell(t, &web.ActiveProject{Slug: "home", Name: "Home"}, web.NavScopeProject)
	start := strings.Index(out, `class="dock`)
	end := start + strings.Index(out[start:], "</nav>")
	dock, rail := out[start:end], out[:start]+out[end:]
	inDock := 0
	for _, it := range projectNav() {
		href := `href="/orgs/acme/projects/home` + it.Path + `"`
		if !strings.Contains(rail, href) && it.Show == nil {
			t.Errorf("rail lacks %s", href)
		}
		if it.InDock {
			inDock++
			if !strings.Contains(dock, href) {
				t.Errorf("dock lacks %s", href)
			}
		} else if strings.Contains(dock, href) {
			t.Errorf("dock has %s but InDock is false", href)
		}
	}
	if inDock == 0 {
		t.Fatal("no dock items in projectNav")
	}
}

func TestBrand_NameHasOneSource(t *testing.T) {
	root := repoRoot(t)
	for _, f := range []string{"internal/web/templates/layout.templ", "internal/web/templates/brand_yasaku.templ"} {
		if strings.Contains(readSource(t, root, f), AppName) {
			t.Errorf("%s spells %q; use AppName", f, AppName)
		}
	}
	d := web.LayoutData{Title: "T", Nonce: "n", Version: "v1", Principal: &session.Principal{Email: "a@example.com"}, ActiveOrg: &web.ActiveOrg{Slug: "acme", Name: "Acme"}, ActiveNav: web.ActiveNav{Scope: web.NavScopeOrg}, Content: templ.Raw("<p>page</p>")}
	var b strings.Builder
	if err := Layout(d).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.Contains(out, ">"+AppName+"</span>") || !strings.Contains(out, AppName+" · ") {
		t.Errorf("brand row and version line must both show %q", AppName)
	}
}

func TestShell_MechanismFilesAreAppNeutral(t *testing.T) {
	root := repoRoot(t)
	files := []string{
		"internal/web/templates/layout.templ",
		"internal/web/templates/nav.go",
		"internal/web/templates/tip.templ",
		"internal/web/static/dialog.js",
		"internal/web/static/dropdown.js",
		"internal/web/static/tips.js",
		"internal/web/static/copy.js",
	}
	kit, err := filepath.Glob(filepath.Join(root, "internal/web/templates/ui_*.templ"))
	if err != nil || len(kit) == 0 {
		t.Fatalf("no kit templates: %v", err)
	}
	for _, f := range kit {
		files = append(files, strings.TrimPrefix(f, root+string(filepath.Separator)))
	}
	app := strings.ToLower(AppName)
	for _, f := range files {
		src := strings.ToLower(strings.ReplaceAll(readSource(t, root, f), "altalune.id/"+app+"/", ""))
		if strings.Contains(src, app) {
			t.Errorf("%s names the app; move app data into a *_%s file", f, app)
		}
	}
}
