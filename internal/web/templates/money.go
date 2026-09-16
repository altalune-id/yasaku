package templates

import (
	"golang.org/x/text/language"

	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/money"
)

// Money formats a for the page locale.
func Money(d web.LayoutData, a money.Amount) string {
	return a.Format(localeTag(d))
}

// SignedMoney prefixes an inflow with "+" and an outflow with a minus sign; zero stays bare.
func SignedMoney(d web.LayoutData, a money.Amount) string {
	tag := localeTag(d)
	switch {
	case a.Minor > 0:
		return "+" + a.Format(tag)
	case a.Minor < 0:
		return "−" + a.Neg().Format(tag)
	default:
		return a.Format(tag)
	}
}

func localeTag(d web.LayoutData) language.Tag {
	tag, err := language.Parse(string(d.Locale))
	if err != nil {
		return language.AmericanEnglish
	}
	return tag
}
