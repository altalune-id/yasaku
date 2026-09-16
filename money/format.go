package money

import (
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"
)

// Format renders a for display in the locale of tag, e.g. "Rp40.000" or "$12.34".
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
	return sign + a.Currency.symbol() + num
}
