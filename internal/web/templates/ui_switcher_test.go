package templates

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"altalune.id/yasaku/internal/web"
)

var phoneTrigger = regexp.MustCompile(`<[a-z]+[^>]*data-dialog-open="switcher"[^>]*>`)

var activeRow = regexp.MustCompile(`(?s)aria-current="page"[^>]*>(?:<svg.*?</svg>)?<span class="min-w-0 flex-1 truncate">([^<]+)</span>`)

func renderSwitcher(t *testing.T, d web.LayoutData) string {
	t.Helper()
	var b strings.Builder
	if err := switcherList(d, "notebook-text").Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

func TestSwitcher_OneOrgHidesOrgGroups(t *testing.T) {
	out := renderSwitcher(t, web.LayoutData{
		ActiveOrg:     &web.ActiveOrg{Slug: "home", Name: "Home Org", CanManage: true},
		ActiveProject: &web.ActiveProject{Slug: "b", Name: "Budget"},
	})
	if strings.Contains(out, "menu-title") {
		t.Errorf("one org must show no org headings: %s", out)
	}
	if !strings.Contains(out, `<button type="button" data-dialog-close title="Budget" class="menu-active" aria-current="page">`) {
		t.Errorf("the current project row must close the switcher, styled menu-active: %s", out)
	}
	if strings.Contains(out, `href="/orgs/home/projects/b/overview"`) {
		t.Errorf("the current project row must not navigate: %s", out)
	}
	for _, want := range []string{`aria-current="page"`, "nav.new_project", "switcher.manage_org", `href="/orgs/home"`} {
		if !strings.Contains(out, want) {
			t.Errorf("switcher lacks %s", want)
		}
	}
}

func TestSwitcher_OtherOrgsLinkToTheOrgWithSystemBadge(t *testing.T) {
	out := renderSwitcher(t, web.LayoutData{
		ActiveOrg:     &web.ActiveOrg{Slug: "home", Name: "Home Org"},
		OtherOrgs:     []web.ActiveOrg{{Slug: "ops", Name: "Ops", System: true}},
		ActiveProject: &web.ActiveProject{Slug: "b", Name: "Budget"},
		OtherProjects: []web.ActiveProject{{Slug: "c", Name: "Cash"}},
	})
	if strings.Count(out, "Home Org") != 1 {
		t.Errorf("active org must show once, as the heading: %s", out)
	}
	for _, want := range []string{"switcher.other_orgs", `href="/orgs/ops/switch"`, `href="/orgs/home"`, "common.system", "rtl:rotate-180", `title="Cash"`} {
		if !strings.Contains(out, want) {
			t.Errorf("switcher lacks %s", want)
		}
	}
	if strings.Contains(out, `href="/orgs/ops"`) {
		t.Error("an other-org row must land on that org's project, not its overview")
	}
	if strings.Contains(out, "Current") || strings.Contains(out, ">Switch<") {
		t.Error("hardcoded English labels must be gone")
	}
}

func TestSwitcher_PhoneTriggerOpensTheSheet(t *testing.T) {
	out := renderShell(t, &web.ActiveProject{Slug: "home", Name: "Home"}, web.NavScopeProject)
	for _, want := range []string{`data-dialog-open="switcher"`, `aria-haspopup="dialog"`, `<dialog id="switcher" class="modal modal-bottom sm:modal-middle"`} {
		if !strings.Contains(out, want) {
			t.Errorf("shell lacks %s", want)
		}
	}
}

func TestSwitcher_TriggerNamesTheCheckedProject(t *testing.T) {
	d := web.LayoutData{
		ActiveOrg:     &web.ActiveOrg{Slug: "home", Name: "Household"},
		ActiveProject: &web.ActiveProject{Slug: "family", Name: "Family budget"},
		OtherProjects: []web.ActiveProject{{Slug: "holiday", Name: "Holiday fund"}},
	}
	var b strings.Builder
	if err := switcherRail(d, "notebook-text").Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	summary := out[strings.Index(out, "<summary"):strings.Index(out, "</summary>")]
	checked := activeRow.FindStringSubmatch(out)
	if checked == nil {
		t.Fatalf("no aria-current row: %s", out)
	}
	if !strings.Contains(summary, ">"+checked[1]+"<") {
		t.Errorf("trigger %q must name the checked project %q", summary, checked[1])
	}
	if !strings.Contains(summary, `text-xs font-normal text-base-content/70">Household<`) || !strings.Contains(summary, "h-auto min-h-11") {
		t.Errorf("the trigger shows the org small above the project, even with one org: %s", summary)
	}
}

func TestLayout_NavbarSwitcherAlwaysHasItsSheet(t *testing.T) {
	for _, tc := range []struct {
		name    string
		project *web.ActiveProject
		scope   web.NavScope
	}{
		{"project", &web.ActiveProject{Slug: "p", Name: "P"}, web.NavScopeProject},
		{"org", nil, web.NavScopeOrg},
		{"settings", nil, web.NavScopeSettings},
	} {
		out := renderShell(t, tc.project, tc.scope)
		trigger := phoneTrigger.FindString(out)
		if trigger == "" {
			t.Errorf("%s: no navbar switcher trigger", tc.name)
			continue
		}
		if !strings.HasPrefix(trigger, `<button type="button"`) {
			t.Errorf("%s: navbar switcher must be a plain button, got %s", tc.name, trigger)
		}
		if strings.Count(out, `<dialog id="switcher"`) != 1 {
			t.Errorf("%s: the trigger needs exactly one switcher dialog on the page", tc.name)
		}
	}
}
