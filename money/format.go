package money

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/currency"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"
)

// Format renders a for display in the locale of tag with that locale's currency symbol, e.g. "Rp 40.000", "IDR 40,000" or "$12.34".
func (a Amount) Format(tag language.Tag) string {
	if !a.Currency.Valid() {
		return "0"
	}
	exp := a.Currency.Exponent()
	disp := a.Currency.DisplayExponent()
	if a.Minor%pow10(exp-disp) != 0 {
		disp = exp
	}
	value := float64(a.Minor) / float64(pow10(exp))
	p := message.NewPrinter(tag)
	num := p.Sprint(number.Decimal(value, number.MinFractionDigits(disp), number.MaxFractionDigits(disp)))
	sign := ""
	if strings.HasPrefix(num, "-") {
		sign, num = "-", num[1:]
	}
	sym := a.Currency.SymbolFor(tag)
	if r, _ := utf8.DecodeLastRuneInString(sym); unicode.IsLetter(r) {
		sym += "\u00a0"
	}
	return sign + sym + num
}

// SymbolFor is the currency symbol the locale of tag uses, e.g. "Rp" in Indonesian and "IDR" in English.
func (c Currency) SymbolFor(tag language.Tag) string {
	unit, err := currency.ParseISO(string(c))
	if err != nil {
		return c.Symbol()
	}
	return message.NewPrinter(tag).Sprint(currency.Symbol(unit))
}
