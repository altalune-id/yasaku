package templates

import (
	"strings"
	"testing"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/platform/capabilities"
	"altalune.id/yasaku/internal/web"
)

func TestOrgsPage_CopyComesFromTheLocale(t *testing.T) {
	d := web.LayoutData{Nonce: "n", Caps: capabilities.Capabilities{OrgCreation: true}, Translator: i18n.NewEmbeddedBundle(i18n.EnUS).For(i18n.ArSA)}
	for name, v := range map[string]OrgsView{
		"list":  {Orgs: []OrgSummary{{Name: "Acme", Slug: "acme"}}},
		"empty": {},
	} {
		out := render(t, OrgsPage(d, v))
		for _, english := range []string{"Organizations", "Every workspace you belong to.", "You do not belong to any organization yet.", "Create one"} {
			if strings.Contains(out, english) {
				t.Errorf("%s: ar-SA page still shows %q", name, english)
			}
		}
	}
}
