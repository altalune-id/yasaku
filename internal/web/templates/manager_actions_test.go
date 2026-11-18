package templates

import (
	"strings"
	"testing"

	"github.com/a-h/templ"

	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/web"
)

func TestManagerOnlyActions_HiddenFromMembers(t *testing.T) {
	pages := map[string]func(d web.LayoutData) templ.Component{
		"layout without a project": func(d web.LayoutData) templ.Component {
			d.Principal, d.ActiveNav.Scope = &session.Principal{}, web.NavScopeProject
			return Layout(web.WithContent(d, templ.NopComponent))
		},
		"dashboard, empty": func(d web.LayoutData) templ.Component { return DashboardPage(d, DashboardView{OrgSlug: "acme"}) },
		"projects, empty":  func(d web.LayoutData) templ.Component { return ProjectsPage(d, ProjectsView{OrgSlug: "acme"}) },
		"home, period ended": func(d web.LayoutData) templ.Component {
			return homeEnded(d, YasakuOverviewView{ProjectSlug: "home", PeriodID: "cur"})
		},
	}
	actions := []string{"/projects/new", "/periods/cur/close"}
	for name, page := range pages {
		t.Run(name, func(t *testing.T) {
			manager := render(t, page(web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme", CanManage: true}}))
			member := render(t, page(web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme"}}))
			shown := false
			for _, a := range actions {
				shown = shown || strings.Contains(manager, a)
				if strings.Contains(member, a) {
					t.Errorf("member sees %s", a)
				}
			}
			if !shown {
				t.Error("manager sees no manager action")
			}
		})
	}
}

func TestProjectsPage_EmptyTellsAMemberToAskAManager(t *testing.T) {
	member := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme"}}
	requireContains(t, render(t, ProjectsPage(member, ProjectsView{OrgSlug: "acme"})), "projects.empty_member")
	requireContains(t, render(t, DashboardPage(member, DashboardView{OrgSlug: "acme"})), "projects.empty_member")
}
