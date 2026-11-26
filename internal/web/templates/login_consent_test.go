package templates

import (
	"strings"
	"testing"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/web"
)

func TestLogin_ConsentSentence(t *testing.T) {
	d := web.LayoutData{Translator: i18n.NewEmbeddedBundle(i18n.EnUS).For(i18n.EnUS)}
	terms := `<a href="/terms" class="link link-hover">Terms of Service</a>`
	privacy := `<a href="/privacy" class="link link-hover">Privacy Policy</a>`
	for _, tc := range []struct {
		name, terms, privacy, want string
	}{
		{"both", "/terms", "/privacy", "By continuing, you agree to the " + terms + " and " + privacy + "."},
		{"terms only", "/terms", "", "By continuing, you agree to the " + terms + "."},
		{"privacy only", "", "/privacy", "By continuing, you agree to the " + privacy + "."},
		{"escaped url", `/t?a="x"<b>`, "", `By continuing, you agree to the <a href="/t?a=&#34;x&#34;&lt;b&gt;" class="link link-hover">Terms of Service</a>.`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := render(t, LoginPage(d, LoginView{LocalAuth: true, TermsURL: tc.terms, PrivacyURL: tc.privacy}))
			start := strings.Index(body, "data-legal-links")
			if start < 0 {
				t.Fatal("no data-legal-links paragraph")
			}
			p := body[start : start+strings.Index(body[start:], "</p>")]
			if !strings.Contains(p, tc.want) {
				t.Errorf("consent = %q, want it to contain %q", p, tc.want)
			}
		})
	}
	if strings.Contains(render(t, LoginPage(d, LoginView{LocalAuth: true})), "data-legal-links") {
		t.Error("no URLs, no consent sentence")
	}
}
