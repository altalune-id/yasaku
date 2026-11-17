package templates

import (
	"strings"
	"testing"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/web"
)

func TestFirstRunWallets_HintsNameTheChipsAndSkipFormJargon(t *testing.T) {
	jargon := []string{"Submit", "Mengirim", "Menghantar", "送信", "الإرسال"}
	for _, loc := range []i18n.Locale{i18n.EnUS, i18n.IdID, i18n.MsMY, i18n.JaJP, i18n.ArSA} {
		t.Run(string(loc), func(t *testing.T) {
			tr := i18n.NewEmbeddedBundle(loc).For(loc)
			d := web.LayoutData{Translator: tr, ActiveOrg: &web.ActiveOrg{Slug: "acme"}}
			v := FirstRunView{ProjectSlug: "home", Step: 2, Chips: []FirstRunChip{
				{Kind: "cash", LabelKey: "wallet.kind_cash"},
				{Kind: "bank", LabelKey: "wallet.kind_bank"},
				{Kind: "ewallet", LabelKey: "wallet.kind_ewallet"},
				{Kind: "other", LabelKey: "wallet.kind_other"},
			}}
			body := render(t, firstRunWallets(d, v))
			start := strings.Index(body, "border-dashed")
			if start < 0 {
				t.Fatal("no empty hint")
			}
			hint := body[start : start+strings.Index(body[start:], "</p>")]
			for _, key := range []string{"wallet.kind_cash", "wallet.kind_bank", "wallet.kind_ewallet"} {
				if label := tr.T(key); !strings.Contains(hint, label) {
					t.Errorf("empty hint %q does not name the chip %q", hint, label)
				}
			}
			for _, w := range jargon {
				if strings.Contains(tr.T("firstrun.retry_hint"), w) {
					t.Errorf("retry hint uses form jargon %q: %s", w, tr.T("firstrun.retry_hint"))
				}
			}
		})
	}
}
