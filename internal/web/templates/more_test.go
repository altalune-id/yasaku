package templates

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/web"
)

func shellData(key string, pulse bool, seen ...string) web.LayoutData {
	return web.LayoutData{
		Title:         "T",
		Nonce:         "n0nce",
		Translator:    i18n.NewEmbeddedBundle(i18n.EnUS).For(i18n.EnUS),
		Principal:     &session.Principal{Email: "a@example.com", TipsSeen: seen},
		ActiveOrg:     &web.ActiveOrg{Slug: "acme", Name: "Acme"},
		ActiveProject: &web.ActiveProject{Slug: "home", Name: "Home"},
		ActiveNav:     web.ActiveNav{Scope: web.NavScopeProject, ProjectKey: key},
		CurrentPath:   "/orgs/acme/projects/home/more",
		AddPulse:      pulse,
		Opensheet:     true,
		Content:       templ.Raw("<p>page</p>"),
	}
}

func dockOf(t *testing.T, d web.LayoutData) string {
	t.Helper()
	var b strings.Builder
	if err := Layout(d).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	start := strings.Index(out, `class="dock`)
	return out[start : start+strings.Index(out[start:], "</nav>")]
}

func TestDock_CenterPlusOpensTheAddSheetAndMoreIsALink(t *testing.T) {
	dock := dockOf(t, shellData("overview", false))
	for _, want := range []string{
		`href="/orgs/acme/projects/home/transactions/new" data-dialog-open="tx-add-dialog" data-dialog-src="/orgs/acme/projects/home/transactions/new" data-tour="add"`,
		`href="/orgs/acme/projects/home/more"`,
	} {
		if !strings.Contains(dock, want) {
			t.Errorf("dock lacks %s\n%s", want, dock)
		}
	}
	if strings.Contains(dock, "data-drawer-toggle") || strings.Contains(dock, "alt-drawer") {
		t.Error("More is a page, not the drawer")
	}
	if strings.Contains(dock, "animate-ping") {
		t.Error("the + pulses only while Home asks for it")
	}
	if more := dockOf(t, shellData("more", false)); !strings.Contains(more, `aria-current="page"`) || !strings.Contains(more, `dock-active`) {
		t.Error("More is marked current on its own page")
	}
}

func TestDock_PulseAndTipOnlyWhileAskedAndUnseen(t *testing.T) {
	pulse := regexp.MustCompile(`data-tip-pulse="add-first"[^>]*animate-ping[^>]*motion-reduce:animate-none`)
	var b strings.Builder
	if err := Layout(shellData("overview", true)).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !pulse.MatchString(out) || !strings.Contains(out, `data-tip="add-first"`) || !strings.Contains(out, `data-tip-for="add"`) {
		t.Error("an asked-for pulse shows the ping and the anchored tip")
	}
	for name, d := range map[string]web.LayoutData{"not asked": shellData("overview", false), "seen": shellData("overview", true, "add-first")} {
		b.Reset()
		if err := Layout(d).Render(context.Background(), &b); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(b.String(), "animate-ping") || strings.Contains(b.String(), `data-tip="add-first"`) {
			t.Errorf("%s: no pulse and no tip", name)
		}
	}
}

func TestMorePage_ListsEveryPageTipsResetAndAppearance(t *testing.T) {
	d := shellData("more", false)
	out := render(t, MorePage(d))
	for _, want := range []string{
		`href="/orgs/acme/projects/home/categories"`,
		`href="/orgs/acme/projects/home/periods"`,
		`href="/orgs/acme/projects/home/opensheet"`,
		`href="/orgs/acme/projects/home/settings"`,
		`href="/orgs/acme/projects/home/apikeys"`,
		`href="/orgs/acme/members"`,
		`href="/orgs/acme/apikeys"`,
		`action="/tips/reset"`,
		`name="redirect" value="/orgs/acme/projects/home/more"`,
		`action="/theme"`,
		`Show tips again`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("More lacks %s", want)
		}
	}
	if strings.Contains(strings.ToLower(out), "csv") {
		t.Error("no export exists")
	}
	if rows := strings.Count(out, `class="list-row`); rows != 8 {
		t.Errorf("rows = %d, want 8: one list as in the prototype, Organization leads to Members and its Invites", rows)
	}
	if strings.Count(out, `block truncate text-sm text-base-content/70`) != 8 {
		t.Error("every link row keeps its description on one line")
	}
	d.Opensheet = false
	if strings.Contains(render(t, MorePage(d)), "/opensheet") {
		t.Error("the opensheet row follows the module switch")
	}
}

func TestMoreDescriptions_FitOneLineInEveryLocale(t *testing.T) {
	keys := []string{"more.desc_categories", "more.desc_periods", "more.desc_opensheet", "more.desc_settings", "more.desc_apikeys", "more.desc_organization", "more.desc_org_apikeys", "more.tips_again_desc"}
	bundle := i18n.NewEmbeddedBundle(i18n.EnUS)
	for _, loc := range bundle.All() {
		tr := bundle.For(loc)
		for _, k := range keys {
			if n := len([]rune(tr.T(k))); n == 0 || n > 36 {
				t.Errorf("%s %s has %d runes", loc, k, n)
			}
		}
	}
}
