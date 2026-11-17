package templates

import (
	"context"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/web"
)

func renderShell(t *testing.T, project *web.ActiveProject, scope web.NavScope) string {
	t.Helper()
	d := web.LayoutData{
		Title:         "T",
		Nonce:         "n0nce",
		Principal:     &session.Principal{Email: "a@example.com"},
		ActiveOrg:     &web.ActiveOrg{Slug: "acme", Name: "Acme"},
		ActiveProject: project,
		ActiveNav:     web.ActiveNav{Scope: scope, ProjectKey: "overview"},
		Content:       templ.Raw("<p>page</p>"),
	}
	var b strings.Builder
	if err := Layout(d).Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

func TestLayout_ShellIsDaisyUIDrawerWithAppearance(t *testing.T) {
	out := renderShell(t, &web.ActiveProject{Slug: "home", Name: "Home"}, web.NavScopeProject)
	for _, want := range []string{
		`class="drawer lg:drawer-open"`,
		`class="drawer-toggle"`,
		`class="drawer-overlay"`,
		`class="menu min-h-0 w-full grow`,
		`<details id="appearance"`,
		`action="/theme"`,
		`class="dock dock-md`,
		`>Yasaku</span>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("shell lacks %s", want)
		}
	}
	if strings.Count(out, `aria-current="page"`) < 2 {
		t.Error("the active item must be marked in both the rail and the dock")
	}
}

func TestLayout_DockOnlyWithAProject(t *testing.T) {
	out := renderShell(t, nil, web.NavScopeOrg)
	if strings.Contains(out, `class="dock`) {
		t.Error("the dock links into a project; it must not render without one")
	}
	if !strings.Contains(out, `drawer-button`) {
		t.Error("without the dock the phone navbar needs the drawer button")
	}
}

func TestAppDropdown_DetailsWithSummaryButton(t *testing.T) {
	trigger := templ.Raw("<span>open</span>")
	child := templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, `<p id="panel">x</p>`)
		return err
	})
	var b strings.Builder
	if err := AppDropdown(trigger, "", "w-full", "btn-ghost", "end-0 top-full").Render(templ.WithChildren(context.Background(), child), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := b.String()
	for _, want := range []string{`<details class="dropdown w-full">`, `<summary class="btn btn-ghost">`, `class="absolute z-40 `, `<p id="panel">x</p>`} {
		if !strings.Contains(out, want) {
			t.Errorf("AppDropdown() = %q, want %s", out, want)
		}
	}
}

func TestToast_ReplacesTheHostOutOfBand(t *testing.T) {
	var host, toast strings.Builder
	if err := ToastHost().Render(context.Background(), &host); err != nil {
		t.Fatal(err)
	}
	if err := Toast(web.LayoutData{Nonce: "n0nce"}, "Saved").Render(context.Background(), &toast); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(host.String(), `id="toast"`) || !strings.Contains(toast.String(), `id="toast" hx-swap-oob="true" hx-nonce="n0nce"`) {
		t.Fatalf("host %q and toast %q must share the id and swap out of band", host.String(), toast.String())
	}
	if !strings.Contains(toast.String(), `role="status"`) {
		t.Fatalf("toast %q must announce itself", toast.String())
	}
}

func TestLayout_BrandIconsAreServed(t *testing.T) {
	out := renderShell(t, nil, web.NavScopeOrg)
	root := repoRoot(t)
	for _, icon := range []struct{ tag, file string }{
		{`<link rel="icon" type="image/svg+xml" href="`, "yasaku-mark-16.svg"},
		{`<link rel="icon" type="image/png" sizes="32x32" href="`, "favicon-32x32.png"},
		{`<link rel="icon" type="image/png" sizes="16x16" href="`, "favicon-16x16.png"},
		{`<link rel="apple-touch-icon" href="`, "apple-touch-icon.png"},
	} {
		i := strings.Index(out, icon.tag)
		if i < 0 || !strings.Contains(out[i:i+len(icon.tag)+80], icon.file) {
			t.Errorf("head lacks %s%s", icon.tag, icon.file)
		}
		if _, err := os.Stat(filepath.Join(root, "internal/web/static", icon.file)); err != nil {
			t.Errorf("static/%s: %v", icon.file, err)
		}
	}
	if strings.Contains(out, "favicon.ico") {
		t.Error("the old favicon.ico must be gone")
	}
}

func TestAppButton_NeverWraps(t *testing.T) {
	var b strings.Builder
	ctx := templ.WithChildren(context.Background(), templ.Raw("New project"))
	if err := AppButton("/new", "btn-primary", templ.Attributes{"data-dialog-open": "x"}).Render(ctx, &b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"btn shrink-0 whitespace-nowrap", "btn-primary", `href="/new"`, `data-dialog-open="x"`} {
		if !strings.Contains(out, want) {
			t.Errorf("AppButton() = %q, want %s", out, want)
		}
	}
}

func TestPageHeader_StacksOnPhonesAndKeepsActionsWhole(t *testing.T) {
	var b strings.Builder
	ctx := templ.WithChildren(context.Background(), templ.Raw("<a>Act</a>"))
	if err := PageHeader("Title", "Sub").Render(ctx, &b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"flex flex-wrap items-start justify-between", `class="min-w-0 flex-[1_1_12rem]"`, "shrink-0", ">Title</h1>", ">Sub</p>", "<a>Act</a>"} {
		if !strings.Contains(out, want) {
			t.Errorf("PageHeader() = %q, want %s", out, want)
		}
	}
}

func TestLayout_DrawerOpenersAreKeyboardOperable(t *testing.T) {
	for name, c := range map[string]struct {
		out  string
		want int
	}{
		"dock More is a page link": {renderShell(t, &web.ActiveProject{Slug: "home", Name: "Home"}, web.NavScopeProject), 0},
		"phone hamburger":          {renderShell(t, nil, web.NavScopeOrg), 1},
	} {
		out := c.out
		openers := 0
		for _, tag := range strings.Split(out, "<label")[1:] {
			tag = tag[:strings.Index(tag, ">")]
			if !strings.Contains(tag, `for="alt-drawer"`) || strings.Contains(tag, "drawer-overlay") {
				continue
			}
			openers++
			for _, want := range []string{`role="button"`, `tabindex="0"`, `data-drawer-toggle`, `aria-expanded="false"`} {
				if !strings.Contains(tag, want) {
					t.Errorf("%s: drawer opener %q lacks %s", name, tag, want)
				}
			}
		}
		if openers != c.want {
			t.Errorf("%s: %d drawer openers, want %d", name, openers, c.want)
		}
		if !strings.Contains(out, `<input id="alt-drawer" type="checkbox" class="drawer-toggle" tabindex="-1"`) {
			t.Errorf("%s: the drawer checkbox must leave the tab order; its openers are the tab stops", name)
		}
	}
}

func TestLayout_PNGFaviconsMatchTheSVGMark(t *testing.T) {
	root := repoRoot(t)
	for _, name := range []string{"favicon-16x16.png", "favicon-32x32.png"} {
		f, err := os.Open(filepath.Join(root, "internal/web/static", name))
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(f)
		_ = f.Close()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		solid := 0
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
				if c.A != 0xff {
					continue
				}
				solid++
				if c.R != 0xff || c.G != 0xbb || c.B != 0x00 {
					t.Fatalf("%s: solid pixel %v at %d,%d, want #ffbb00 like the SVG mark", name, c, x, y)
				}
			}
		}
		if solid == 0 {
			t.Errorf("%s has no solid pixels", name)
		}
	}
}
