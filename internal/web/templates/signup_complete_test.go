package templates

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"altalune.id/yasaku/internal/web"
)

var formTag = regexp.MustCompile(`<(/?)form\b[^>]*>`)

// SECURITY: a nested logout form is dropped by the HTML parser, so "Wrong account" would submit the outer form and create a workspace or accept terms.
func TestLogoutFormIsNotNestedInTheSignupOrWelcomeForm(t *testing.T) {
	for name, page := range map[string]templ.Component{
		"signup":  SignupCompletePage(web.LayoutData{}, SignupCompleteView{Email: "a@yasaku.test", AskDisplayName: true, AskAccept: true, TermsURL: "https://example.test/terms"}),
		"welcome": WelcomePage(web.LayoutData{}, WelcomeView{AskDisplayName: true, AskAccept: true, TermsURL: "https://example.test/terms"}),
	} {
		var sb strings.Builder
		if err := page.Render(context.Background(), &sb); err != nil {
			t.Fatalf("%s: render: %v", name, err)
		}
		got := sb.String()
		depth, logoutForms := 0, 0
		for _, m := range formTag.FindAllStringSubmatch(got, -1) {
			if m[1] == "/" {
				depth--
				continue
			}
			depth++
			if depth > 1 {
				t.Fatalf("%s: nested <form> in %q", name, got)
			}
			if strings.Contains(m[0], `action="/logout"`) {
				logoutForms++
			}
		}
		if depth != 0 {
			t.Fatalf("%s: unbalanced <form> tags in %q", name, got)
		}
		if logoutForms != 1 {
			t.Fatalf("%s: want one top-level logout form, got %d in %q", name, logoutForms, got)
		}
	}
}
