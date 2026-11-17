package money

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"
)

// ParseMajor parses a user-typed major-unit amount for c. NOTE: for a currency shown with decimals, a trailing "," that isn't a valid decimal mark falls back to grouping, but a trailing "." in that position is an error.
func ParseMajor(s string, c Currency) (Amount, error) { return parse(s, c, 0, "") }

// ParseLocale parses like ParseMajor, using tag's decimal mark to settle a lone separator before a three-digit tail.
func ParseLocale(s string, c Currency, tag language.Tag) (Amount, error) {
	return parse(s, c, DecimalMark(tag), c.SymbolFor(tag))
}

// DecimalMark returns the decimal separator tag's locale types: ',' or '.'.
func DecimalMark(tag language.Tag) byte {
	if s := message.NewPrinter(tag).Sprint(number.Decimal(1.5, number.MinFractionDigits(1))); len(s) == 3 && s[1] == ',' {
		return ','
	}
	return '.'
}

func parse(s string, c Currency, dec byte, localSymbol string) (Amount, error) {
	if !c.Valid() {
		return Amount{}, &UnknownCurrencyError{Code: string(c)}
	}
	raw := strings.TrimSpace(normalize(strings.TrimSpace(s), dec))
	if localSymbol != "" {
		raw = strings.TrimSpace(strings.TrimPrefix(raw, localSymbol))
	}
	raw = strings.TrimPrefix(raw, c.Symbol())
	raw = strings.TrimPrefix(strings.ToUpper(raw), string(c))
	raw = strings.ReplaceAll(strings.TrimSpace(raw), " ", "")
	if raw == "" {
		return Amount{}, &ParseError{Input: s, Reason: ReasonEmpty}
	}
	if isExpression(raw) {
		return Amount{}, &ParseError{Input: s, Reason: ReasonExpression}
	}
	if strings.HasPrefix(raw, "-") || strings.HasPrefix(raw, "−") {
		return Amount{}, &ParseError{Input: s, Reason: ReasonNegative}
	}
	whole, frac, reason := splitDecimal(raw, c.DisplayExponent(), dec)
	if reason != "" {
		return Amount{}, &ParseError{Input: s, Reason: reason}
	}
	whole = strings.NewReplacer(".", "", ",", "").Replace(whole)
	if whole == "" || strings.Trim(whole, "0123456789") != "" || strings.Trim(frac, "0123456789") != "" {
		return Amount{}, &ParseError{Input: s, Reason: ReasonNotNumber}
	}
	exp := c.Exponent()
	if len(frac) > exp {
		return Amount{}, &ParseError{Input: s, Reason: ReasonTooManyDecimals}
	}
	frac += strings.Repeat("0", exp-len(frac))
	n, err := strconv.ParseInt(whole+frac, 10, 64)
	if err != nil {
		return Amount{}, &ParseError{Input: s, Reason: ReasonOverflow}
	}
	return Amount{Minor: n, Currency: c}, nil
}

func normalize(s string, dec byte) string {
	mark, group := '.', ','
	if dec == ',' {
		mark, group = ',', '.'
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= '\u0660' && r <= '\u0669':
			return '0' + r - '\u0660'
		case r >= '\u06F0' && r <= '\u06F9':
			return '0' + r - '\u06F0'
		case r == '\u066B':
			return mark
		case r == '\u066C':
			return group
		case r == '\u00A0' || r == '\u202F':
			return -1
		}
		return r
	}, s)
}

func isExpression(raw string) bool {
	if strings.ContainsAny(raw, "+*/×÷xX()=") {
		return true
	}
	_, size := utf8.DecodeRuneInString(raw)
	return strings.ContainsAny(raw[size:], "-−")
}

func splitDecimal(raw string, displayExp int, dec byte) (whole, frac, reason string) {
	i := strings.LastIndexAny(raw, ".,")
	if i < 0 {
		return raw, "", ""
	}
	sep := raw[i]
	tail := raw[i+1:]
	if displayExp == 0 {
		return splitNoDecimals(raw, i, tail)
	}
	if dec != 0 && len(tail) == 3 && strings.Count(raw, ".")+strings.Count(raw, ",") == 1 {
		return splitHinted(raw, i, dec)
	}
	if len(tail) >= 1 && len(tail) <= displayExp {
		return raw[:i], tail, ""
	}
	if sep == ',' {
		return raw, "", ""
	}
	return "", "", ReasonAmbiguous
}

func splitHinted(raw string, i int, dec byte) (whole, frac, reason string) {
	if raw[i] == dec {
		return raw[:i], raw[i+1:], ""
	}
	if !validGroups(raw, raw[i]) {
		return "", "", ReasonAmbiguous
	}
	return raw, "", ""
}

// NOTE: a lone three-digit tail is grouping; any other tail is a decimal part, dropped only when it is all zeros.
func splitNoDecimals(raw string, i int, tail string) (whole, frac, reason string) {
	if strings.Trim(tail, "0123456789") != "" {
		return raw, "", ""
	}
	mark, head := raw[i], raw[:i]
	other := byte('.')
	if mark == '.' {
		other = ','
	}
	if len(tail) == 3 && strings.IndexByte(head, other) < 0 {
		if !validGroups(raw, mark) {
			return "", "", ReasonAmbiguous
		}
		return raw, "", ""
	}
	if strings.IndexByte(head, mark) >= 0 || !validGroups(head, other) {
		return "", "", ReasonAmbiguous
	}
	if strings.Trim(tail, "0") != "" {
		return "", "", ReasonNoDecimals
	}
	return head, "", ""
}

func validGroups(s string, sep byte) bool {
	groups := strings.Split(s, string(sep))
	if len(groups) == 1 {
		return true
	}
	if len(groups[0]) < 1 || len(groups[0]) > 3 || groups[0][0] == '0' {
		return false
	}
	for _, g := range groups[1:] {
		if len(g) != 3 {
			return false
		}
	}
	return true
}
