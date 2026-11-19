package templates

import (
	"strings"
	"testing"

	"altalune.id/yasaku/internal/web"
)

func TestFirstRunPage_MemberWaitsWithoutWriteControls(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme"}}
	got := render(t, FirstRunPage(d, FirstRunView{ProjectSlug: "home", Step: 1, Waiting: true}))
	requireContains(t, got, "firstrun.waiting_title", "firstrun.waiting_body", `href="/orgs/acme/projects"`, "firstrun.waiting_back")
	for _, banned := range []string{"<form", `name="currency"`, `class="steps`} {
		if strings.Contains(got, banned) {
			t.Errorf("waiting page renders %s", banned)
		}
	}
	if strings.Contains(render(t, FirstRunPage(d, FirstRunView{ProjectSlug: "home", Step: 1})), "firstrun.waiting_title") {
		t.Error("a manager sees the waiting page")
	}
}
