package templates

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/web"
)

const dialogHostOpen = `<div id="dialog-host" hx-morph-skip>`

func renderWithDialog(t *testing.T, shell bool) string {
	t.Helper()
	d := web.LayoutData{
		Title:   "T",
		Nonce:   "n0nce",
		Content: templ.Raw(`<p>page</p>`),
		Dialogs: templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
			_, err := io.WriteString(w, `<dialog id="page-dialog"></dialog>`)
			return err
		}),
	}
	if shell {
		d.Principal = &session.Principal{Email: "a@example.com"}
		d.ActiveOrg = &web.ActiveOrg{Slug: "acme", Name: "Acme"}
		d.ActiveProject = &web.ActiveProject{Slug: "home", Name: "Home"}
		d.ActiveNav = web.ActiveNav{Scope: web.NavScopeProject, ProjectKey: "overview"}
	}
	return render(t, Layout(d))
}

func TestLayout_DialogHostIsBodyLevelAndHoldsEveryDialog(t *testing.T) {
	for _, shell := range []bool{true, false} {
		out := renderWithDialog(t, shell)
		if n := strings.Count(out, `id="dialog-host"`); n != 1 {
			t.Fatalf("shell=%v: %d dialog hosts, want 1", shell, n)
		}
		host := strings.Index(out, dialogHostOpen)
		if host < 0 {
			t.Fatalf("shell=%v: the host lacks hx-morph-skip: %s", shell, out)
		}
		body := strings.Index(out, "<body")
		before := out[body:host]
		if strings.Count(before, "<div") != strings.Count(before, "</div>") {
			t.Errorf("shell=%v: the dialog host is nested inside a div, so a swap can remove it", shell)
		}
		if strings.Contains(before, "<dialog") {
			t.Errorf("shell=%v: a dialog renders outside the host", shell)
		}
		if !strings.Contains(out[host:], `<dialog id="page-dialog">`) {
			t.Errorf("shell=%v: the page dialog is not in the host", shell)
		}
	}
	if !strings.Contains(renderWithDialog(t, true)[strings.Index(renderWithDialog(t, true), dialogHostOpen):], `<dialog id="switcher"`) {
		t.Error("the switcher sheet is not in the host")
	}
}

func TestLayout_MainIsTheFocusFallback(t *testing.T) {
	for _, shell := range []bool{true, false} {
		if !strings.Contains(renderWithDialog(t, shell), `<main id="main" tabindex="-1"`) {
			t.Errorf("shell=%v: main needs id and tabindex=-1 so dialog.js can return focus to it", shell)
		}
	}
}

func TestLayout_LoadsKitScriptsOnceWithTheNonce(t *testing.T) {
	out := renderWithDialog(t, true)
	for _, js := range []string{"dialog.js", "dropdown.js", "tips.js", "copy.js"} {
		want := `src="/static/` + js
		if n := strings.Count(out, want); n != 1 {
			t.Errorf("layout loads %s %d times, want 1", js, n)
		}
	}
	if strings.Contains(out, "data-switcher") || strings.Contains(out, "document.addEventListener('keydown'") {
		t.Error("the inline dropdown closer is still in the layout")
	}
	if strings.Contains(out, "CopyBound") {
		t.Error("the inline copy handler is still in the layout")
	}
}
