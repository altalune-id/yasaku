package templates

import "altalune.id/yasaku/money"

// CurrencyLabel returns the currency name with its symbol, e.g. "Indonesian Rupiah (Rp)", or the code when no name exists; tr is a translator's T method.
//
//i18n:use currency.*
func CurrencyLabel(tr func(key string, args ...any) string, c money.Currency) string {
	key := "currency." + string(c)
	if label := tr(key); label != key {
		return label
	}
	return string(c)
}
