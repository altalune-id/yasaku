package templates_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/web/templates"
	"altalune.id/yasaku/money"
)

func TestCurrencyLabel_NameAndSymbolInEveryLocale(t *testing.T) {
	t.Parallel()
	b := i18n.NewEmbeddedBundle(i18n.EnUS)
	for _, loc := range b.All() {
		for _, c := range []money.Currency{money.IDR, "USD", "SGD", "MYR", "EUR", "JPY"} {
			got := templates.CurrencyLabel(b.For(loc).T, c)
			assert.NotEqual(t, string(c), got, "%s %s", loc, c)
			assert.NotEqual(t, "currency."+string(c), got, "%s %s", loc, c)
		}
	}
	assert.Equal(t, "Indonesian Rupiah (Rp)", templates.CurrencyLabel(b.For(i18n.EnUS).T, money.IDR))
	assert.Equal(t, "Rupiah Indonesia (Rp)", templates.CurrencyLabel(b.For(i18n.IdID).T, money.IDR))
}

func TestCurrencyLabel_UnknownCurrencyFallsBackToCode(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "XTS", templates.CurrencyLabel(i18n.NewEmbeddedBundle(i18n.EnUS).For(i18n.EnUS).T, "XTS"))
}
