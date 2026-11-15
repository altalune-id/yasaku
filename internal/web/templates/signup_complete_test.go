package templates

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"altalune.id/yasaku/internal/web"
)

var formTag = regexp.MustCompile(`<(/?)form\b[^>]*>`)

// SECURITY: a nested logout form is dropped by the HTML parser, so "Wrong account" would submit the signup form and create a workspace.
func TestSignupCompletePage_LogoutFormIsNotNestedInTheSignupForm(t *testing.T) {
	var sb strings.Builder
	v := SignupCompleteView{Email: "a@yasaku.test", AskDisplayName: true, AskAccept: true, TermsURL: "https://example.test/terms"}
	if err := SignupCompletePage(web.LayoutData{}, v).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
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
			t.Fatalf("nested <form> in %q", got)
		}
		if strings.Contains(m[0], `action="/logout"`) {
			logoutForms++
		}
	}
	if depth != 0 {
		t.Fatalf("unbalanced <form> tags in %q", got)
	}
	if logoutForms != 1 {
		t.Fatalf("want one top-level logout form, got %d in %q", logoutForms, got)
	}
}
